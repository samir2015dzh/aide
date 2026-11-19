package owner

import (
	"testing"
	"time"

	localv1 "github.com/hermeshai/hermesh/gen/go/hermesh/local/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// A machine heard from, gone silent past the lost limit, and back: its
// heartbeat stops when it goes, a lost span opens from then once the limit
// passes and closes when it is back, and the span outlives a restart.
func TestLiveness(t *testing.T) {
	dir := t.TempDir()
	dev, err := Open(Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	o := dev.owner()
	r := o.rep
	f := &feed{name: "vps-1", online: true, ready: true}
	s := &sources{feeds: map[string]*feed{"m_1": f, "m_local": {local: true, online: true, ready: true}}, isSettled: true}
	r.src = s
	t0 := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	look := func(at time.Duration, relayUp bool) *localv1.Machine {
		r.mu.Lock()
		r.livenessLocked(s, relayUp, t0.Add(at))
		r.mu.Unlock()
		m := &localv1.Machine{MachineId: "m_1", Online: f.online}
		if !f.online {
			m.Unreachable = "offline"
		}
		r.liveness([]*localv1.Machine{m})
		return m
	}

	if m := look(0, true); m.HeardAt != nil || m.UnreachableSince != nil || len(m.LostSpans) != 0 {
		t.Fatalf("heard from now: no heartbeat time, nothing lost: %v", m)
	}
	f.online, f.ready = false, false
	m := look(2*time.Second, true)
	if !m.HeardAt.AsTime().Equal(t0) || !m.UnreachableSince.AsTime().Equal(t0.Add(2*time.Second)) || len(m.LostSpans) != 0 {
		t.Fatalf("gone: heard at t0, unreachable since t0+2s, not lost yet: %v", m)
	}
	m = look(o.limits.LostAfter, true)
	if len(m.LostSpans) != 1 || !m.LostSpans[0].From.AsTime().Equal(t0) || m.LostSpans[0].To != nil {
		t.Fatalf("past the lost limit, a span open from the last heartbeat: %v", m)
	}
	f.online, f.ready = true, true
	m = look(10*time.Minute, true)
	if m.HeardAt != nil || m.UnreachableSince != nil || len(m.LostSpans) != 1 || !m.LostSpans[0].To.AsTime().Equal(t0.Add(10*time.Minute)) {
		t.Fatalf("back: the span closed: %v", m)
	}

	// This device loses the relay: the machine is no longer heard from,
	// though the relay last listed it online.
	m = look(11*time.Minute, false)
	if !m.HeardAt.AsTime().Equal(t0.Add(10 * time.Minute)) {
		t.Fatalf("no relay: heard at the last look with it: %v", m)
	}
	look(12*time.Minute, false)

	// Gone and back between two looks, silent past the lost limit by the
	// relay's last heard: lost all the same, a closed span.
	g := &feed{name: "vps-2", online: true, ready: true}
	s.feeds["m_2"] = g
	f.online = false // m_1 stays gone meanwhile
	t1 := t0.Add(13 * time.Minute)
	r.mu.Lock()
	r.livenessLocked(s, true, t1)
	g.online, g.ready, g.relaySeen = false, false, t1.Add(-5*time.Second)
	r.livenessLocked(s, true, t1.Add(20*time.Second))
	g.online, g.ready, g.relaySeen = true, true, time.Time{}
	r.livenessLocked(s, true, t1.Add(26*time.Second))
	got := r.spans["m_2"]
	r.mu.Unlock()
	if len(got) != 1 || !got[0].From.Equal(t1.Add(-5*time.Second)) || !got[0].To.Equal(t1.Add(26*time.Second)) {
		t.Fatalf("back between looks: %v", got)
	}
	dev.Close()

	dev2, err := Open(Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer dev2.Close()
	m = &localv1.Machine{MachineId: "m_1", LastSeen: timestamppb.New(t0)}
	dev2.owner().rep.liveness([]*localv1.Machine{m})
	if len(m.LostSpans) != 2 || m.LostSpans[1].To != nil || !m.HeardAt.AsTime().Equal(t0) {
		t.Fatalf("after a restart: both spans, the second open; heard as the relay last saw it: %v", m)
	}
}
