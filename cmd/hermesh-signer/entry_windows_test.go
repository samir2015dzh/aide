package main

import (
	"context"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/hermeshai/hermesh/internal/signer/unlock"
	"github.com/hermeshai/hermesh/pkg/identity"
)

// passphraseKey is a root key in passphrase mode, with "correct horse".
func passphraseKey(t *testing.T) (*unlock.Keys, string) {
	t.Helper()
	keys := &unlock.Keys{Vault: &unlock.SoftVault{}, Dir: t.TempDir()}
	pub, err := keys.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	id := identity.KeyID(pub)
	if ok, err := keys.UsePassphrase(id, "correct horse", func() (bool, error) { return true, nil }); !ok || err != nil {
		t.Fatal(ok, err)
	}
	return keys, id
}

// overPanel waits for a window of p's other than under to show.
func overPanel(t *testing.T, p *drawnPanel, under *view) *view {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		p.mu.Lock()
		v := p.open
		p.mu.Unlock()
		if v != nil && v != under {
			if r, _, _ := procIsWindowVisible.Call(v.window()); r != 0 {
				time.Sleep(200 * time.Millisecond)
				return v
			}
		}
	}
	t.Fatal("the window did not show")
	return nil
}

func typeIn(v *view, s string) {
	for _, u := range utf16.Encode([]rune(s)) {
		post(v, wmChar, uintptr(u), 0)
	}
}

// eventuallyTrue waits for f, on the test's side of the window.
func eventuallyTrue(t *testing.T, what string, f func() bool) {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); !f(); time.Sleep(50 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatal(what)
		}
	}
}

// TestDrawnPanelPassphrase: in passphrase mode the panel shows the same
// batch, and Approve asks for the passphrase in the signer's own window,
// not Windows Hello; a wrong one stays to try again, the right one opens
// the key for the signature.
func TestDrawnPanelPassphrase(t *testing.T) {
	keys, id := passphraseKey(t)
	p := testPanel(false, func() (bool, error) {
		t.Error("Windows Hello asked in passphrase mode")
		return false, nil
	})
	p.keys = keys
	v := &view{p: p, cards: grantBatch, focus: "deny", keyID: id}
	done := make(chan struct{})
	go func() { onOwnThread(func() { v.show() }); close(done) }()
	shown(t, p)
	shot(t, v, "grant-passphrase")
	click(v, "approve")
	e := overPanel(t, p, v)
	shot(t, e, "passphrase")
	typeIn(e, "wrong horse")
	key(e, 0x0D)
	eventuallyTrue(t, "a wrong passphrase not told", func() bool { return e.problem == p.w.WrongPassphrase })
	shot(t, e, "passphrase-wrong")
	typeIn(e, "correct horse")
	key(e, 0x0D)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the panel did not answer")
	}
	if !v.ok || v.err != nil {
		t.Fatalf("approved: %v %v", v.ok, v.err)
	}
	if _, err := keys.Sign(context.Background(), id, [][]byte{[]byte("m")}); err != nil {
		t.Fatalf("the key, unlocked: %v", err)
	}
}

// TestUnlockSwitch: choosing a passphrase takes it twice and Windows
// Hello; going back takes the passphrase, and wrong ones lock it a while.
func TestUnlockSwitch(t *testing.T) {
	keys := &unlock.Keys{Vault: &unlock.SoftVault{}, Dir: t.TempDir()}
	pub, _ := keys.Create(context.Background())
	id := identity.KeyID(pub)
	asked := 0
	p := testPanel(false, func() (bool, error) { asked++; return true, nil })
	p.keys = keys

	result := make(chan answer, 1)
	go onOwnThread(func() {
		ok, err := p.usePassphrase(id)
		result <- answer{ok, err}
	})
	e := shown(t, p)
	shot(t, e, "use-passphrase")
	typeIn(e, "correct horse")
	key(e, 0x0D) // to the next field
	typeIn(e, "correct house")
	key(e, 0x0D)
	eventuallyTrue(t, "a mismatch not told", func() bool { return e.problem == p.w.Mismatch })
	typeIn(e, "correct horse")
	key(e, 0x09) // Tab
	typeIn(e, "correct horse")
	click(e, "submit")
	if a := answered(t, result); !a.ok || a.err != nil || asked != 1 {
		t.Fatalf("switched: %+v, Windows Hello asked %d times", a, asked)
	}
	if m, _ := keys.Mode(id); m != unlock.Passphrase {
		t.Fatalf("mode %s", m)
	}

	now := time.Now()
	keys.Now = func() time.Time { return now }
	go onOwnThread(func() {
		ok, err := p.useHello(id)
		result <- answer{ok, err}
	})
	e = shown(t, p)
	for range 5 {
		typeIn(e, "wrong")
		key(e, 0x0D)
		time.Sleep(200 * time.Millisecond)
		eventuallyTrue(t, "not checked", func() bool { return !e.waiting }) // Argon2id takes a moment
	}
	eventuallyTrue(t, "not locked", func() bool { return e.lockLeft() > 0 })
	shot(t, e, "use-hello-locked")
	now = now.Add(time.Minute)
	typeIn(e, "correct horse")
	key(e, 0x0D)
	if a := answered(t, result); !a.ok || a.err != nil {
		t.Fatalf("back to Windows Hello: %+v", a)
	}
	if m, _ := keys.Mode(id); m != unlock.Hello {
		t.Fatalf("mode %s", m)
	}
}
