package machine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"golang.org/x/sys/unix"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
)

// A session another DSH process holds (it flocks session.lock, as DSH
// does), and DSH Desktop does not run: nothing is sent, and the owner is
// told where it is open. Held by DSH Desktop itself, it goes.
func TestDSHTakeoverHeldElsewhere(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DSH_HOME", home)
	h := newHarness(t, Limits{})
	writeDSHSession(t, home, "owners", "/home/alex/app", 1)
	matches, _ := filepath.Glob(filepath.Join(home, "sessions", "*", "owners"))
	f, err := os.Create(filepath.Join(matches[0], "session.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	b := startFakeBridge(t, home)
	h.n.SetDSHTakeover(&machinev1.SetDSHTakeoverRequest{SessionId: "owners", On: true})
	send := func() error {
		_, err := h.n.SendDSHMessage(context.Background(), &machinev1.SendDSHMessageRequest{SessionId: "owners", Text: "hi"})
		return err
	}
	if err := send(); connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "another DSH program") || len(b.prompts) != 0 {
		t.Fatalf("held elsewhere: %v %v", err, b.prompts)
	}
	b.live["owners"] = true // DSH Desktop is the holder
	if err := send(); err != nil || len(b.prompts) != 1 {
		t.Fatalf("held by DSH Desktop: %v %v", err, b.prompts)
	}
}
