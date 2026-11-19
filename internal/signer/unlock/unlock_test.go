package unlock

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hermeshai/hermesh/pkg/identity"
)

func newKey(t *testing.T, k *Keys) string {
	t.Helper()
	pub, err := k.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return identity.KeyID(pub)
}

func signs(t *testing.T, k *Keys, keyID string) error {
	t.Helper()
	sigs, err := k.Sign(context.Background(), keyID, [][]byte{[]byte("m")})
	if err != nil {
		return err
	}
	pub, _ := k.Vault.Public(mustLoad(t, k, keyID).Name)
	if err := identity.VerifyRaw(pub, []byte("m"), sigs[0]); err != nil {
		t.Fatalf("the signature: %v", err)
	}
	return nil
}

func mustLoad(t *testing.T, k *Keys, keyID string) record {
	t.Helper()
	rec, err := k.load(keyID)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

var (
	verified = func() (bool, error) { return true, nil }
	declined = func() (bool, error) { return false, nil }
)

// TestModes: a new key is in Windows Hello mode; switching to a passphrase
// takes Windows Hello and seals the same key's PIN, after which it signs
// only right after the passphrase; switching back takes the passphrase.
func TestModes(t *testing.T) {
	k := &Keys{Vault: &SoftVault{}, Dir: t.TempDir()}
	id := newKey(t, k)
	if m, _ := k.Mode(id); m != Hello {
		t.Fatalf("a new key: %s", m)
	}
	if b, _ := os.ReadFile(k.path(id, ".json")); strings.Contains(string(b), "mode") {
		t.Fatalf("a Hello record as before passphrase mode: %s", b)
	}
	if err := signs(t, k, id); err != nil {
		t.Fatal(err)
	}
	pin := mustLoad(t, k, id).PIN

	if _, err := k.UsePassphrase(id, "short", verified); !errors.Is(err, ErrShort) {
		t.Fatalf("a short passphrase: %v", err)
	}
	if ok, err := k.UsePassphrase(id, "correct horse", declined); ok || err != nil {
		t.Fatalf("Windows Hello declined: %v %v", ok, err)
	}
	if m, _ := k.Mode(id); m != Hello {
		t.Fatalf("switched without Windows Hello: %s", m)
	}
	if ok, err := k.UsePassphrase(id, "correct horse", verified); !ok || err != nil {
		t.Fatalf("switch: %v %v", ok, err)
	}
	if m, _ := k.Mode(id); m != Passphrase {
		t.Fatalf("after the switch: %s", m)
	}
	for _, ext := range []string{".json", ".pin"} {
		if b, _ := os.ReadFile(k.path(id, ext)); strings.Contains(string(b), pin) {
			t.Fatalf("%s holds the PIN in the clear", ext)
		}
	}
	if _, err := k.UsePassphrase(id, "another one", verified); err == nil {
		t.Fatal("switched to a passphrase twice")
	}

	if err := signs(t, k, id); !errors.Is(err, ErrNotUnlocked) {
		t.Fatalf("signed without the passphrase: %v", err)
	}
	if err := k.Unlock(id, "wrong horse"); !errors.Is(err, ErrPassphrase) {
		t.Fatalf("a wrong passphrase: %v", err)
	}
	if err := signs(t, k, id); !errors.Is(err, ErrNotUnlocked) {
		t.Fatalf("signed after a wrong passphrase: %v", err)
	}
	if err := k.Unlock(id, "correct horse"); err != nil {
		t.Fatal(err)
	}
	if err := signs(t, k, id); err != nil {
		t.Fatalf("signed with the passphrase: %v", err)
	}
	if err := signs(t, k, id); !errors.Is(err, ErrNotUnlocked) {
		t.Fatalf("one passphrase signed twice: %v", err)
	}

	if err := k.UseHello(id, "wrong horse"); !errors.Is(err, ErrPassphrase) {
		t.Fatalf("back with a wrong passphrase: %v", err)
	}
	if err := k.UseHello(id, "correct horse"); err != nil {
		t.Fatal(err)
	}
	if rec := mustLoad(t, k, id); rec.Mode != Hello || rec.PIN != pin {
		t.Fatalf("back in Windows Hello mode: %+v", rec)
	}
	if _, err := os.Stat(k.path(id, ".pin")); !os.IsNotExist(err) {
		t.Fatal("the sealed PIN stayed")
	}
	if err := signs(t, k, id); err != nil {
		t.Fatal(err)
	}
	if err := k.Unlock(id, "correct horse"); err == nil {
		t.Fatal("unlocked a key in Windows Hello mode with a passphrase")
	}

	// Deleted in passphrase mode: without its PIN, and all of it.
	k.UsePassphrase(id, "correct horse", verified)
	if err := k.Delete(id); err != nil {
		t.Fatal(err)
	}
	if left, _ := os.ReadDir(k.Dir); len(left) != 0 {
		t.Fatalf("left: %v", left)
	}
}

// TestOldRecord: a key made before passphrase mode is in Windows Hello mode.
func TestOldRecord(t *testing.T) {
	v := &SoftVault{}
	name, pub, _ := v.Create("0123")
	k := &Keys{Vault: v, Dir: t.TempDir()}
	id := identity.KeyID(pub)
	os.WriteFile(k.path(id, ".json"), []byte(`{"name":"`+name+`","pin":"0123"}`), 0o600)
	if m, err := k.Mode(id); m != Hello || err != nil {
		t.Fatalf("mode %s: %v", m, err)
	}
	if err := signs(t, k, id); err != nil {
		t.Fatal(err)
	}
}

// TestLockout: wrong passphrases wait longer and longer, the right one is
// not even tried meanwhile, a restart keeps the count, and only the right
// passphrase clears it.
func TestLockout(t *testing.T) {
	now := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	dir := t.TempDir()
	v := &SoftVault{}
	k := &Keys{Vault: v, Dir: dir, Now: clock}
	id := newKey(t, k)
	k.UsePassphrase(id, "correct horse", verified)

	for i := range 5 {
		if err := k.Unlock(id, "wrong"); !errors.Is(err, ErrPassphrase) {
			t.Fatalf("wrong passphrase %d: %v", i+1, err)
		}
	}
	if w := k.Wait(id); w != 30*time.Second {
		t.Fatalf("after five wrong: wait %v", w)
	}
	var locked *Locked
	if err := k.Unlock(id, "correct horse"); !errors.As(err, &locked) || locked.Wait != 30*time.Second {
		t.Fatalf("the right passphrase while it waits: %v", err)
	}
	if err := k.UseHello(id, "correct horse"); !errors.As(err, &locked) {
		t.Fatalf("switching back while it waits: %v", err)
	}

	// The signer restarts: the count stays.
	k = &Keys{Vault: v, Dir: dir, Now: clock}
	now = now.Add(20 * time.Second)
	if w := k.Wait(id); w != 10*time.Second {
		t.Fatalf("after a restart: wait %v", w)
	}
	now = now.Add(10 * time.Second)
	if err := k.Unlock(id, "wrong"); !errors.Is(err, ErrPassphrase) {
		t.Fatal(err)
	}
	if w := k.Wait(id); w != time.Minute {
		t.Fatalf("after six wrong: wait %v", w)
	}
	// A clock set back doesn't make the wait longer.
	now = now.Add(-24 * time.Hour)
	if w := k.Wait(id); w != time.Minute {
		t.Fatalf("clock set back: wait %v", w)
	}
	now = now.Add(24*time.Hour + time.Minute)
	if err := k.Unlock(id, "correct horse"); err != nil {
		t.Fatalf("the right passphrase after the wait: %v", err)
	}
	if err := k.Unlock(id, "wrong"); !errors.Is(err, ErrPassphrase) || k.Wait(id) != 0 {
		t.Fatalf("the count started again: %v, wait %v", err, k.Wait(id))
	}
}

func TestDelay(t *testing.T) {
	for wrong, want := range map[int]time.Duration{0: 0, 4: 0, 5: 30 * time.Second, 6: time.Minute, 7: 2 * time.Minute,
		11: 32 * time.Minute, 12: time.Hour, 40: time.Hour, 1 << 20: time.Hour} {
		if got := Delay(wrong); got != want {
			t.Errorf("Delay(%d) = %v, want %v", wrong, got, want)
		}
	}
}

// TestCountedWhileRead: the signer's window reads the count of wrong
// passphrases each time it draws, and its spinner draws while one is
// checked; counting that one, or clearing the count after the right one,
// must not fail meanwhile. On Windows it did ("Access is denied"): a file
// open to read could not be replaced or deleted, and the window closed on
// the error.
func TestCountedWhileRead(t *testing.T) {
	k := &Keys{Vault: &SoftVault{}, Dir: t.TempDir()}
	id := newKey(t, k)
	k.UsePassphrase(id, "correct horse", verified)

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { // the window, drawing
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				k.Wait(id)
			}
		}
	}()
	defer func() { close(stop); <-done }()

	path := k.path(id, ".tries")
	for i := range 300 {
		if err := writeFile(path, mustJSON(tries{Wrong: 1, Last: time.Now()})); err != nil {
			t.Fatalf("count %d: %v", i, err)
		}
		if i%3 == 0 {
			remove(path)
			if _, err := os.Stat(path); err == nil {
				t.Fatalf("count %d not cleared", i)
			}
		}
	}
	remove(path)
	for i := range 4 {
		if err := k.Unlock(id, "wrong"); !errors.Is(err, ErrPassphrase) {
			t.Fatalf("wrong passphrase %d: %v", i+1, err)
		}
	}
	if err := k.Unlock(id, "correct horse"); err != nil {
		t.Fatalf("the right passphrase: %v", err)
	}
	if k.Wait(id) != 0 || k.tries(id).Wrong != 0 {
		t.Fatalf("the count was not cleared: %+v", k.tries(id))
	}
}
