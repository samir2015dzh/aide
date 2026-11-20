package e2e

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
)

// TestSessionRefsAndMacros drives session references (#s_…) and work-order
// macros (!plan, !implement) with the real CLI through the relay (borrowed
// item 13, docs/cli.md): the machine expands them before the turn, so the
// fake agent runs what the referenced session's result says, and the
// timeline's user message is the expanded text. Another agent, whose grant
// sees only its own sessions, is refused the reference; "@name" in a prompt
// stays text.
func TestSessionRefsAndMacros(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	w := newOwnerMac(t, []string{"AIDE_FAKE_PROVIDERS=1"}, &localv1.GrantSpec{})
	userTexts := func(env []string, id string) []string {
		t.Helper()
		r := run(t, env, 30*time.Second, filepath.Join(w.bin, "aide"), "session", "events", id, "--limit", "1000", "-o", "json")
		var doc struct {
			Events []struct{ Type, Text string }
		}
		if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil {
			t.Fatalf("events: %v %+v", err, r)
		}
		var out []string
		for _, e := range doc.Events {
			if e.Type == "user_message" {
				out = append(out, e.Text)
			}
		}
		return out
	}

	// An earlier session, whose final result is itself a line the fake
	// agent runs: the new session's agent runs it only if it got it.
	old, r := w.startSession(time.Minute, "--wait", "--prompt", "say say from the old session")
	if r.code != 0 || old.Result != "say from the old session" {
		t.Fatalf("old session: %+v", r)
	}
	s, r := w.startSession(time.Minute, "--wait", "--prompt", "carry on from #"+old.SessionID+" and ask @bot2")
	if r.code != 0 || s.Result != "from the old session" {
		t.Fatalf("reference not injected: %+v", r)
	}
	u := userTexts(w.cliEnv, s.SessionID)
	if len(u) != 1 || !strings.HasPrefix(u[0], "carry on from #"+old.SessionID+" and ask @bot2\n\n[Aide] Referenced session #"+old.SessionID) ||
		!strings.Contains(u[0], "Final result:\nsay from the old session") {
		t.Fatalf("timeline: %q", u)
	}

	// Into the session too, with a macro.
	got, r := w.send(time.Minute, s.SessionID, "!implement say implemented")
	if r.code != 0 || got.Result != "implemented" {
		t.Fatalf("!implement: %+v", r)
	}
	if u := userTexts(w.cliEnv, s.SessionID); !strings.HasPrefix(u[len(u)-1], "[Aide !implement] Implement the task below.") || !strings.HasSuffix(u[len(u)-1], "\n\nsay implemented") {
		t.Fatalf("!implement timeline: %q", u)
	}
	// And as a message to the session, the way agents write to one.
	if d, r := sendMessage(t, w, w.cliEnv, time.Minute, "--to", s.SessionID, "--message", "!plan again from #"+old.SessionID); !d.OK {
		t.Fatalf("message send: %+v", r)
	}
	if got := w.cli(time.Minute, "session", "wait", s.SessionID, "-o", "json"); firstSession(got.stdout).Result != "from the old session" {
		t.Fatalf("message send not expanded: %+v", got)
	}
	if r := w.cli(30*time.Second, "session", "events", s.SessionID, "--limit", "1000", "-o", "json"); !strings.Contains(r.stdout, `[Aide !plan] Plan only.`) ||
		!strings.Contains(r.stdout, `again from #`+old.SessionID+`\n\n[Aide] Referenced session #`+old.SessionID) {
		t.Fatalf("message send timeline: %+v", r)
	}

	p, r := w.startSession(time.Minute, "--wait", "--prompt", "!plan say planned")
	if r.code != 0 || p.Result != "planned" || p.Title != "!plan say planned" {
		t.Fatalf("!plan: %+v", r)
	}
	if u := userTexts(w.cliEnv, p.SessionID); len(u) != 1 || !strings.HasPrefix(u[0], "[Aide !plan] Plan only. Do not create, edit or delete any file") {
		t.Fatalf("!plan timeline: %q", u)
	}
	if r := w.cli(time.Minute, "session", "start", "--project", "demo", "--prompt", "!nope say x", "-o", "json"); r.code == 0 || !strings.Contains(r.stdout, "unknown macro !nope") {
		t.Fatalf("unknown macro: %+v", r)
	}

	// Another agent sees only its own sessions: the reference is refused,
	// as is one to no session.
	env2 := w.addAgent("bot2", nil)
	for _, ref := range []string{old.SessionID, "s_00000000_nosuch"} {
		r := run(t, env2, time.Minute, filepath.Join(w.bin, "aide"), "session", "start", "--project", "demo", "-o", "json", "--prompt", "look at #"+ref)
		if r.code == 0 || !strings.Contains(r.stdout, "referenced session #"+ref+" not found, or not visible to you") {
			t.Fatalf("bot2 referencing %s: %+v", ref, r)
		}
	}
}
