// Package sessionstate works out what every screen and the CLI say of a
// session (docs/session-states.md): what it is doing (SessionState) and how
// fresh that is (SessionLink), two things never folded into one. The same
// table is in packages/core (states.ts) and the Android app (States.kt);
// testdata/cases.json holds the cases all three are checked against.
package sessionstate

import (
	"os"
	"strconv"
	"strings"
	"time"

	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
)

// Limits are the two limits states are worked out with.
type Limits struct {
	// LostAfter: a machine not heard from for this long is lost.
	LostAfter time.Duration
	// StallAfter: a running session with no event for this long is stalled.
	StallAfter time.Duration
}

// The default limits. A machine that dies silently is found gone by the
// relay after 20 s of silence (peerQuiet), which then says when it last
// heard from it; the owner's devices hear of that at once. 30 s from its
// last heartbeat leaves the relay its 20 s and a reconnect's few, so a
// blip never reads "lost". Five minutes of silence is past a model's
// longest usual think, and well before the machine's own watchdog stops a
// quiet turn (15 minutes).
const (
	DefaultLostAfter  = 30 * time.Second
	DefaultStallAfter = 5 * time.Minute
)

// Default is the default limits.
func Default() Limits { return Limits{LostAfter: DefaultLostAfter, StallAfter: DefaultStallAfter} }

// FromEnv is the default limits, less those AIDE_LOST_AFTER and
// AIDE_STALL_AFTER set (a Go duration, "90s", or seconds, "90").
func FromEnv() Limits {
	l := Default()
	if d, ok := envDuration("AIDE_LOST_AFTER"); ok {
		l.LostAfter = d
	}
	if d, ok := envDuration("AIDE_STALL_AFTER"); ok {
		l.StallAfter = d
	}
	return l
}

func envDuration(name string) (time.Duration, bool) {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return 0, false
	}
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		return time.Duration(n) * time.Second, true
	}
	if d, err := time.ParseDuration(v); err == nil && d > 0 {
		return d, true
	}
	return 0, false
}

// Machine is what the viewer knows of a session's machine.
type Machine struct {
	// Known: the machine is listed. A session of one that is not (removed,
	// or not listed yet) is lost.
	Known bool
	// Local: the viewer's own machine, read in process: always live.
	Local bool
	// Unreachable: why it cannot be read now; "" while it can.
	Unreachable string
	// Syncing: back, and being read again.
	Syncing bool
	// HeardAt: when the viewer last knew it was there, set once it no
	// longer is (zero while it is heard from, and when not known);
	// UnreachableSince: since when it cannot be read.
	HeardAt, UnreachableSince time.Time
}

// View is what is shown of a session.
type View struct {
	State sessionv1.SessionState
	Link  sessionv1.SessionLink
	// LastEventAt: when it last recorded anything (zero: not known).
	LastEventAt time.Time
	// Quiet: how long since then, while it runs (0 otherwise).
	Quiet time.Duration
	// HeardAt: when its machine was last heard from (zero for the viewer's
	// own machine, which always is, and when not known).
	HeardAt time.Time
	// Silent: how long its machine has not been heard from, while it
	// cannot be read (0 otherwise).
	Silent time.Duration
}

// StateOf is what a session is doing, from its status and progress alone.
func StateOf(s *sessionv1.Session) sessionv1.SessionState {
	switch s.GetStatus() {
	case sessionv1.SessionStatus_SESSION_STATUS_QUEUED, sessionv1.SessionStatus_SESSION_STATUS_PREPARING:
		return sessionv1.SessionState_SESSION_STATE_QUEUED
	case sessionv1.SessionStatus_SESSION_STATUS_RUNNING:
		return sessionv1.SessionState_SESSION_STATE_RUNNING
	case sessionv1.SessionStatus_SESSION_STATUS_WAITING_INPUT:
		return sessionv1.SessionState_SESSION_STATE_WAITING_INPUT
	case sessionv1.SessionStatus_SESSION_STATUS_ARCHIVED:
		return sessionv1.SessionState_SESSION_STATE_ARCHIVED
	case sessionv1.SessionStatus_SESSION_STATUS_CRASHED:
		return sessionv1.SessionState_SESSION_STATE_FAILED
	case sessionv1.SessionStatus_SESSION_STATUS_IDLE:
		f := s.GetProgress().GetLastFailure()
		switch {
		case f == nil && Unseen(s):
			return sessionv1.SessionState_SESSION_STATE_DONE_UNSEEN
		case f == nil:
			return sessionv1.SessionState_SESSION_STATE_DONE
		case f.GetKind() == sessionv1.FailureKind_FAILURE_KIND_INTERRUPTED:
			return sessionv1.SessionState_SESSION_STATE_INTERRUPTED
		default:
			return sessionv1.SessionState_SESSION_STATE_FAILED
		}
	}
	return sessionv1.SessionState_SESSION_STATE_UNSPECIFIED
}

