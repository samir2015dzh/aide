// Package devicekey keeps a Linux trusted device's root key (hermesh
// docs/onboarding.md §9.3): a P-256 key in a file sealed with a passphrase
// (Argon2id, then AES-256-GCM). Only `hermeshd approve` and `hermeshd
// device` open it, for as long as they run; the owner role never does.
package devicekey

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/argon2"

	"github.com/hermeshai/hermesh/pkg/identity"
)

// Label is how the other devices name the kind of key (RootKey.platform).
const Label = "linux · passphrase"

// ErrPassphrase: the passphrase does not open the key.
var ErrPassphrase = errors.New("wrong passphrase")

// sealed is the file: the KDF's parameters, and the key's PKCS#8 DER sealed
// with the key they derive. The public key is in the clear, to find the key
// a request names without the passphrase.
type sealed struct {
	Version   int    `json:"version"`
	KDF       string `json:"kdf"`
	Salt      []byte `json:"salt"`
	Time      uint32 `json:"time"`
	MemoryKiB uint32 `json:"memory_kib"`
	Threads   uint8  `json:"threads"`
	Nonce     []byte `json:"nonce"`
	Sealed    []byte `json:"sealed"`
	PublicKey []byte `json:"public_key"` // SPKI DER
}

// The OWASP minimum for Argon2id is 19 MiB, 2 passes; this is above it.
const (
	argonTime   = 3
	argonMemory = 64 * 1024
	argonLanes  = 4
)

// Path is where the key with this key id lives in dir.
func Path(dir, keyID string) string { return filepath.Join(dir, keyID+".key") }

// Create makes a new key sealed with passphrase in dir, and returns it.
func Create(dir, passphrase string) (*identity.SoftwareKey, error) {
	k, err := identity.GenerateKey()
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(k.Key)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return k, Seal(Path(dir, identity.KeyID(k.Public())), passphrase, der, identity.MarshalPublicKey(k.Public()))
}

// Open unseals the key with this key id.
func Open(dir, keyID, passphrase string) (*identity.SoftwareKey, error) {
	der, _, err := Unseal(Path(dir, keyID), passphrase)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("this computer doesn't hold the key %s", keyID)
		}
		return nil, err
	}
	pk, err := x509.ParsePKCS8PrivateKey(der)
	ek, ok := pk.(*ecdsa.PrivateKey)
	if err != nil || !ok || ek.Curve != elliptic.P256() {
		return nil, errors.New("the key file holds no P-256 key")
	}
	k := &identity.SoftwareKey{Key: ek}
	if identity.KeyID(k.Public()) != keyID {
		return nil, errors.New("the key file holds another key")
	}
	return k, nil
}

// Seal writes secret to path sealed with passphrase, as a key file is,
// bound to the public key pub (SPKI DER): a Windows trusted device's
// passphrase seals its TPM key's PIN this way (internal/signer/unlock).
func Seal(path, passphrase string, secret, pub []byte) error {
	s := sealed{Version: 1, KDF: "argon2id", Salt: random(16), Time: argonTime, MemoryKiB: argonMemory, Threads: argonLanes, Nonce: random(12), PublicKey: pub}
	aead, err := s.aead(passphrase)
	if err != nil {
		return err
	}
	s.Sealed = aead.Seal(nil, s.Nonce, secret, s.PublicKey)
	b, _ := json.MarshalIndent(s, "", "  ")
	if err := os.WriteFile(path+".tmp", b, 0o600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

// Unseal opens what Seal wrote to path, and the public key it is bound to;
// ErrPassphrase when passphrase does not open it.
func Unseal(path, passphrase string) (secret, pub []byte, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var s sealed
	if err := json.Unmarshal(b, &s); err != nil || s.Version != 1 || s.KDF != "argon2id" {
		return nil, nil, fmt.Errorf("%s: not a key this version reads", path)
	}
	aead, err := s.aead(passphrase)
	if err != nil {
		return nil, nil, err
	}
	secret, err = aead.Open(nil, s.Nonce, s.Sealed, s.PublicKey)
	if err != nil {
		return nil, nil, ErrPassphrase
	}
	return secret, s.PublicKey, nil
}

// Delete removes the key with this key id (the device left its owner).
func Delete(dir, keyID string) error {
	err := os.Remove(Path(dir, keyID))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *sealed) aead(passphrase string) (cipher.AEAD, error) {
	if s.Time == 0 || s.MemoryKiB == 0 || s.Threads == 0 || len(s.Salt) < 16 {
		return nil, errors.New("bad key file parameters")
	}
	key := argon2.IDKey([]byte(passphrase), s.Salt, s.Time, s.MemoryKiB, s.Threads, 32)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func random(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}
