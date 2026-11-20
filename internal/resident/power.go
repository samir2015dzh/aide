package resident

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// Power saving (Windows; docs/resident.md §2). While no session runs, Her
// does no work and no window of the app is open, aided and her ask Windows
// to run them slowly (execution speed throttling, EcoQoS: efficiency mode
// on Windows 11) at below normal priority. Anything to do ends it at once:
// a window opening, a session starting (Wake, before its agent starts),
// her starting work. Agents never run throttled: they are started at normal
// priority whatever aided's (internal/provider), and the processes her
// started while saving are set back to normal when it ends.

// enterAfter: how long everything must have been idle before saving
// starts, so that a window closed and opened again, or a session between
// two turns, does not flip it back and forth.
const enterAfter = 30 * time.Second

// Checks: how often idleness is looked at while not saving, and how often
// while saving (her may start work by herself, a routine: noticed within
// this, and her's processes set back to normal).
var (
	checkEvery  = 15 * time.Second
	savingCheck = 2 * time.Second
)

// State is what power saving is doing, for `aided status` and the control
// socket's /power (docs/resident.md §2.3).
type State struct {
	Supported bool      `json:"supported"`         // this system can save power this way (Windows)
	Saving    bool      `json:"saving"`            // aided and her run throttled, below normal
	Since     time.Time `json:"since,omitzero"`    // when Saving last changed
	Reason    string    `json:"reason"`            // why it is as it is
	Priority  string    `json:"priority"`          // aided's priority class now, as Task Manager shows it
	Her       int       `json:"her_pid,omitempty"` // her's process, throttled with aided
	Error     string    `json:"error,omitempty"`   // the last call that failed
}

// Saver decides and applies power saving for this process and her's.
type Saver struct {
	log     *slog.Logger
	busy    func() int // sessions running or queued, and her's work
	her     func() int // her's pid, 0 while she does not run
	viewers atomic.Int32
	kick    chan struct{}
	// The system's calls (tests replace them).
	supported bool
	self      func(on bool) error
	other     func(pid int, on, tree bool) error
	priority  func() string
	trim      func(pid int) (before, after uint64, err error)
	tree      func(pid int) []int
	// app is the desktop app's main process, while it watches (SetApp).
	app atomic.Int64

	mu        sync.Mutex
	state     State
	idleSince time.Time // zero while something goes on
	herPid    int       // her's process as throttled
}

var current atomic.Pointer[Saver]

// NewSaver makes the process's saver, which Wake and Viewing reach; Run
// runs it.
func NewSaver(log *slog.Logger, busy, herPid func() int) *Saver {
	s := &Saver{log: log, busy: busy, her: herPid, kick: make(chan struct{}, 1), supported: supported, self: throttleSelf, other: throttle, priority: priority,
		trim: trimWorkingSet, tree: descendants}
	s.state = State{Supported: supported, Reason: "starting", Priority: priority()}
	current.Store(s)
	return s
}

// Current is the process's power saving, if it has a saver.
func Current() (State, bool) {
	if s := current.Load(); s != nil {
		return s.State(), true
	}
	return State{Supported: supported, Reason: "not running"}, false
}

// Wake ends power saving now, before work starts (a session's agent about
// to start); it returns once aided runs at normal priority again.
func Wake(why string) {
	if s := current.Load(); s != nil {
		s.check(why)
	}
}

// SetApp tells the saver of the desktop app's main process (its own watch,
// for notifications, says which), until done: when saving starts, the app's
// processes give back their idle memory with aided's and her's (trim).
func SetApp(pid int) (done func()) {
	s := current.Load()
	if s == nil || pid <= 0 {
		return func() {}
	}
	s.app.Store(int64(pid))
	return func() { s.app.CompareAndSwap(int64(pid), 0) }
}

// Viewing counts a window of the app watching (a stream it holds open) until
// done is called: saving ends while one is open.
func Viewing() (done func()) {
	s := current.Load()
	if s == nil {
		return func() {}
	}
	if s.viewers.Add(1) == 1 {
		s.check("a window opened")
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			if s.viewers.Add(-1) == 0 {
				s.poke()
			}
		})
	}
}

func (s *Saver) poke() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// State is a copy of what it does now.
func (s *Saver) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Run checks idleness until ctx ends, then leaves power saving.
func (s *Saver) Run(ctx context.Context) {
	defer current.CompareAndSwap(s, nil)
	if !s.supported {
		s.mu.Lock()
		s.state.Reason = "not supported on this system"
		s.mu.Unlock()
		return
	}
	// Normal to begin with: the service's supervisor runs below normal, and
	// a child takes that (internal/service starts the daemon at normal, an
	// older supervisor did not).
	if err := s.self(false); err != nil {
		s.log.Warn("power saving: setting normal priority", "err", err)
	}
	s.mu.Lock()
	s.state.Priority = s.priority()
	s.mu.Unlock()
	s.check("")
	t := time.NewTimer(checkEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.set(false, "aided stopping")
			return
		case <-s.kick:
		case <-t.C:
		}
		next := s.check("")
		t.Reset(next)
	}
}

