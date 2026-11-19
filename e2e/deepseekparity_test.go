package e2e

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	localv1 "github.com/hermeshai/hermesh/gen/go/hermesh/local/v1"
	sessionv1 "github.com/hermeshai/hermesh/gen/go/hermesh/session/v1"
)

// TestDeepSeekParity walks what an agent does with DeepSeek Harness through
// Hermesh (what Agents Anywhere did, WO #9) with the deepseek ACP agent,
// played by a scripted agent that speaks real ACP and, like DSH's `acp`
// profile, resumes and closes sessions: the machine is online for the agent
// and the owner's app; a session starts on machine/project with a prompt;
// the owner's app lists it, live, with who started it; a message sent while
// a turn runs is taken after it; a question is answered; a running turn is
// interrupted, the agent's session closed first (so DSH keeps it), and the
// next message goes on in the same agent session; an idle session is
// archived and brought back.
func TestDeepSeekParity(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	state := t.TempDir()
	w := newOwnerMac(t, []string{"HERMESH_DEEPSEEK_BIN=" + filepath.Join(binaries(t), "fakeacp"+exe), "FAKE_ACP_STATE=" + state,
		"FAKE_ACP_RESUME=resume", "FAKE_ACP_CLOSE=1"}, &localv1.GrantSpec{Agents: []string{"deepseek"}})
	ctx := context.Background()

	// 8: the machine is online, to the agent and in the owner's app.
	if r := w.cli(30*time.Second, "project", "list"); r.code != 0 || !strings.Contains(r.stdout, "owner-mac/demo") || !strings.Contains(r.stdout, "deepseek") || strings.Contains(r.stdout, "offline") {
		t.Fatalf("projects: %+v", r)
	}
	if res, err := w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{}); err != nil || len(res.Machines) != 1 || !res.Machines[0].Online {
		t.Fatalf("machines in the app: %v %v", res, err)
	}

	// 1: a session on machine/project, with its first prompt.
	r := w.cli(time.Minute, "session", "start", "--project", "owner-mac/demo", "--agent", "deepseek", "--prompt", "say hello from deepseek", "--wait", "-o", "json")
	s := firstSession(r.stdout)
	if r.code != 0 || s.Status != "idle" || s.Result != "hello from deepseek" {
		t.Fatalf("start: %+v", r)
	}
	id, ref := s.SessionID, s.Ref

	// 5: the owner's app lists it with who started it, and its conversation.
	appSession := func() *sessionv1.Session {
		res, err := w.app.client.ListSessions(ctx, &localv1.ListSessionsRequest{})
		if err != nil {
			t.Fatal(err)
		}
		for _, x := range res.Sessions {
			if x.SessionId == id {
				return x
			}
		}
		return nil
	}
	if a := appSession(); a == nil || a.Agent != "deepseek" || a.CreatedBy == "" || a.Status != sessionv1.SessionStatus_SESSION_STATUS_IDLE {
		t.Fatalf("in the app: %v", a)
	}
	if e := w.cli(30*time.Second, "session", "events", id, "--limit", "100"); !strings.Contains(e.stdout, "hello from deepseek") {
		t.Fatalf("events: %+v", e)
	}

	// 4: a message while a turn runs: ACP takes one prompt at a time, so it
	// is the next turn, in the same agent session.
	if r := w.cli(30*time.Second, "message", "send", "--to", id, "--message", "say working\nsleep 2s\nsay first done"); r.code != 0 {
		t.Fatalf("message: %+v", r)
	}
	eventually(t, 30*time.Second, "running", func() bool { return w.get(id).Status == "running" })
	if r := w.cli(30*time.Second, "message", "send", "--to", id, "--message", "say second"); r.code != 0 {
		t.Fatalf("message while running: %+v", r)
	}
	eventually(t, time.Minute, "the message's turn", func() bool { g := w.get(id); return g.Status == "idle" && g.Result == "second" })
	if e := w.cli(30*time.Second, "session", "events", id, "--limit", "200"); !strings.Contains(e.stdout, "first done") {
		t.Fatalf("the first turn: %+v", e)
	}

	// 4: the agent asks; the answer reaches it.
	if _, r := w.send(time.Minute, id, "ask Deploy now?|Yes,No"); r.code != 8 {
		t.Fatalf("question: %+v", r)
	}
	if r := w.cli(time.Minute, "session", "answer", id, "--item", "choice=Yes", "--item", "note=now", "--wait", "-o", "json"); r.code != 0 || firstSession(r.stdout).Result != "answer: v-yes now" {
		t.Fatalf("answer: %+v", r)
	}

	// 6: interrupt a running turn: the agent session is closed, not cut off,
	// and the next message resumes it.
	if r := w.cli(30*time.Second, "message", "send", "--to", id, "--message", "say starting\nsleep 60s"); r.code != 0 {
		t.Fatalf("long turn: %+v", r)
	}
	eventually(t, 30*time.Second, "running", func() bool { return w.get(id).Status == "running" })
	if r := w.cli(30*time.Second, "session", "interrupt", id); r.code != 0 {
		t.Fatalf("interrupt: %+v", r)
	}
	eventually(t, 15*time.Second, "idle after the interrupt", func() bool { return w.get(id).Status == "idle" })
	if b, _ := os.ReadFile(filepath.Join(state, "closed.log")); !strings.Contains(string(b), ref) {
		t.Fatalf("the agent session was not closed: %q", b)
	}
	if s, r := w.send(time.Minute, id, "history"); r.code != 0 || s.Ref != ref || !strings.Contains(s.Result, "history: ") {
		t.Fatalf("after the interrupt: %+v", r)
	}

	// 7: archive the idle session; it can be brought back.
	if r := w.cli(30*time.Second, "session", "archive", id); r.code != 0 || w.get(id).Status != "archived" {
		t.Fatalf("archive: %+v", r)
	}
	if a := appSession(); a == nil || a.Status != sessionv1.SessionStatus_SESSION_STATUS_ARCHIVED {
		t.Fatalf("archived in the app: %v", a)
	}
	if r := w.cli(30*time.Second, "session", "unarchive", id); r.code != 0 {
		t.Fatalf("unarchive: %+v", r)
	}
	if s, r := w.send(time.Minute, id, "say back"); r.code != 0 || s.Result != "back" || s.Ref != ref {
		t.Fatalf("after unarchiving: %+v", r)
	}
}
