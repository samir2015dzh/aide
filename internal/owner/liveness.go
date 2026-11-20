package owner

import (
	"encoding/json"
	"slices"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
)

// Whether each machine is there, as the screens show it next to its
// sessions' states (docs/session-states.md §3–4): when it was last heard
// from, since when it cannot be read, and the times it was lost.

// livenessEvery is how often the machines' liveness is brought up to date:
// a heartbeat's time is this precise.
const livenessEvery = 2 * time.Second

// Lost spans kept per machine: those of the last day, at most this many.
const (
	lostSpansKept = 20
	lostSpansFor  = 24 * time.Hour
)

// kLostSpans keeps the machines' lost spans across restarts:
// {machine id: [{from, to}]}, in JSON, times in RFC 3339.
const kLostSpans = "lost_spans"

type lostSpan struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to,omitzero"`
}

// relayUp says whether this device holds its connection to the relay now.
func (o *Owner) relayUp() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.client == nil {
		return false
	}
	select {
	case <-o.client.Done():
		return false
	default:
		return true
	}
}

// stateLimits are the limits this device works sessions' states out with.
func (o *Owner) stateLimits() *localv1.StateLimits {
	l := o.limits
	return &localv1.StateLimits{LostAfterSeconds: uint32(l.LostAfter / time.Second), StallAfterSeconds: uint32(l.StallAfter / time.Second)}
}

// livenessLocked brings the machines' liveness up to date at now: a
// machine is heard from while the relay holds its connection (it lists it
// online, and this device is connected to the relay to know) and following
// it has not failed. A machine not heard from for the lost limit opens a
// lost span, closed when it is heard from again. Tells the screens when a
// span opened or closed.
func (r *replica) livenessLocked(s *sources, relayUp bool, now time.Time) {
	if r.src != s {
		return
	}
	spans := r.lostSpansLocked()
	changed, kick := false, false
	for id, f := range s.feeds {
		heard := f.local || (relayUp && f.online && f.err == "")
		reachable := f.local || (relayUp && f.online && f.ready)
		if !heard && !f.relaySeen.IsZero() {
			// The relay knows better: it held the connection until it found
			// the machine gone, and says when it last heard from it.
			f.heardAt = f.relaySeen
		}
		last, wasHeard := f.heardAt, f.heard
		lost := !last.IsZero() && now.Sub(last) >= r.o.limits.LostAfter
		if heard {
			f.heardAt = now
		}
		if f.heard != heard {
			kick = true // its heartbeat's time is shown, or no longer
		}
		f.heard = heard
		switch {
		case reachable:
			f.downSince = time.Time{}
		case f.downSince.IsZero():
			f.downSince = now
		}
		list := spans[id]
		open := len(list) > 0 && list[len(list)-1].To.IsZero()
		switch {
		case heard && open:
			list[len(list)-1].To = now
			spans[id], changed = list, true
		case heard && !wasHeard && lost:
			// Back just as it was found lost, between two looks: lost all the same.
			spans[id], changed = append(list, lostSpan{From: last, To: now}), true
		case !heard && !open && lost:
			spans[id], changed = append(list, lostSpan{From: last}), true
		}
	}
	for id, list := range spans { // a day's worth, of machines still listed
		if s.feeds[id] == nil && s.isSettled {
			delete(spans, id)
			changed = true
			continue
		}
		kept := slices.DeleteFunc(slices.Clone(list), func(x lostSpan) bool { return !x.To.IsZero() && now.Sub(x.To) > lostSpansFor })
		if len(kept) > lostSpansKept {
			kept = kept[len(kept)-lostSpansKept:]
		}
		if len(kept) != len(list) {
			spans[id], changed = kept, true
		}
	}
	if changed {
		b, _ := json.Marshal(spans)
		r.o.st.set(kLostSpans, b)
	}
	if changed || kick {
		r.kickLocked()
	}
}

// lostSpansLocked is the machines' lost spans, read from disk the first time.
func (r *replica) lostSpansLocked() map[string][]lostSpan {
	if r.spans == nil {
		r.spans = map[string][]lostSpan{}
		if b, _ := r.o.st.get(kLostSpans); len(b) > 0 {
			json.Unmarshal(b, &r.spans)
		}
	}
	return r.spans
}

// liveness sets, of each machine but this device's own, when it was last
// heard from (once it no longer is: a machine heard from now has none),
// since when it cannot be read, and its lost spans. A machine not followed
// since this device started has the relay's last seen.
func (r *replica) liveness(machines []*localv1.Machine) {
	r.mu.Lock()
	defer r.mu.Unlock()
	spans := r.lostSpansLocked()
	var feeds map[string]*feed
	if r.src != nil {
		feeds = r.src.feeds
	}
	for _, m := range machines {
		if m.Local {
			continue
		}
		f := feeds[m.MachineId]
		// Its heartbeat's time once it is not heard from: the relay's last
		// seen while it is offline; else as the last look found it, or, if
		// it went since (unreachable now, the look still to come), when it
		// was last heard.
		var heard time.Time
		if f != nil && (!f.heard || m.Unreachable != "") {
			heard = f.heardAt
		}
		if seen := m.GetLastSeen(); !m.Online && seen != nil {
			heard = seen.AsTime()
		}
		if !heard.IsZero() {
			m.HeardAt = timestamppb.New(heard)
		}
		if m.Unreachable != "" {
			since := time.Now() // just now, the look still to come
			if f != nil && !f.downSince.IsZero() {
				since = f.downSince
			}
			m.UnreachableSince = timestamppb.New(since)
		}
		for _, x := range spans[m.MachineId] {
			ls := &localv1.LostSpan{From: timestamppb.New(x.From)}
			if !x.To.IsZero() {
				ls.To = timestamppb.New(x.To)
			}
			m.LostSpans = append(m.LostSpans, ls)
		}
	}
}

// keepLiveness brings the machines' liveness up to date every
// livenessEvery while s runs and a window looks, else every
// livenessBackground, and at once when a window comes (docs/resident.md §4).
func (r *replica) keepLiveness(s *sources) {
	t := time.NewTimer(livenessEvery)
	defer t.Stop()
	for {
		up := r.o.relayUp()
		r.mu.Lock()
		r.livenessLocked(s, up, time.Now())
		r.mu.Unlock()
		looking, arrived := windowLooking()
		if looking {
			t.Reset(livenessEvery)
		} else {
			t.Reset(livenessBackground)
		}
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
		case <-arrived:
		}
	}
}

// livenessBackground: how often liveness is brought up to date while no
// window looks (the notification area's state, notices).
const livenessBackground = 10 * time.Second
