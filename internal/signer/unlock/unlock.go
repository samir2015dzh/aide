// Package unlock keeps the root keys of a Windows trusted device and how
// each one is unlocked (docs/windows.md §7.5). A key lives in a vault, the
// TPM, which never lets it out, and is usable only with a random PIN that
// the signer keeps in a folder only administrators read. In Windows Hello
// mode, the default, the PIN is there as it is and Windows Hello confirms
// the owner before each signature. In passphrase mode the PIN is sealed
// with a passphrase of the owner's (internal/devicekey's format: Argon2id,
// then AES-256-GCM), typed in the signer's panel instead; wrong ones make
// the next try wait longer and longer, and the count survives restarts.
//
// Switching modes never moves or changes the key: only how its PIN is
// kept. The device keeps its root key, so its root cert, and everything it
// signed or paired, stay as they are.
package unlock

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hermeshai/hermesh/internal/devicekey"
	"github.com/hermeshai/hermesh/internal/signer"
	"github.com/hermeshai/hermesh/pkg/identity"
)

// Mode is how a root key is unlocked for a signature.
type Mode string

const (
	Hello      Mode = "hello"
	Passphrase Mode = "passphrase"
)

// MinPassphrase is the shortest passphrase, in characters, as for a Linux
// trusted device's key (hermeshd device setup).
const MinPassphrase = 8

var (
	// ErrPassphrase: the passphrase does not open the key.
	ErrPassphrase = devicekey.ErrPassphrase
	// ErrShort: a new passphrase is shorter than MinPassphrase.
	ErrShort = fmt.Errorf("a passphrase has at least %d characters", MinPassphrase)
	// ErrNotUnlocked: a key in passphrase mode was asked to sign without
	// its passphrase (Unlock) first.
	ErrNotUnlocked = errors.New("the key was not unlocked with its passphrase")
)

// Locked: too many wrong passphrases; the next one is tried only after Wait.
type Locked struct{ Wait time.Duration }

func (e *Locked) Error() string {
	return fmt.Sprintf("too many wrong passphrases: try again in %s", e.Wait.Round(time.Second))
}

// Vault holds the keys themselves, each usable only with its PIN: the TPM
// (hermesh-signer), or software keys in tests (SoftVault).
type Vault interface {
	// Create makes a key that only pin opens, and names it.
	Create(pin string) (name string, pub *ecdsa.PublicKey, err error)
	// Public is the public half of the key name.
	Public(name string) (*ecdsa.PublicKey, error)
	// Sign signs each message with the key name: ECDSA P-256 over
	// SHA-256, ASN.1.
	Sign(name, pin string, messages [][]byte) ([][]byte, error)
	// Delete deletes the key name; pin is empty when the signer has no
	// PIN at hand (passphrase mode).
	Delete(name, pin string) error
}

// Keys are root keys in a Vault, each with its PIN in Dir: signer.Keys.
//
//	<key id>.json   its name in the vault, its mode and, in Hello mode, its PIN
//	<key id>.pin    passphrase mode: its PIN, sealed with the passphrase
//	<key id>.tries  the wrong passphrases in a row, and when the last was typed
type Keys struct {
	Vault Vault
	Dir   string
	Now   func() time.Time // nil: time.Now

	mu   sync.Mutex
	open map[string]string // PINs Unlock unsealed, by key id, for the Sign that follows
}

var _ signer.Keys = (*Keys)(nil)

// record is what opens one root key. A record from before passphrase mode
// has no mode: Hello.
type record struct {
	Name string `json:"name"`
	PIN  string `json:"pin,omitempty"`
	Mode Mode   `json:"mode,omitempty"`
}

func (k *Keys) path(keyID, ext string) string { return filepath.Join(k.Dir, keyID+ext) }