// Unseen says whether a session has finished turns the owner has not
// opened (Session.seen; from an older machine, which keeps none, every
// turn counts as seen).
func Unseen(s *sessionv1.Session) bool {
	return s.GetSeen() != nil && s.GetProgress().GetTurnCount() > s.GetSeen().GetTurns()
}

// LastEventAt is when a session last recorded anything: its last event's
// time, else (an older machine) when it last changed.
func LastEventAt(s *sessionv1.Session) time.Time {
	if t := s.GetProgress().GetLastEventAt(); t != nil && t.GetSeconds() > 0 {
		return t.AsTime()
	}
	if t := s.GetUpdatedAt(); t != nil && t.GetSeconds() > 0 {
		return t.AsTime()
	}
	return time.Time{}
}

// Of is what is shown of a session at now.
func Of(s *sessionv1.Session, m Machine, l Limits, now time.Time) View {
	v := View{State: StateOf(s), LastEventAt: LastEventAt(s)}
	if !m.Local {
		v.HeardAt = m.HeardAt
	}
	switch {
	case !m.Known:
		v.Link = sessionv1.SessionLink_SESSION_LINK_LOST
		if !m.HeardAt.IsZero() {
			v.Silent = nonNegative(now.Sub(m.HeardAt))
		}
	case m.Local:
		v.Link = sessionv1.SessionLink_SESSION_LINK_LIVE
	default:
		// Not heard from (HeardAt set), or not readable: checked until the
		// lost limit passes since it was last heard from, then lost.
		since := m.HeardAt
		switch {
		case m.Unreachable != "":
			v.Link = sessionv1.SessionLink_SESSION_LINK_CHECKING
			if since.IsZero() {
				since = m.UnreachableSince
			}
			if since.IsZero() {
				v.Link = sessionv1.SessionLink_SESSION_LINK_LOST
			}
		case m.Syncing:
			v.Link = sessionv1.SessionLink_SESSION_LINK_SYNCING
		case !since.IsZero():
			v.Link = sessionv1.SessionLink_SESSION_LINK_CHECKING // readable a moment ago, but no heartbeat
		default:
			v.Link = sessionv1.SessionLink_SESSION_LINK_LIVE
		}
		if !since.IsZero() {
			if v.Silent = nonNegative(now.Sub(since)); v.Silent >= l.LostAfter {
				v.Link = sessionv1.SessionLink_SESSION_LINK_LOST
			}
		}
	}
	if v.State == sessionv1.SessionState_SESSION_STATE_RUNNING && !v.LastEventAt.IsZero() {
		v.Quiet = nonNegative(now.Sub(v.LastEventAt))
		if v.Link == sessionv1.SessionLink_SESSION_LINK_LIVE && v.Quiet >= l.StallAfter {
			v.Link = sessionv1.SessionLink_SESSION_LINK_STALLED
		}
	}
	return v
}

func nonNegative(d time.Duration) time.Duration { return max(d, 0) }

// Live says whether what is shown is current: the state may be acted on
// (answered, interrupted, archived).
func (v View) Live() bool {
	return v.Link == sessionv1.SessionLink_SESSION_LINK_LIVE || v.Link == sessionv1.SessionLink_SESSION_LINK_STALLED
}

// StateName is a state's name in the CLI's output: "running", "done_unseen".
func StateName(st sessionv1.SessionState) string {
	if st == sessionv1.SessionState_SESSION_STATE_UNSPECIFIED {
		return "unknown"
	}
	return strings.ToLower(strings.TrimPrefix(st.String(), "SESSION_STATE_"))
}

// LinkName is a link's name in the CLI's output: "live", "lost".
func LinkName(k sessionv1.SessionLink) string {
	if k == sessionv1.SessionLink_SESSION_LINK_UNSPECIFIED {
		return "unknown"
	}
	return strings.ToLower(strings.TrimPrefix(k.String(), "SESSION_LINK_"))
}

// Text is the one line every end shows (English; the screens translate
// it, docs/session-states.md §5): "running", "running · quiet 12m",
// "lost · last known running · 7m ago".
func (v View) Text() string {
	st := strings.ReplaceAll(StateName(v.State), "_", " ")
	switch v.Link {
	case sessionv1.SessionLink_SESSION_LINK_STALLED:
		return st + " · quiet " + Ago(v.Quiet)
	case sessionv1.SessionLink_SESSION_LINK_CHECKING:
		return st + "? · machine not answering"
	case sessionv1.SessionLink_SESSION_LINK_LOST:
		if v.Silent > 0 {
			return "lost · last known " + st + " · " + Ago(v.Silent) + " ago"
		}
		return "lost · last known " + st
	case sessionv1.SessionLink_SESSION_LINK_SYNCING:
		return st + " · catching up"
	}
	return st
}

// Ago is a duration as lists say it: "40s", "12m", "3h", "2d".
func Ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d/time.Second)) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	}
	return strconv.Itoa(int(d/(24*time.Hour))) + "d"
}
