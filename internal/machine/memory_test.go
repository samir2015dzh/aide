package machine

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
)

// While the system is short of memory a new session stays queued, says why,
// and starts by itself once memory is back (docs/resident.md §3).
func TestQueuedWhileMemoryIsLow(t *testing.T) {
	was := memoryRecheck
	memoryRecheck = 20 * time.Millisecond
	t.Cleanup(func() { memoryRecheck = was })
	h := newHarness(t, DefaultLimits())
	var low atomic.Bool
	low.Store(true)
	h.s.memoryLow = func() (bool, string) {
		if low.Load() {
			return true, "300 MB of 16384 MB free (needs 1638 MB)"
		}
		return false, ""
	}
	id := h.start("say hello")
	time.Sleep(200 * time.Millisecond) // several looks at memory
	s, err := h.s.Get(h.p, id)
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != sessionv1.SessionStatus_SESSION_STATUS_QUEUED || s.Progress.GetPhase() != PhaseQueuedLowMemory {
		t.Fatalf("while memory is low: %v, phase %q", s.Status, s.Progress.GetPhase())
	}
	said := 0
	for _, e := range h.events(id) {
		if st := e.GetStatus(); st != nil && strings.HasPrefix(st.Reason, "waiting for memory: 300 MB") {
			said++
		}
	}
	if said != 1 {
		t.Fatalf("said why %d times:\n%s", said, h.dump(id))
	}
	low.Store(false)
	h.wait(id, idle)
	if tc := h.lastTurn(id); tc.Result != "hello" {
		t.Fatalf("turn: %v", tc)
	}
	if s, _ := h.s.Get(h.p, id); s.Progress.GetPhase() == PhaseQueuedLowMemory {
		t.Fatal("still says it waits for memory")
	}
}
