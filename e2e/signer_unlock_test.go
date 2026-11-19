package e2e

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	localv1 "github.com/hermeshai/hermesh/gen/go/hermesh/local/v1"
	"github.com/hermeshai/hermesh/internal/preview"
	"github.com/hermeshai/hermesh/internal/signer"
	"github.com/hermeshai/hermesh/internal/signer/unlock"
	"github.com/hermeshai/hermesh/pkg/identity"
)

// typingOwner plays the owner at the Windows signer's panel: it approves
// every batch, confirming with Windows Hello, or, for a root key in
// passphrase mode, by typing the passphrases it was given, in turn, until
// one opens the key; out of them, it closes the panel.
type typingOwner struct {
	keys    *unlock.Keys
	matched chan preview.Match

	mu     sync.Mutex
	hellos int
	typed  []string
	tried  []error // what each passphrase typed did
}

func (o *typingOwner) Approve(ctx context.Context, cards []preview.Card) (bool, error) {
	keyID := signer.RootKeyID(ctx)
	mode, err := o.keys.Mode(keyID)
	if err != nil {
		return false, err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if mode == unlock.Hello {
		o.hellos++
		return true, nil
	}
	for len(o.typed) > 0 {
		p := o.typed[0]
		o.typed = o.typed[1:]
		err := o.keys.Unlock(keyID, p)
		o.tried = append(o.tried, err)
		if err == nil {
			return true, nil
		}
	}
	return false, nil
}

func (o *typingOwner) Failed(error)            {}
func (o *typingOwner) Show()                   {}
func (o *typingOwner) Matched(m preview.Match) { o.matched <- m }

// typing gives the owner passphrases to type, and takes what the last ones did.
func (o *typingOwner) typing(p ...string) (tried []error, hellos int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	tried, o.typed, o.tried = o.tried, p, nil
	return tried, o.hellos
}

// TestSignerPassphrase: a Windows trusted device whose owner switched its
// root key from Windows Hello to a passphrase approves a device joining
// with the passphrase, through hermeshd and the relay as on a real PC
// (the signer's core, internal/signer/unlock, with software keys in place
// of the TPM, and the owner typing in place of the panel). Wrong
// passphrases are refused and then wait, and restarting the signer
// doesn't end the wait; the same key signs throughout, so nothing paired
// before changes; switching back to Windows Hello needs the passphrase
// once, and Windows Hello confirms again after (docs/windows.md §7.5).
func TestSignerPassphrase(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	bin := binaries(t)
	tmp := t.TempDir()
	port := freePort(t)
	relayData := filepath.Join(tmp, "relay")
	relay := start(t, "relay", nil, filepath.Join(bin, "hermesh-relay"), "serve", "--data-dir", relayData, "--listen", fmt.Sprintf("127.0.0.1:%d", port))
	eventually(t, 20*time.Second, "relay up", func() bool { return strings.Contains(relay.output(), "relay listening") })
	claim := strings.TrimSpace(run(t, nil, 10*time.Second, filepath.Join(bin, "hermesh-relay"), "claim-code", "--data-dir", relayData).stdout)
	sockDir, _ := os.MkdirTemp(shortTemp, "hm")
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	sock := filepath.Join(sockDir, "o.sock")
	start(t, "owner", nil, filepath.Join(bin, "hermeshd"), "run", "--owner", "--no-machine", "--data-dir", filepath.Join(tmp, "owner"), "--socket", sock)
	c := localClient(t, sock)
	ctx := context.Background()

	var mu sync.Mutex
	now := time.Now()
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	vault, dir := &unlock.SoftVault{}, filepath.Join(tmp, "keys")
	os.MkdirAll(dir, 0o700)
	o := &typingOwner{matched: make(chan preview.Match, 1)}
	// startSigner runs the signer as its task does, until stop: each time a
	// new process, keeping only what is on disk (and in the TPM).
	startSigner := func() (stop func()) {
		keys := &unlock.Keys{Vault: vault, Dir: dir, Now: clock}
		o.mu.Lock()
		o.keys = keys
		o.mu.Unlock()
		ctx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			signer.Run(ctx, c, keys, o)
			close(done)
		}()
		return func() { cancel(); <-done }
	}
	stop := startSigner()
	defer func() { stop() }()
	eventually(t, 20*time.Second, "the signer watching", func() bool {
		st, err := c.GetStatus(ctx, &localv1.GetStatusRequest{})
		return err == nil && st.Signer.GetConnected()
	})

	// Set up in Windows Hello mode, the default.
	if _, err := c.Setup(ctx, &localv1.SetupRequest{RelayUrl: fmt.Sprintf("http://127.0.0.1:%d", port), ClaimCode: claim, RootLabel: "PC", RootPlatform: "windows"}); err != nil {
		t.Fatalf("setup: %v", err)
	}
	st, err := c.GetStatus(ctx, &localv1.GetStatusRequest{})
	if err != nil || !st.Initialized {
		t.Fatalf("after setup: %v %v", st, err)
	}
	keyID := st.DeviceKeyId
	if _, hellos := o.typing(); hellos == 0 {
		t.Fatal("setup was not confirmed with Windows Hello")
	}

	// The owner switches to a passphrase: Windows Hello confirms it once
	// (hermesh-signer unlock --to passphrase).
	o.mu.Lock()
	keys := o.keys
	o.mu.Unlock()
	hello := 0
	if ok, err := keys.UsePassphrase(keyID, "correct horse", func() (bool, error) { hello++; return true, nil }); !ok || err != nil || hello != 1 {
		t.Fatalf("switch to a passphrase: %v %v, Windows Hello %d", ok, err, hello)
	}

	// An iPhone asks to join.
	sock2 := filepath.Join(sockDir, "o2.sock")
	start(t, "owner iphone", nil, filepath.Join(bin, "hermeshd"), "run", "--owner", "--no-machine", "--data-dir", filepath.Join(tmp, "iphone"), "--socket", sock2)
	root2, _ := identity.GenerateKey()
	phone := newTestApp(t, sock2, root2)
	join, err := phone.client.RequestJoin(ctx, &localv1.RequestJoinRequest{RelayUrl: fmt.Sprintf("http://127.0.0.1:%d", port), OwnerId: st.OwnerId,
		RootPublicKey: rootPub(root2), RootLabel: "iPhone", RootPlatform: "ios"})
	if err != nil {
		t.Fatalf("request join: %v", err)
	}
	eventually(t, 15*time.Second, "the join request here", func() bool {
		res, err := c.ListPairingRequests(ctx, &localv1.ListPairingRequestsRequest{})
		return err == nil && len(res.Pairings) == 1 && res.Pairings[0].Code == join.Code
	})

	// Five wrong passphrases, then the right one waits: nothing is signed.
	o.typing("wrong", "wrong", "wrong", "wrong", "wrong", "correct horse")
	if _, err := c.ApproveDevicePairing(ctx, &localv1.ApproveDevicePairingRequest{Code: join.Code}); err == nil {
		t.Fatal("approved with wrong passphrases")
	}
	tried, hellos := o.typing()
	var locked *unlock.Locked
	if len(tried) != 6 || !errors.As(tried[5], &locked) {
		t.Fatalf("tried: %v", tried)
	}
	for _, err := range tried[:5] {
		if !errors.Is(err, unlock.ErrPassphrase) {
			t.Fatalf("tried: %v", tried)
		}
	}

	// The signer restarts (the PC did): the wait goes on.
	stop()
	stop = startSigner()
	o.typing("correct horse")
	if _, err := c.ApproveDevicePairing(ctx, &localv1.ApproveDevicePairingRequest{Code: join.Code}); err == nil {
		t.Fatal("approved while it waits, after a restart")
	}
	if tried, _ := o.typing(); len(tried) != 1 || !errors.As(tried[0], &locked) {
		t.Fatalf("after a restart: %v", tried)
	}

	// After the wait, the right passphrase approves, without Windows Hello.
	mu.Lock()
	now = now.Add(unlock.Delay(5))
	mu.Unlock()
	o.typing("correct horse")
	approval, err := c.ApproveDevicePairing(ctx, &localv1.ApproveDevicePairingRequest{Code: join.Code})
	if err != nil {
		t.Fatalf("approve with the passphrase: %v", err)
	}
	if tried, h := o.typing(); len(tried) != 1 || tried[0] != nil || h != hellos {
		t.Fatalf("approved with %v, Windows Hello %d times more", tried, h-hellos)
	}
	var m preview.Match
	select {
	case m = <-o.matched:
	case <-time.After(10 * time.Second):
		t.Fatal("no digits shown")
	}
	if m.Digits != approval.MatchCode || m.Label != "iPhone" {
		t.Fatalf("the signer shows %+v; hermeshd says %s", m, approval.MatchCode)
	}
	eventually(t, 60*time.Second, "the approval reaches the iPhone", func() bool {
		w, err := phone.client.WaitJoin(ctx, &localv1.WaitJoinRequest{Code: join.Code})
		return err == nil && w.Done
	})
	if _, err := phone.client.ConfirmJoin(ctx, &localv1.ConfirmJoinRequest{Code: join.Code, Accept: true, MatchCode: m.Digits}); err != nil {
		t.Fatalf("the digits the signer showed: %v", err)
	}
	// The same root key throughout: this PC is still the device it was.
	devices, err := c.ListDevices(ctx, &localv1.ListDevicesRequest{})
	if err != nil || len(devices.Devices) != 2 {
		t.Fatalf("devices: %v %v", devices, err)
	}
	for _, d := range devices.Devices {
		if d.ThisDevice && d.KeyId != keyID {
			t.Fatalf("this PC's key changed: %s, was %s", d.KeyId, keyID)
		}
	}

	// Back to Windows Hello, with the passphrase; then removing the iPhone
	// asks Windows Hello, and no passphrase.
	o.mu.Lock()
	keys = o.keys
	o.mu.Unlock()
	if err := keys.UseHello(keyID, "correct horse"); err != nil {
		t.Fatalf("back to Windows Hello: %v", err)
	}
	for _, d := range devices.Devices {
		if !d.ThisDevice {
			if _, err := c.RemoveDevice(ctx, &localv1.RemoveDeviceRequest{KeyId: d.KeyId}); err != nil {
				t.Fatalf("remove the iPhone: %v", err)
			}
		}
	}
	if tried, h := o.typing(); len(tried) != 0 || h == hellos {
		t.Fatalf("after switching back: passphrases %v, Windows Hello %d times more", tried, h-hellos)
	}
}
