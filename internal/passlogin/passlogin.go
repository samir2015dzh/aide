// Package passlogin is logging in with an account and password
// (docs/password-login.md): the owner's password key, a root key listed in
// the root cert like any trusted device's, kept on the relay sealed by the
// password. A client that knows the password opens it and signs, with it,
// what a trusted device would have approved; the relay hands the sealed key
// only to a client that proves the password, and never sees either.
package passlogin

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"
	"golang.org/x/crypto/argon2"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/hermeshai/hermesh/gen/go/hermesh/identity/v1"
	relayv1 "github.com/hermeshai/hermesh/gen/go/hermesh/relay/v1"
	"github.com/hermeshai/hermesh/gen/go/hermesh/relay/v1/relayv1connect"
	"github.com/hermeshai/hermesh/pkg/identity"
)

// Platform names the password key in the root cert (RootKey.platform), so
// that devices tell it from a device's, and Label is its label.
const (
	Platform = "password"
	Label    = "账号密码登录"
)

// Argon2id costs: as internal/devicekey's (above OWASP's minimum), so that
// a guess costs a phone well under a second and an attacker as much.
const (
	argonTime    = 3
	argonMemory  = 64 * 1024
	argonThreads = 4
	// Limits a relay's answer may not exceed (a relay can't make a phone
	// spend a gigabyte).
	maxMemory = 256 * 1024
	maxTime   = 10
)

// MinPassword is the shortest password accepted.
const MinPassword = 8

// ErrWrong is the relay's answer to a wrong account or password.
var ErrWrong = errors.New("wrong account or password")

// NormalizeAccount is an account name as the relay compares it.
func NormalizeAccount(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// CheckNew says what is wrong with a new account and password, if anything.
func CheckNew(account, password string) error {
	a := NormalizeAccount(account)
	switch {
	case a == "":
		return errors.New("an account name is needed")
	case len(a) > 64 || strings.ContainsAny(a, " \t\r\n/"):
		return errors.New("the account name is at most 64 characters, without spaces or slashes")
	case utf8.RuneCountInString(password) < MinPassword:
		return fmt.Errorf("the password needs at least %d characters", MinPassword)
	case strings.EqualFold(strings.TrimSpace(password), a):
		return errors.New("the password can't be the account name")
	}
	return nil
}

// NewKDF is fresh parameters for a new password.
func NewKDF() *relayv1.PasswordKdf {
	return &relayv1.PasswordKdf{Salt: random(16), Time: argonTime, MemoryKib: argonMemory, Threads: argonThreads}
}

// Derive is Argon2id(password): the auth key, which the relay checks, and
// the key that seals the password key, which never leaves the client.
func Derive(password string, kdf *relayv1.PasswordKdf) (authKey, sealKey []byte, err error) {
	if kdf == nil || len(kdf.Salt) < 16 || kdf.Time == 0 || kdf.Time > maxTime || kdf.MemoryKib < 8*1024 || kdf.MemoryKib > maxMemory ||
		kdf.Threads == 0 || kdf.Threads > 16 {
		return nil, nil, errors.New("the server sent password parameters out of bounds")
	}
	k := argon2.IDKey([]byte(password), kdf.Salt, kdf.Time, kdf.MemoryKib, uint8(kdf.Threads), 64)
	return k[:32], k[32:], nil
}

// Verifier is what the relay keeps to check an auth key.
func Verifier(authKey []byte) []byte {
	h := sha256.Sum256(authKey)
	return h[:]
}

// Check tells whether authKey matches the verifier, in constant time.
func Check(verifier, authKey []byte) bool {
	return len(verifier) == sha256.Size && subtle.ConstantTimeCompare(verifier, Verifier(authKey)) == 1
}

// aad binds the sealed key to its owner and public key: a relay can't hand
// one owner's sealed key out as another's.
func aad(ownerID string, spki []byte) []byte {
	return append([]byte("hermesh password key\x00"+ownerID+"\x00"), spki...)
}

// Seal makes what the relay keeps for account and password: key sealed by
// the password.
func Seal(ownerID, account, password string, key *identity.SoftwareKey) (*relayv1.PasswordLogin, error) {
	kdf := NewKDF()
	authKey, sealKey, err := Derive(password, kdf)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key.Key)
	if err != nil {
		return nil, err
	}
	gcm, err := newGCM(sealKey)
	if err != nil {
		return nil, err
	}
	spki := identity.MarshalPublicKey(key.Public())
	nonce := random(gcm.NonceSize())
	return &relayv1.PasswordLogin{Account: NormalizeAccount(account), Kdf: kdf, Verifier: Verifier(authKey), Nonce: nonce,
		SealedKey: gcm.Seal(nil, nonce, der, aad(ownerID, spki)), PublicKey: spki}, nil
}

