//go:build unix

package e2e

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/sessionstate"
)

// TestSessionsOfAnOfflineMachine: a session's machine drops in the middle
// of a turn: it loses its network (frozen: it answers nothing), and later
// its aided stops. While it is away nothing shows its session as at
// work (docs/session-states.md): within the lost limit of its last
// heartbeat, the owner's screens hold the session as it last was, its
// machine's heartbeat time with it, and work it out as lost (last known
// running); the agent's CLI lists it under lost_sessions, lost, with its
// heartbeat's age, and session get fails as unreachable with the session as
// last read, lost. Once the machine is back every one of them shows the
// session as it is now, by itself: its turn done, or, where aided
// stopped, crashed; and the screens keep the time it was lost. Before it
// drops, a turn quiet past the stall limit reads stalled.
func TestSessionsOfAnOfflineMachine(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	w, vpsID, restart := farOwnerWithVPS(t, 0)
	vps := w.logs[len(w.logs)-1] // the vps-1 aided farOwnerWithVPS started
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := w.app.client.AddProject(ctx, &localv1.AddProjectRequest{MachineId: vpsID, ProjectId: "demo", Path: gitRepo(t, map[string]string{"README.md": "# vps\n"})}); err != nil {
		t.Fatal(err)
	}
	scr := watchScreen(ctx, t, w)
	onScreen := func(id string) (*sessionv1.Session, *localv1.Machine) {
		scr.mu.Lock()
		defer scr.mu.Unlock()
		return scr.sessions[id], scr.machines[vpsID]
	}
	running := func(id string) {
		t.Helper()
		eventually(t, 30*time.Second, id+" at work, on the CLI and the screen", func() bool {
			s, m := onScreen(id)
			return w.get(id).Status == "running" && s.GetStatus() == sessionv1.SessionStatus_SESSION_STATUS_RUNNING && m.GetUnreachable() == ""
		})
	}
	lostAfter := sessionstate.Default().LostAfter
	// onScreenNow is what a screen works out of session id now, with the
	// limits the core gave it.
	onScreenNow := func(id string, limits sessionstate.Limits) sessionstate.View {
		s, m := onScreen(id)
		var heard, since time.Time
		if m.GetHeardAt() != nil {
			heard = m.HeardAt.AsTime()
		}
		if m.GetUnreachableSince() != nil {
			since = m.UnreachableSince.AsTime()
		}
		return sessionstate.Of(s, sessionstate.Machine{Known: m != nil, Unreachable: m.GetUnreachable(), Syncing: m.GetSyncing(), HeardAt: heard, UnreachableSince: since}, limits, time.Now())
	}
	// away checks what each surface shows of session id while vps-1 is away.
	away := func(id string) {
		t.Helper()
		gone := time.Now()
		eventually(t, 60*time.Second, "vps-1 offline to the owner and the screen", func() bool {
			_, m := onScreen(id)
			res, err := w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
			return err == nil && slices.ContainsFunc(res.Machines, func(m *localv1.Machine) bool { return m.MachineId == vpsID && !m.Online && m.LastSeen != nil }) &&
				m.GetUnreachable() == "offline"
		})
		// The screen keeps the session as it last was, with its time, its
		// machine unreachable: the apps show its state as unknown.
		s, _ := onScreen(id)
		if s == nil || s.UpdatedAt == nil {
			t.Fatalf("the screen dropped %s, or its time: %v", id, s)
		}
		t.Logf("screen: %s %v, updated %v; vps-1 unreachable: offline", id, s.Status, s.UpdatedAt.AsTime())
		// Lost within the lost limit of its last heartbeat, never running.
		_, m := onScreen(id)
		if m.GetHeardAt() == nil || m.GetUnreachableSince() == nil {
			t.Fatalf("the screen has no heartbeat time for vps-1: %v", m)
		}
		eventually(t, lostAfter+10*time.Second, id+" lost on the screen", func() bool {
			v := onScreenNow(id, sessionstate.Default())
			if v.Link == sessionv1.SessionLink_SESSION_LINK_LIVE || v.Link == sessionv1.SessionLink_SESSION_LINK_STALLED {
				t.Fatalf("the screen shows %s live while vps-1 is offline: %s", id, v.Text())
			}
			return v.Link == sessionv1.SessionLink_SESSION_LINK_LOST
		})
		v := onScreenNow(id, sessionstate.Default())
		t.Logf("screen: %q, %v after this test saw vps-1 offline; vps-1 last heard %v ago", v.Text(), time.Since(gone).Round(time.Second), v.Silent.Round(time.Second))
		// The owner's list: not among the sessions, its machine unreachable.
		list, err := w.app.client.ListSessions(ctx, &localv1.ListSessionsRequest{})
		if err != nil || slices.ContainsFunc(list.Sessions, func(s *sessionv1.Session) bool { return s.SessionId == id }) ||
			!slices.ContainsFunc(list.Unreachable, func(u *localv1.Unreachable) bool { return u.MachineId == vpsID && u.Error == "offline" }) {
			t.Fatalf("the owner's ListSessions: %v %v", list, err)
		}
		// The agent's list: the machine offline since when; not the session.
		r := w.cli(time.Minute, "session", "list", "-o", "json")
		var doc struct {
			Sessions     []sessionJSON
			LostSessions []sessionJSON `json:"lost_sessions"`
			Offline      []struct {
				Machine  string
				LastSeen *string `json:"last_seen"`
			}
		}
		json.Unmarshal([]byte(r.stdout), &doc)
		i := slices.IndexFunc(doc.LostSessions, func(s sessionJSON) bool { return s.SessionID == id })
		if r.code != 0 || slices.ContainsFunc(doc.Sessions, func(s sessionJSON) bool { return s.SessionID == id }) ||
			len(doc.Offline) != 1 || doc.Offline[0].Machine != "vps-1" || doc.Offline[0].LastSeen == nil || i < 0 {
			t.Fatalf("session list: %+v", r)
		}
		if l := doc.LostSessions[i]; l.State != "running" || l.Link != "lost" || !l.Stale || l.HeartbeatAge == nil || *l.HeartbeatAge < int64(lostAfter/time.Second) {
			t.Fatalf("session list: lost_sessions: %+v\n%s", l, r.stdout)
		}
		t.Logf("CLI list: lost_sessions[%s] = %q, heartbeat %ds ago", id, doc.LostSessions[i].StateText, *doc.LostSessions[i].HeartbeatAge)
		r = w.cli(time.Minute, "session", "list")
		if !strings.Contains(r.stdout, "offline: vps-1 (last seen ") || !strings.Contains(r.stdout, "lost · last known running · ") {
			t.Fatalf("session list (text): %+v", r)
		}
		t.Logf("session list:\n%s", r.stdout)
		for _, line := range strings.Split(r.stdout, "\n") {
			if strings.Contains(line, id) && !strings.Contains(line, "lost · ") {
				t.Fatalf("session list (text) shows %s as other than lost: %q", id, line)
			}
		}
		// session get: unreachable, with the session last known, lost.
		r = w.cli(time.Minute, "session", "get", id, "-o", "json")
		if got := firstSession(r.stdout); r.code != 4 || !strings.Contains(r.stdout, `"kind": "unreachable"`) || !strings.Contains(r.stdout, "machine vps-1 is offline (last seen ") ||
			got.State != "running" || got.Link != "lost" || !got.Stale {
			t.Fatalf("session get: %+v", r)
		}
		t.Logf("CLI get: exit 4, unreachable, session %q\n%s", firstSession(r.stdout).StateText, r.stdout)
	}
	// back checks that the screen keeps the time vps-1 was lost, ended.
	back := func() {
		t.Helper()
		eventually(t, 30*time.Second, "vps-1's lost span closed on the screen", func() bool {
			_, m := onScreen("")
			n := len(m.GetLostSpans())
			return n > 0 && m.LostSpans[n-1].To != nil && m.HeardAt == nil
		})
		_, m := onScreen("")
		last := m.LostSpans[len(m.LostSpans)-1]
		t.Logf("screen: vps-1 lost %s – %s", last.From.AsTime().Format(time.TimeOnly), last.To.AsTime().Format(time.TimeOnly))
	}

	// The machine loses its network in the middle of a turn.
	s, r := w.startSession(time.Minute, "--project", "vps-1/demo", "--prompt", "sleep 40s\nsay finished")
	if s.SessionID == "" {
		t.Fatalf("start: %+v", r)
	}
	running(s.SessionID)
	scr.mu.Lock()
	if l := scr.status.GetStateLimits(); l.GetLostAfterSeconds() != uint32(lostAfter/time.Second) || l.GetStallAfterSeconds() != uint32(sessionstate.DefaultStallAfter/time.Second) {
		t.Fatalf("the core's state limits: %v", l)
	}
	scr.mu.Unlock()
	// Quiet past a (short) stall limit: stalled, to the CLI and the screen.
	time.Sleep(6 * time.Second)
	stalled := firstSession(run(t, append(slices.Clone(w.cliEnv), "AIDE_STALL_AFTER=5"), time.Minute, filepath.Join(w.bin, "aide"), "session", "get", s.SessionID, "-o", "json").stdout)
	if stalled.State != "running" || stalled.Link != "stalled" || stalled.StalledFor < 5 || stalled.LastEventAt == nil || stalled.Stale {
		t.Fatalf("a quiet turn, stall limit 5s: %+v", stalled)
	}
	if v := onScreenNow(s.SessionID, sessionstate.Limits{LostAfter: lostAfter, StallAfter: 5 * time.Second}); v.Link != sessionv1.SessionLink_SESSION_LINK_STALLED {
		t.Fatalf("a quiet turn on the screen, stall limit 5s: %s", v.Text())
	}
	if w.get(s.SessionID).Link != "live" {
		t.Fatalf("under the default stall limit, live")
	}
	t.Logf("quiet turn, stall limit 5s: %q", stalled.StateText)
	if r := run(t, append(slices.Clone(w.cliEnv), "AIDE_STALL_AFTER=5"), time.Minute, filepath.Join(w.bin, "aide"), "session", "list"); r.code == 0 {
		t.Logf("session list, stall limit 5s:\n%s", r.stdout)
	}
	vps.cmd.Process.Signal(syscall.SIGSTOP)
	frozen := time.Now()
	// Before the relay notices, the CLI can't reach it and says so.
	r = w.cli(2*time.Minute, "session", "get", s.SessionID, "-o", "json")
	var failed struct {
		Error struct{ Kind, Message string }
	}
	json.Unmarshal([]byte(r.stdout), &failed)
	t.Logf("session get right after vps-1 froze: exit %d after %v: %s: %s", r.code, time.Since(frozen).Round(time.Second), failed.Error.Kind, failed.Error.Message)
	if r.code == 0 {
		t.Fatalf("session get of a frozen machine's session: %+v", r)
	}
	away(s.SessionID)
	t.Logf("vps-1 offline %v after it froze", time.Since(frozen).Round(time.Second))
	vps.cmd.Process.Signal(syscall.SIGCONT)
	eventually(t, 2*time.Minute, "vps-1 back, the turn done, on the screen and the CLI", func() bool {
		x, m := onScreen(s.SessionID)
		got := w.get(s.SessionID)
		return m.GetUnreachable() == "" && m.GetOnline() && x.GetStatus() == sessionv1.SessionStatus_SESSION_STATUS_IDLE && got.Status == "idle" && got.Result == "finished" &&
			got.State == "done_unseen" && got.Link == "live" && !got.Stale
	})
	back()

	// aided stops in the middle of a turn, and starts again.
	s, r = w.startSession(time.Minute, "--project", "vps-1/demo", "--prompt", "sleep 10m")
	if s.SessionID == "" {
		t.Fatalf("start: %+v", r)
	}
	running(s.SessionID)
	restart(true)
	away(s.SessionID)
	restart(false)
	eventually(t, time.Minute, "vps-1 back, the session crashed, on the screen and the CLI", func() bool {
		x, m := onScreen(s.SessionID)
		got := w.get(s.SessionID)
		return m.GetUnreachable() == "" && x.GetStatus() == sessionv1.SessionStatus_SESSION_STATUS_CRASHED && got.Status == "crashed" && got.State == "failed" && got.Link == "live"
	})
	back()
	if got := w.get(s.SessionID); got.LastFailure == nil || got.LastFailure.Kind != "agent_crashed" || !got.LastFailure.Retryable {
		t.Fatalf("after the restart: %+v", got)
	}
}
