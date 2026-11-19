package relay

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	identityv1 "github.com/hermeshai/hermesh/gen/go/hermesh/identity/v1"
	relayv1 "github.com/hermeshai/hermesh/gen/go/hermesh/relay/v1"
	"github.com/hermeshai/hermesh/internal/passlogin"
	"github.com/hermeshai/hermesh/pkg/identity"
)

// Logging in with an account and password (docs/password-login.md). The
// relay keeps the owner's password key sealed by the password and a
// verifier of the key the password derives; it sees neither the password
// nor the key that opens the seal. It hands the sealed key only for the
// right auth key, slows guessing (per address, and a lockout of the account
// after wrong passwords in a row), and takes with it only a root chain
// that adds keys signed by the password key, or a pairing's credential.

const (
	// loginsPerMinute bounds login calls per address.
	loginsPerMinute = 20
	// After lockAfter wrong passwords in a row the account is locked for
	// lockFor, doubling with each further run, up to lockMax.
	lockAfter = 5
	lockFor   = time.Minute
	lockMax   = time.Hour
)

// loginGuard counts an account's wrong passwords in a row.
type loginGuard struct {
	mu    sync.Mutex
	wrong int
	runs  int
	until time.Time
}

// locked returns how long logging in is still locked.
func (g *loginGuard) locked(now time.Time) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.until.Sub(now)
}

func (g *loginGuard) failed(now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.wrong++
	if g.wrong >= lockAfter {
		d := lockFor << g.runs
		if d > lockMax || d <= 0 {
			d = lockMax
		}
		g.until, g.wrong = now.Add(d), 0
		g.runs++
	}
}

func (g *loginGuard) succeeded() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.wrong, g.runs, g.until = 0, 0, time.Time{}
}

// passwordStore is where the relay keeps the login (its meta).
func (s *Server) passwordStore() (*Store, error) {
	return s.sqlite, nil
}

// fakeKDF answers StartLogin for an account that has no login, alike for
// the same name each time, so that answers tell no account names apart.
func (s *Server) fakeKDF(st *Store, account string) (*relayv1.PasswordKdf, error) {
	secret, err := st.loginSecret()
	if err != nil {
		return nil, err
	}
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(account))
	k := passlogin.NewKDF()
	k.Salt = m.Sum(nil)[:16]
	return k, nil
}

func (p *publicService) StartLogin(ctx context.Context, req *relayv1.StartLoginRequest) (*relayv1.StartLoginResponse, error) {
	s := p.s
	st, err := s.passwordStore()
	if err != nil {
		return nil, err
	}
	if !s.loginLimit.Allow(callerIP(ctx)) {
		return nil, errf(connect.CodeResourceExhausted, "too many attempts; wait a minute")
	}
	account := passlogin.NormalizeAccount(req.Account)
	l, err := st.PasswordLogin()
	if err != nil {
		return nil, err
	}
	if l == nil || l.Account != account {
		k, err := s.fakeKDF(st, account)
		return &relayv1.StartLoginResponse{Kdf: k}, err
	}
	return &relayv1.StartLoginResponse{Kdf: l.Kdf}, nil
}

func (p *publicService) Login(ctx context.Context, req *relayv1.LoginRequest) (*relayv1.LoginResponse, error) {
	s := p.s
	st, err := s.passwordStore()
	if err != nil {
		return nil, err
	}
	owner := s.singleOwner()
	if owner == "" {
		return nil, errf(connect.CodeFailedPrecondition, "relay is not claimed yet")
	}
	if !s.loginLimit.Allow(callerIP(ctx)) {
		return nil, errf(connect.CodeResourceExhausted, "too many attempts; wait a minute")
	}
	now := time.Now()
	if d := s.logins.locked(now); d > 0 {
		return nil, errf(connect.CodeResourceExhausted, fmt.Sprintf("too many wrong passwords; try again in %d minutes", int(d.Minutes())+1))
	}
	t, err := s.tenantFor(owner)
	if err != nil {
		return nil, err
	}
	l, err := st.PasswordLogin()
	if err != nil {
		return nil, err
	}
	if l == nil || l.Account != passlogin.NormalizeAccount(req.Account) || !passlogin.Check(l.Verifier, req.AuthKey) {
		s.logins.failed(now)
		s.log.Info("password login refused", "remote", callerIP(ctx))
		return nil, connect.NewError(connect.CodePermissionDenied, passlogin.ErrWrong)
	}
	s.logins.succeeded()
	if !passlogin.Listed(t.trust.Root(), l.PublicKey) {
		return nil, errf(connect.CodeFailedPrecondition, "logging in with a password is turned off")
	}
	switch {
	case req.RootChain != nil:
		if err := s.passwordJoin(t, l.PublicKey, req.RootChain); err != nil {
			return nil, err
		}
		s.log.Info("a device logged in with the password", "remote", callerIP(ctx))
	case req.PairingCode != "":
		row, err := s.store.GetPairing(identity.NormalizePairingCode(req.PairingCode))
		if err != nil {
			return nil, err
		}
		if row == nil || now.After(row.ExpiresAt) || s.pairingOwner(row) != owner {
			return nil, errf(connect.CodeNotFound, "no pending pairing request with this code (it may have expired)")
		}
		if req.Credential.GetSignerKeyId() != keyIDOf(l.PublicKey) {
			return nil, errf(connect.CodeInvalidArgument, "the credential is not signed with the password key")
		}
		if err := s.completePairing(t, row, req.Credential); err != nil {
			return nil, err
		}
		s.log.Info("a pairing completed with the password", "remote", callerIP(ctx))
	}
	return &relayv1.LoginResponse{OwnerId: owner, RootChain: t.trust.RootChain(), Login: &relayv1.PasswordLogin{
		Account: l.Account, Nonce: l.Nonce, SealedKey: l.SealedKey, PublicKey: l.PublicKey}}, nil
}

