package e2e

import (
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
)

// TestSessionPorts: sessions that run at the same time get port blocks of
// their own, for their dev servers; a session keeps its block when its
// agent starts again after aided restarts, and an archived session's
// block goes to the next session.
func TestSessionPorts(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	w := newOwnerMac(t, []string{"AIDE_FAKE_PROVIDERS=1"}, &localv1.GrantSpec{})
	// value is the variable's value in the agent's environment, from the
	// turn's reply "name=value".
	value := func(s sessionJSON, r result, name string) string {
		t.Helper()
		v, ok := strings.CutPrefix(s.Result, name+"=")
		if r.code != 0 || s.Status != "idle" || !ok {
			t.Fatalf("%s: %+v %+v", name, s, r)
		}
		return v
	}
	env := func(id, name string) string {
		t.Helper()
		s, r := w.send(time.Minute, id, "env "+name)
		return value(s, r, name)
	}

	// Two sessions started together, both turns running at once.
	var wg sync.WaitGroup
	ss := make([]sessionJSON, 2)
	rs := make([]result, 2)
	for i := range ss {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ss[i], rs[i] = w.startSession(time.Minute, "--wait", "--prompt", "sleep 1s\nenv AIDE_PORT_BASE")
		}()
	}
	wg.Wait()
	base := map[string]int{}
	for i, s := range ss {
		b, err := strconv.Atoi(value(s, rs[i], "AIDE_PORT_BASE"))
		if err != nil || b < 1024 || b+10 > 32768 {
			t.Fatalf("AIDE_PORT_BASE=%d (%v): outside 1024-32767", b, err)
		}
		if n := env(s.SessionID, "AIDE_PORT_COUNT"); n != "10" {
			t.Fatalf("AIDE_PORT_COUNT=%q", n)
		}
		if wt := env(s.SessionID, "AIDE_WORKTREE"); wt != w.worktree(s.SessionID) {
			t.Fatalf("AIDE_WORKTREE=%q, want %q", wt, w.worktree(s.SessionID))
		}
		base[s.SessionID] = b
	}
	a, b := base[ss[0].SessionID], base[ss[1].SessionID]
	if a < b+10 && b < a+10 {
		t.Fatalf("overlapping blocks: %d+10 and %d+10", a, b)
	}

	// aided restarts: the next turn's agent starts again, with the same block.
	w.restartOwner(false)
	for _, s := range ss {
		if got := env(s.SessionID, "AIDE_PORT_BASE"); got != strconv.Itoa(base[s.SessionID]) {
			t.Fatalf("session %s after the restart: AIDE_PORT_BASE=%s, want %d", s.SessionID, got, base[s.SessionID])
		}
	}

	// Archiving frees the block: the next session gets it.
	if r := w.cli(time.Minute, "session", "archive", ss[0].SessionID); r.code != 0 {
		t.Fatalf("archive: %+v", r)
	}
	s3, r := w.startSession(time.Minute, "--wait", "--prompt", "env AIDE_PORT_BASE")
	if got := value(s3, r, "AIDE_PORT_BASE"); got != strconv.Itoa(a) {
		t.Fatalf("new session's AIDE_PORT_BASE=%s, want the archived session's %d", got, a)
	}
}