// Open unseals the password key with the key the password derived.
func Open(ownerID string, l *relayv1.PasswordLogin, sealKey []byte) (*identity.SoftwareKey, error) {
	gcm, err := newGCM(sealKey)
	if err != nil {
		return nil, err
	}
	if len(l.GetNonce()) != gcm.NonceSize() {
		return nil, errors.New("the sealed password key is malformed")
	}
	der, err := gcm.Open(nil, l.Nonce, l.SealedKey, aad(ownerID, l.PublicKey))
	if err != nil {
		return nil, errors.New("the password does not open the password key")
	}
	k, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, err
	}
	ek, ok := k.(*ecdsa.PrivateKey)
	if !ok || ek.Curve != elliptic.P256() {
		return nil, errors.New("the password key is not a P-256 key")
	}
	sk := &identity.SoftwareKey{Key: ek}
	if !bytes.Equal(identity.MarshalPublicKey(sk.Public()), l.PublicKey) {
		return nil, errors.New("the password key is not the one named")
	}
	return sk, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

func random(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

// Session is a password login that went through: the owner, its root chain
// (verified, listing Key), the password key opened, and the auth key, to
// publish what was signed with it.
type Session struct {
	Account string
	OwnerID string
	Trust   *identity.Trust
	Key     *identity.SoftwareKey
	AuthKey []byte
	pub     relayv1connect.RelayPublicServiceClient
}

// Login proves the password to the relay and opens the password key. The
// relay is not trusted: the key opens only with the password, sealed to
// the owner it names, and the root chain must check out under that owner
// and list the key.
func Login(ctx context.Context, pub relayv1connect.RelayPublicServiceClient, account, password string) (*Session, error) {
	account = NormalizeAccount(account)
	if account == "" || password == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("the account and the password are needed"))
	}
	st, err := pub.StartLogin(ctx, &relayv1.StartLoginRequest{Account: account})
	if err != nil {
		return nil, err
	}
	authKey, sealKey, err := Derive(password, st.Kdf)
	if err != nil {
		return nil, err
	}
	res, err := pub.Login(ctx, &relayv1.LoginRequest{Account: account, AuthKey: authKey})
	if err != nil {
		return nil, err
	}
	trust := identity.NewTrust(res.OwnerId)
	if _, err := trust.AcceptRootChain(res.RootChain); err != nil {
		return nil, fmt.Errorf("the server's root chain does not check out: %w", err)
	}
	key, err := Open(res.OwnerId, res.Login, sealKey)
	if err != nil {
		return nil, err
	}
	if !Listed(trust.Root(), res.Login.PublicKey) {
		return nil, errors.New("logging in with a password is turned off for this account")
	}
	return &Session{Account: account, OwnerID: res.OwnerId, Trust: trust, Key: key, AuthKey: authKey, pub: pub}, nil
}

// Listed tells whether the root cert lists the key.
func Listed(rc *identityv1.RootCert, spki []byte) bool {
	for _, rk := range rc.GetRootKeys() {
		if bytes.Equal(rk.PublicKey, spki) {
			return true
		}
	}
	return false
}

// Join adds root keys to the owner's root cert, signed with the password
// key, and publishes the longer chain; it returns it.
func (s *Session) Join(ctx context.Context, keys ...*identityv1.RootKey) (*identityv1.RootChain, error) {
	cur := s.Trust.Root()
	next := &identityv1.RootCert{OwnerId: s.OwnerID, RootKeys: append(append([]*identityv1.RootKey{}, cur.RootKeys...), keys...),
		Version: cur.Version + 1, IssuedAt: timestamppb.New(time.Now())}
	signed, err := identity.Sign(next, s.Key)
	if err != nil {
		return nil, err
	}
	chain := s.Trust.RootChain()
	chain.Certs = append(chain.Certs, signed)
	if _, err := s.Trust.AcceptRootChain(chain); err != nil {
		return nil, err
	}
	if _, err := s.pub.Login(ctx, &relayv1.LoginRequest{Account: s.Account, AuthKey: s.AuthKey, RootChain: chain}); err != nil {
		return nil, err
	}
	return chain, nil
}

// Complete completes a pending pairing request with a credential signed
// with the password key (an agent's grant).
func (s *Session) Complete(ctx context.Context, code string, credential *identityv1.Signed) error {
	_, err := s.pub.Login(ctx, &relayv1.LoginRequest{Account: s.Account, AuthKey: s.AuthKey, PairingCode: code, Credential: credential})
	return err
}

// Sign signs msgs with the password key.
func (s *Session) Sign(msgs ...proto.Message) ([]*identityv1.Signed, error) {
	out := make([]*identityv1.Signed, 0, len(msgs))
	for _, m := range msgs {
		sg, err := identity.Sign(m, s.Key)
		if err != nil {
			return nil, err
		}
		out = append(out, sg)
	}
	return out, nil
}