// passwordJoin takes a root chain from a client that logged in: one cert
// more, signed by the password key, that keeps every key and adds some.
// What else the password key could sign goes through the owner's own
// connection like any root key's.
func (s *Server) passwordJoin(t *tenant, passwordKey []byte, c *identityv1.RootChain) error {
	cur := t.trust.Root()
	certs := c.GetCerts()
	if uint64(len(certs)) != cur.GetVersion()+1 {
		return errf(connect.CodeAborted, "the root chain changed meanwhile; log in again")
	}
	last := certs[len(certs)-1]
	next := &identityv1.RootCert{}
	if err := identity.Unverified(last, next); err != nil {
		return errf(connect.CodeInvalidArgument, err.Error())
	}
	if last.SignerKeyId != keyIDOf(passwordKey) {
		return errf(connect.CodeInvalidArgument, "the root cert is not signed with the password key")
	}
	for _, rk := range cur.RootKeys {
		if !passlogin.Listed(next, rk.PublicKey) {
			return errf(connect.CodeInvalidArgument, "logging in can only add a device")
		}
	}
	if err := s.acceptRootChain(t, c); err != nil {
		return errf(connect.CodeInvalidArgument, err.Error())
	}
	return nil
}

func keyIDOf(spki []byte) string {
	pub, err := identity.ParsePublicKey(spki)
	if err != nil {
		return ""
	}
	return identity.KeyID(pub)
}

func callerIP(ctx context.Context) string {
	if ci, ok := connect.CallInfoForHandlerContext(ctx); ok {
		return clientIP(ci.Peer().Addr)
	}
	return ""
}

func (o *ownerControl) SetPasswordLogin(ctx context.Context, req *relayv1.SetPasswordLoginRequest) (*relayv1.SetPasswordLoginResponse, error) {
	st, err := o.s.passwordStore()
	if err != nil {
		return nil, err
	}
	t := peerFrom(ctx).t
	l := req.Login
	if l != nil {
		if passlogin.NormalizeAccount(l.Account) == "" || l.Account != passlogin.NormalizeAccount(l.Account) || len(l.Verifier) != sha256.Size ||
			len(l.SealedKey) == 0 || l.Kdf == nil {
			return nil, errf(connect.CodeInvalidArgument, "incomplete password login")
		}
		if _, _, err := passlogin.Derive("", l.Kdf); err != nil {
			return nil, errf(connect.CodeInvalidArgument, err.Error())
		}
		if !passlogin.Listed(t.trust.Root(), l.PublicKey) {
			return nil, errf(connect.CodeFailedPrecondition, "publish the root chain with the password key first")
		}
	}
	if err := st.SetPasswordLogin(l); err != nil {
		return nil, err
	}
	o.s.logins.succeeded()
	return &relayv1.SetPasswordLoginResponse{}, nil
}

func (o *ownerControl) GetPasswordLogin(ctx context.Context, _ *relayv1.GetPasswordLoginRequest) (*relayv1.GetPasswordLoginResponse, error) {
	st, err := o.s.passwordStore()
	if err != nil {
		return nil, err
	}
	l, err := st.PasswordLogin()
	if err != nil || l == nil || !passlogin.Listed(peerFrom(ctx).t.trust.Root(), l.PublicKey) {
		return &relayv1.GetPasswordLoginResponse{}, err
	}
	return &relayv1.GetPasswordLoginResponse{Account: l.Account, PublicKey: l.PublicKey}, nil
}

// PasswordLogin is the login kept, nil when there is none.
func (s *Store) PasswordLogin() (*relayv1.PasswordLogin, error) {
	v, err := s.getMeta("password_login")
	if err != nil || len(v) == 0 {
		return nil, err
	}
	out := &relayv1.PasswordLogin{}
	return out, proto.Unmarshal(v, out)
}

// SetPasswordLogin keeps l, or with nil forgets the login.
func (s *Store) SetPasswordLogin(l *relayv1.PasswordLogin) error {
	if l == nil {
		_, err := s.db.Exec(`DELETE FROM meta WHERE key='password_login'`)
		return err
	}
	b, err := proto.Marshal(l)
	if err != nil {
		return err
	}
	return s.setMeta("password_login", b)
}

// loginSecret makes made-up KDF parameters for unknown accounts.
func (s *Store) loginSecret() ([]byte, error) {
	v, err := s.getMeta("login_secret")
	if err != nil || len(v) > 0 {
		return v, err
	}
	v = []byte(identity.RandomCode(32))
	return v, s.setMeta("login_secret", v)
}
