package resident

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSystem stands for Windows' calls: what aided's and other processes'
// priority is.
type fakeSystem struct {
	mu    sync.Mutex
	self  bool
	procs map[int]bool // throttled
	trees []int        // pids set back with their tree
	calls int
}

func (f *fakeSystem) saver(busy, her *int) *Saver {
	var mu sync.Mutex
	s := NewSaver(slog.New(slog.NewTextHandler(io.Discard, nil)),
		func() int { mu.Lock(); defer mu.Unlock(); return *busy },
		func() int { mu.Lock(); defer mu.Unlock(); return *her })
	s.supported = true
	f.procs = map[int]bool{}
	s.self = func(on bool) error { f.mu.Lock(); defer f.mu.Unlock(); f.self, f.calls = on, f.calls+1; return nil }
	s.other = func(pid int, on, tree bool) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.procs[pid] = on
		if tree {
			f.trees = append(f.trees, pid)
		}
		return nil
	}
	s.priority = func() string {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.self {
			return "below normal"
		}
		return "normal"
	}
	return s
}

// idleFor makes the saver believe it has been idle that long.
func idleFor(s *Saver, d time.Duration) {
	s.mu.Lock()
	s.idleSince = time.Now().Add(-d)
	s.mu.Unlock()
}

func TestPowerSaving(t *testing.T) {
	var f fakeSystem
	busy, her := 0, 4242
	s := f.saver(&busy, &her)
	t.Cleanup(func() { current.CompareAndSwap(s, nil) })

	// Idle, but not for long enough yet.
	s.check("")
	if st := s.State(); st.Saving || !strings.HasPrefix(st.Reason, "idle, saving in") {
		t.Fatalf("at once: %+v", st)
	}
	// Idle long enough: aided and her are throttled, below normal.
	idleFor(s, enterAfter)
	if next := s.check(""); next != savingCheck {
		t.Fatalf("while saving, it looks again in %v", next)
	}
	if st := s.State(); !st.Saving || st.Priority != "below normal" || st.Her != her || !f.self || !f.procs[her] {
		t.Fatalf("idle: %+v, %+v", st, f.procs)
	}
	// A window opens: it ends at once, her and her tree back to normal.
	done := Viewing()
	if st := s.State(); st.Saving || st.Reason != "a window opened" || st.Priority != "normal" || f.self || f.procs[her] || len(f.trees) != 1 {
		t.Fatalf("a window: %+v, %+v", st, f.procs)
	}
	// While it is open, nothing saves however long idle.
	idleFor(s, time.Hour)
	s.check("")
	if s.State().Saving {
		t.Fatal("saving with a window open")
	}
	done()
	done() // twice is once
	if s.viewers.Load() != 0 {
		t.Fatal(s.viewers.Load())
	}
	// It closed: saving waits enterAfter again.
	s.check("")
	if s.State().Saving {
		t.Fatal("saving right after the window closed")
	}
	idleFor(s, enterAfter)
	s.check("")
	if !s.State().Saving {
		t.Fatal("not saving once idle again")
	}
	// Work starts (a session about to start its agent): normal first.
	Wake("a session starts")
	if st := s.State(); st.Saving || st.Reason != "a session starts" || f.self {
		t.Fatalf("woken: %+v", st)
	}
	// Sessions or Her at work: never saving.
	busy = 1
	idleFor(s, time.Hour)
	s.check("")
	if st := s.State(); st.Saving || st.Reason != "sessions or Her at work" {
		t.Fatalf("busy: %+v", st)
	}
	busy = 0
	s.check("")
	idleFor(s, enterAfter)
	s.check("")
	// Her restarts while saving (she starts at normal): the new one is throttled.
	her = 5151
	s.check("")
	if st := s.State(); !st.Saving || st.Her != 5151 || !f.procs[5151] {
		t.Fatalf("her again: %+v %+v", st, f.procs)
	}
	calls := f.calls
	s.check("")
	if f.calls != calls {
		t.Fatal("aided throttled again while saving")
	}
}

// Run sets normal priority first (a supervisor's below normal is
// inherited), and normal again when aided stops.
func TestPowerSavingRun(t *testing.T) {
	var f fakeSystem
	busy, her := 0, 0
	s := f.saver(&busy, &her)
	f.self = true // inherited
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { s.Run(ctx); close(stopped) }()
	for deadline := time.Now().Add(5 * time.Second); s.State().Priority != "normal"; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal(s.State())
		}
	}
	idleFor(s, enterAfter)
	s.poke()
	for deadline := time.Now().Add(5 * time.Second); !s.State().Saving; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal(s.State())
		}
	}
	cancel()
	<-stopped
	if st := s.State(); st.Saving || f.self {
		t.Fatalf("after stopping: %+v", st)
	}
	if _, ok := Current(); ok {
		t.Fatal("still current after Run")
	}
}
