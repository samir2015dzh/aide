package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/machine"
)

// The sessions this client last read of each machine, kept in
// $AIDE_HOME/sessions-seen.json: while a machine cannot be read, session
// list and get say what was last known of its sessions, marked lost
// (docs/session-states.md §6), rather than nothing.

// seenPerMachine is how many sessions are kept per machine, the newest.
const seenPerMachine = 100

type seenFile struct {
	Machines map[string]*seenOf `json:"machines"`
}

type seenOf struct {
	// ReadAt: when the machine last answered this client.
	ReadAt   time.Time         `json:"read_at"`
	Sessions []json.RawMessage `json:"sessions"` // protojson
}

func (a *app) sessionsSeenPath() string { return filepath.Join(a.home, "sessions-seen.json") }

func (a *app) readSeen() *seenFile {
	f := &seenFile{}
	if a.home == "" {
		return f
	}
	if b, err := os.ReadFile(a.sessionsSeenPath()); err == nil {
		json.Unmarshal(b, f)
	}
	if f.Machines == nil {
		f.Machines = map[string]*seenOf{}
	}
	return f
}

// keepSeen keeps sessions just read from machineID at readAt.
func (a *app) keepSeen(machineID string, readAt time.Time, sessions ...*sessionv1.Session) {
	if a.home == "" || machineID == "" {
		return
	}
	f := a.readSeen()
	m := f.Machines[machineID]
	if m == nil {
		m = &seenOf{}
		f.Machines[machineID] = m
	}
	if readAt.After(m.ReadAt) {
		m.ReadAt = readAt
	}
	byID := map[string]*sessionv1.Session{}
	for _, s := range sessionsOf(m) {
		byID[s.SessionId] = s
	}
	for _, s := range sessions {
		if s.GetSessionId() != "" {
			byID[s.SessionId] = s
		}
	}
	all := slices.Collect(func(yield func(*sessionv1.Session) bool) {
		for _, s := range byID {
			if !yield(s) {
				return
			}
		}
	})
	slices.SortFunc(all, machine.NewestFirst)
	if len(all) > seenPerMachine {
		all = all[:seenPerMachine]
	}
	m.Sessions = m.Sessions[:0]
	for _, s := range all {
		if b, err := protojson.Marshal(s); err == nil {
			m.Sessions = append(m.Sessions, b)
		}
	}
	b, err := json.Marshal(f)
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(a.home, ".sessions-seen-*")
	if err != nil {
		return
	}
	_, werr := tmp.Write(b)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Rename(tmp.Name(), a.sessionsSeenPath()) != nil {
		os.Remove(tmp.Name())
	}
}

func sessionsOf(m *seenOf) []*sessionv1.Session {
	var out []*sessionv1.Session
	for _, raw := range m.Sessions {
		s := &sessionv1.Session{}
		if protojson.Unmarshal(raw, s) == nil {
			out = append(out, s)
		}
	}
	return out
}

// seenSessions is what this client last read of machineID, and when.
func (a *app) seenSessions(machineID string) ([]*sessionv1.Session, time.Time) {
	m := a.readSeen().Machines[machineID]
	if m == nil {
		return nil, time.Time{}
	}
	return sessionsOf(m), m.ReadAt
}

// seenSession is session id as this client last read it, and when.
func (a *app) seenSession(id string) (*sessionv1.Session, time.Time) {
	for _, m := range a.readSeen().Machines {
		for _, s := range sessionsOf(m) {
			if s.SessionId == id {
				return s, m.ReadAt
			}
		}
	}
	return nil, time.Time{}
}