// check looks at what goes on and saves or not accordingly; it returns when
// to look again. wake, if set, is work about to start.
func (s *Saver) check(wake string) time.Duration {
	if !s.supported {
		return checkEvery
	}
	busy, viewers := 0, s.viewers.Load()
	if wake == "" && viewers == 0 && s.busy != nil {
		busy = s.busy()
	}
	s.mu.Lock()
	saving, idleSince, herPid := s.state.Saving, s.idleSince, s.herPid
	s.mu.Unlock()
	now := time.Now()
	reason := ""
	switch {
	case wake != "":
		reason = wake
	case viewers > 0:
		reason = "a window is open"
	case busy > 0:
		reason = "sessions or Her at work"
	}
	if reason != "" {
		s.mu.Lock()
		s.idleSince = time.Time{}
		s.mu.Unlock()
		if saving {
			s.set(false, reason)
		} else {
			s.note(reason)
		}
		return checkEvery
	}
	if idleSince.IsZero() {
		s.mu.Lock()
		s.idleSince = now
		s.mu.Unlock()
		idleSince = now
	}
	if !saving {
		if wait := enterAfter - now.Sub(idleSince); wait > 0 {
			s.note("idle, saving in " + wait.Round(time.Second).String())
			return min(wait, checkEvery)
		}
		s.set(true, "no session running, Her idle, no window open")
		return savingCheck
	}
	// Saving: her may have started again since (another process).
	if pid := s.herPid0(); pid != herPid {
		s.set(true, "her started again")
	}
	return savingCheck
}

func (s *Saver) herPid0() int {
	if s.her == nil {
		return 0
	}
	return s.her()
}

// note keeps why it does not save, without logging it.
func (s *Saver) note(reason string) {
	s.mu.Lock()
	s.state.Reason = reason
	s.mu.Unlock()
}

// set enters or leaves power saving, for aided and her, and logs it.
func (s *Saver) set(on bool, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	her := s.herPid0()
	was := s.state.Saving
	var errs []string
	if was != on {
		if err := s.self(on); err != nil {
			errs = append(errs, "aided: "+err.Error())
		}
	}
	if on {
		if her != 0 && her != s.herPid {
			if err := s.other(her, true, false); err != nil {
				errs = append(errs, "her: "+err.Error())
			}
		}
	} else if s.herPid != 0 {
		// Her, and what she started while saving (it took her priority).
		if err := s.other(s.herPid, false, true); err != nil {
			errs = append(errs, "her: "+err.Error())
		}
	}
	if on {
		s.herPid = her
	} else {
		s.herPid = 0
	}
	if was != on {
		s.state.Since = time.Now()
		if on {
			go s.trimAll(her)
		}
	}
	s.state.Saving, s.state.Reason, s.state.Priority, s.state.Her = on, reason, s.priority(), s.herPid
	s.state.Error = ""
	if len(errs) > 0 {
		s.state.Error = errs[0]
	}
	if was == on && len(errs) == 0 && !(on && reason == "her started again") {
		return
	}
	level := slog.LevelInfo
	if len(errs) > 0 {
		level = slog.LevelWarn
	}
	what := "power saving ends"
	if on {
		what = "power saving starts"
	}
	s.log.Log(context.Background(), level, what, "reason", reason, "priority", s.state.Priority, "her_pid", s.herPid, "api", apiResult(errs))
}

// trimAll has Windows take the idle pages of aided, her and the desktop
// app's processes (main, GPU, utilities; its windows are all closed) as
// saving starts: nothing of theirs runs then but timers, which touch little,
// and the memory is free for whatever else runs (docs/resident.md §1.2).
// The pages a process touches again come back from the standby list.
func (s *Saver) trimAll(her int) {
	pids := []int{os.Getpid()}
	if her != 0 {
		pids = append(pids, her)
	}
	if app := int(s.app.Load()); app != 0 {
		pids = append(pids, app)
		pids = append(pids, s.tree(app)...)
	}
	var before, after uint64
	trimmed := 0
	for _, pid := range pids {
		b, a, err := s.trim(pid)
		if err != nil {
			continue // gone, or not ours to trim
		}
		before, after, trimmed = before+b, after+a, trimmed+1
	}
	s.log.Info("power saving: idle memory given back", "processes", trimmed, "working_set_mib_before", before>>20, "after", after>>20)
}

func apiResult(errs []string) string {
	if len(errs) == 0 {
		return "ok"
	}
	return errs[0]
}
