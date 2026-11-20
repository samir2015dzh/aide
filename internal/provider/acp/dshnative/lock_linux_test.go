package dshnative

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
)

// A session whose session.lock a process flocks, as DSH does while it has
// the session open, is running or idle by its last turn; the probe leaves
// the lock as it is.
func TestLockProbeLinux(t *testing.T) {
	home := t.TempDir()
	idle := writeSession(t, home, map[string]any{"id": "idle"}, true, conversation(0, 1, "a", "b"))
	running := writeSession(t, home, map[string]any{"id": "running"}, true, events(0, ev("turn/start", map[string]any{"turn": 1}), userMsg("Go")))
	writeSession(t, home, map[string]any{"id": "closed"}, true, conversation(0, 1, "a", "b"))
	os.WriteFile(filepath.Join(home, "sessions", "_no-cwd", "closed", "session.lock"), nil, 0o600) // DSH had it open once
	for _, dir := range []string{idle, running} {
		f, err := os.Create(filepath.Join(dir, "session.lock"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
			t.Fatal(err)
		}
	}
	list, err := List(home, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]machinev1.DSHSessionState{"idle": machinev1.DSHSessionState_DSH_SESSION_STATE_IDLE,
		"running": machinev1.DSHSessionState_DSH_SESSION_STATE_RUNNING, "closed": machinev1.DSHSessionState_DSH_SESSION_STATE_ENDED}
	for _, s := range list {
		if s.State != want[s.SessionId] {
			t.Errorf("%s: %v", s.SessionId, s.State)
		}
	}
	// Still held by its holder alone: another can't take it.
	g, _ := os.Open(filepath.Join(idle, "session.lock"))
	defer g.Close()
	if err := unix.Flock(int(g.Fd()), unix.LOCK_EX|unix.LOCK_NB); err == nil {
		t.Fatal("the lock was let go")
	}
}