func (k *Keys) load(keyID string) (record, error) {
	var rec record
	b, err := readFile(k.path(keyID, ".json"))
	if err != nil {
		return rec, fmt.Errorf("this PC has no root key %s", keyID)
	}
	if err := json.Unmarshal(b, &rec); err != nil {
		return rec, err
	}
	if rec.Mode == "" {
		rec.Mode = Hello
	}
	return rec, nil
}

func (k *Keys) save(keyID string, rec record) error {
	if rec.Mode == Hello {
		rec.Mode = "" // as records were before passphrase mode
	}
	b, _ := json.Marshal(rec)
	return writeFile(k.path(keyID, ".json"), b)
}

// writeFile replaces path at once, so a record is never half written.
func writeFile(path string, b []byte) error {
	if err := os.WriteFile(path+".tmp", b, 0o600); err != nil {
		return err
	}
	return retry(func() error { return os.Rename(path+".tmp", path) })
}

// remove deletes path, if it is there.
func remove(path string) { retry(func() error { return os.Remove(path) }) }

func (k *Keys) now() time.Time {
	if k.Now != nil {
		return k.Now()
	}
	return time.Now()
}

// Create makes a new root key, in Windows Hello mode.
func (k *Keys) Create(context.Context) (*ecdsa.PublicKey, error) {
	b := make([]byte, 32)
	rand.Read(b)
	pin := hex.EncodeToString(b)
	name, pub, err := k.Vault.Create(pin)
	if err != nil {
		return nil, err
	}
	return pub, k.save(identity.KeyID(pub), record{Name: name, PIN: pin, Mode: Hello})
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// Sign signs with the root key keyID: in passphrase mode, only right after
// Unlock.
func (k *Keys) Sign(_ context.Context, keyID string, messages [][]byte) ([][]byte, error) {
	rec, err := k.load(keyID)
	if err != nil {
		return nil, err
	}
	pin := rec.PIN
	if rec.Mode == Passphrase {
		k.mu.Lock()
		p, ok := k.open[keyID]
		delete(k.open, keyID)
		k.mu.Unlock()
		if !ok {
			return nil, ErrNotUnlocked
		}
		pin = p
	}
	return k.Vault.Sign(rec.Name, pin, messages)
}

// Delete deletes the root key keyID and what opens it.
func (k *Keys) Delete(keyID string) error {
	rec, err := k.load(keyID)
	if err == nil {
		err = k.Vault.Delete(rec.Name, rec.PIN)
	}
	for _, ext := range []string{".json", ".pin", ".tries"} {
		remove(k.path(keyID, ext))
	}
	return err
}

// Mode is how the root key keyID is unlocked.
func (k *Keys) Mode(keyID string) (Mode, error) {
	rec, err := k.load(keyID)
	return rec.Mode, err
}

// Unlock opens the root key keyID, in passphrase mode, for the Sign that
// follows. A wrong passphrase is ErrPassphrase; after too many, *Locked
// says how long until the next one is tried.
func (k *Keys) Unlock(keyID, passphrase string) error {
	pin, err := k.unseal(keyID, passphrase)
	if err != nil {
		return err
	}
	k.mu.Lock()
	if k.open == nil {
		k.open = map[string]string{}
	}
	k.open[keyID] = pin
	k.mu.Unlock()
	return nil
}

// UsePassphrase switches the root key keyID to passphrase mode, sealing its
// PIN with passphrase, once hello (Windows Hello, the mode it leaves)
// verified the owner. It reports whether they were.
func (k *Keys) UsePassphrase(keyID, passphrase string, hello func() (bool, error)) (bool, error) {
	rec, err := k.load(keyID)
	if err != nil {
		return false, err
	}
	if rec.Mode != Hello {
		return false, fmt.Errorf("the key %s is unlocked with its passphrase already", keyID)
	}
	if len([]rune(passphrase)) < MinPassphrase {
		return false, ErrShort
	}
	if ok, err := hello(); !ok || err != nil {
		return false, err
	}
	pub, err := k.Vault.Public(rec.Name)
	if err != nil {
		return false, err
	}
	if identity.KeyID(pub) != keyID {
		return false, fmt.Errorf("the vault's key %s is not %s", rec.Name, keyID)
	}
	// The sealed PIN first, then the record without it: a switch cut short
	// leaves the key in Windows Hello mode.
	remove(k.path(keyID, ".tries"))
	if err := devicekey.Seal(k.path(keyID, ".pin"), passphrase, []byte(rec.PIN), identity.MarshalPublicKey(pub)); err != nil {
		return false, err
	}
	return true, k.save(keyID, record{Name: rec.Name, Mode: Passphrase})
}

// UseHello switches the root key keyID back to Windows Hello mode, with its
// passphrase (ErrPassphrase and *Locked as for Unlock).
func (k *Keys) UseHello(keyID, passphrase string) error {
	pin, err := k.unseal(keyID, passphrase)
	if err != nil {
		return err
	}
	rec, err := k.load(keyID)
	if err != nil {
		return err
	}
	if err := k.save(keyID, record{Name: rec.Name, PIN: pin}); err != nil {
		return err
	}
	remove(k.path(keyID, ".pin"))
	return nil
}

// unseal opens keyID's sealed PIN with passphrase, counting wrong ones.
func (k *Keys) unseal(keyID, passphrase string) (string, error) {
	rec, err := k.load(keyID)
	if err != nil {
		return "", err
	}
	if rec.Mode != Passphrase {
		return "", fmt.Errorf("the key %s is unlocked with Windows Hello", keyID)
	}
	t := k.tries(keyID)
	if wait := t.wait(k.now()); wait > 0 {
		return "", &Locked{Wait: wait}
	}
	// Counted before it is tried, as a TPM counts a PIN: a try cut short
	// still counts.
	if err := writeFile(k.path(keyID, ".tries"), mustJSON(tries{Wrong: t.Wrong + 1, Last: k.now()})); err != nil {
		return "", err
	}
	pin, pub, err := devicekey.Unseal(k.path(keyID, ".pin"), passphrase)
	if err != nil {
		return "", err
	}
	if p, err := identity.ParsePublicKey(pub); err != nil || identity.KeyID(p) != keyID {
		return "", fmt.Errorf("the sealed PIN is another key's than %s", keyID)
	}
	remove(k.path(keyID, ".tries"))
	return string(pin), nil
}

// Wait is how long until a passphrase for keyID is tried again.
func (k *Keys) Wait(keyID string) time.Duration { return k.tries(keyID).wait(k.now()) }

func (k *Keys) tries(keyID string) tries {
	var t tries
	if b, err := readFile(k.path(keyID, ".tries")); err == nil {
		json.Unmarshal(b, &t)
	}
	return t
}

// tries are the wrong passphrases in a row. Modeled on the TPM's
// dictionary-attack lockout: the count is kept on disk, where only
// administrators write, so restarting the signer or the PC resets nothing;
// while it must wait, a passphrase is not even tried; only the right
// passphrase starts the count again.
type tries struct {
	Wrong int       `json:"wrong"`
	Last  time.Time `json:"last"`
}

// The first freeTries wrong passphrases cost nothing; each one after that
// doubles the wait before the next, from firstWait up to maxWait.
const (
	freeTries = 5
	firstWait = 30 * time.Second
	maxWait   = time.Hour
)

// Delay is the wait after wrong wrong passphrases in a row.
func Delay(wrong int) time.Duration {
	if wrong < freeTries {
		return 0
	}
	if n := wrong - freeTries; n < 7 {
		return min(firstWait<<n, maxWait)
	}
	return maxWait
}

func (t tries) wait(now time.Time) time.Duration {
	d := Delay(t.Wrong)
	left := t.Last.Add(d).Sub(now)
	// A clock set back doesn't make it longer than the whole wait.
	return max(0, min(left, d))
}
