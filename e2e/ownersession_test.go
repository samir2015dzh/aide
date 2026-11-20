package e2e

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
)

// The owner starts sessions from the app, as its New Session sheet does:
// on another machine through the relay, in a project there or in a GitHub
// repository that machine has never cloned, and on this Mac's own machine;
// then follows up, interrupts, and answers the agent's question. The
// sessions show as started by the owner, an agent can't send to them, and
// the owner still can't send to an agent's session. What the machine
// refuses (a repository it can't reach, a missing branch, an option the
// agent doesn't offer) comes back to the app as it said it, and starts
// nothing.
func TestOwnerSessions(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	gh := t.TempDir()
	env := append([]string{"AIDE_FAKE_PROVIDERS=1"}, githubEnv(gh)...)
	w := newOwnerMac(t, env, &localv1.GrantSpec{})
	ctx := context.Background()
	app := w.app.client

	// On GitHub: acme/widget, with main.
	bare := filepath.Join(gh, "acme", "widget.git")
	gitOut(t, w.repo, "init", "-q", "--bare", "-b", "main", bare)
	work := filepath.Join(t.TempDir(), "work")
	gitOut(t, w.repo, "clone", "-q", bare, work)
	gitOut(t, work, "checkout", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("# widget\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOut(t, work, "add", "README.md")
	gitOut(t, work, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "README")
	gitOut(t, work, "push", "-q", "origin", "main")

	// Another machine, reached through the relay, with a project.
	home, err := app.GetStatus(ctx, &localv1.GetStatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	other, _ := w.otherMac("work-mac", env)
	far := w.logInMachineOnly(other, home, "work-mac").LocalMachineId
	if _, err := other.AddProject(ctx, &localv1.AddProjectRequest{MachineId: far, ProjectId: "billing", Path: gitRepo(t, map[string]string{"README.md": "# billing\n"})}); err != nil {
		t.Fatal(err)
	}

	find := func(id string) *sessionv1.Session {
		res, err := app.ListSessions(ctx, &localv1.ListSessionsRequest{})
		if err != nil {
			return nil
		}
		for _, s := range res.Sessions {
			if s.SessionId == id {
				return s
			}
		}
		return nil
	}
	// waitTurn waits for the session to be idle after turn n, and returns
	// what the agent said last.
	waitTurn := func(id string, n uint32) string {
		t.Helper()
		var said string
		eventually(t, time.Minute, "turn done", func() bool {
			s := find(id)
			if s == nil || s.Status != sessionv1.SessionStatus_SESSION_STATUS_IDLE || s.Progress.GetTurnCount() < n {
				return false
			}
			evs, err := app.ListSessionEvents(ctx, &localv1.ListSessionEventsRequest{SessionId: id, Limit: 1000})
			if err != nil {
				return false
			}
			for _, e := range evs.Events {
				if m := e.GetAgentMessage(); m != nil {
					said = m.Text
				}
			}
			return true
		})
		return said
	}
	start := func(machine string, req *sessionv1.StartSessionRequest) (*sessionv1.Session, error) {
		res, err := app.StartSession(ctx, &localv1.StartSessionRequest{MachineId: machine, Session: req})
		return res.GetSession(), err
	}

	// In a project on work-mac. Its agents may not be checked yet at first.
	var s *sessionv1.Session
	eventually(t, time.Minute, "a session on work-mac", func() bool {
		s, err = start(far, &sessionv1.StartSessionRequest{ProjectId: "billing", Agent: "claude", Prompt: "say from the app"})
		return err == nil
	})
	if s.CreatedBy != "owner" || s.MachineId != far {
		t.Fatalf("started: %v", s)
	}
	if got := waitTurn(s.SessionId, 1); got != "from the app" {
		t.Fatalf("first turn said %q", got)
	}
	// Followed up, then interrupted mid-turn and followed up again.
	if _, err := app.SendMessage(ctx, &localv1.SendMessageRequest{SessionId: s.SessionId, Text: "say more"}); err != nil {
		t.Fatal(err)
	}
	if got := waitTurn(s.SessionId, 2); got != "more" {
		t.Fatalf("follow-up said %q", got)
	}
	if _, err := app.SendMessage(ctx, &localv1.SendMessageRequest{SessionId: s.SessionId, Text: "sleep 60s\nsay too late"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, 30*time.Second, "running", func() bool {
		x := find(s.SessionId)
		return x != nil && x.Status == sessionv1.SessionStatus_SESSION_STATUS_RUNNING
	})
	if _, err := app.InterruptSession(ctx, &localv1.InterruptSessionRequest{SessionId: s.SessionId}); err != nil {
		t.Fatal(err)
	}
	if got := waitTurn(s.SessionId, 3); got == "too late" {
		t.Fatal("the interrupted turn went on")
	}
	if _, err := app.SendMessage(ctx, &localv1.SendMessageRequest{SessionId: s.SessionId, Text: "say after the stop"}); err != nil {
		t.Fatal(err)
	}
	if got := waitTurn(s.SessionId, 4); got != "after the stop" {
		t.Fatalf("after the interrupt said %q", got)
	}

	// The agent's question, answered from the app.
	if _, err := app.SendMessage(ctx, &localv1.SendMessageRequest{SessionId: s.SessionId, Text: "ask Pick|Which one?|red,blue"}); err != nil {
		t.Fatal(err)
	}
	var q *sessionv1.Question
	eventually(t, 30*time.Second, "the question", func() bool {
		x := find(s.SessionId)
		q = x.GetProgress().GetPendingQuestion()
		return q != nil
	})
	if _, err := app.AnswerQuestion(ctx, &localv1.AnswerQuestionRequest{SessionId: s.SessionId, QuestionId: q.QuestionId,
		Answers: []*sessionv1.Answer{{ItemId: q.Items[0].Id, Selected: []string{"blue"}}}}); err != nil {
		t.Fatal(err)
	}
	if got := waitTurn(s.SessionId, 5); !strings.Contains(got, "answer: blue") {
		t.Fatalf("after the answer said %q", got)
	}

	// In a GitHub repository work-mac never cloned.
	r, err := start(far, &sessionv1.StartSessionRequest{Repo: "acme/widget", Agent: "codex", Prompt: "say in the repo"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Repo != "acme/widget" || r.RepoBranch != "main" || r.ProjectId != "github.acme.widget" || r.CreatedBy != "owner" {
		t.Fatalf("repo session: %v", r)
	}
	if got := waitTurn(r.SessionId, 1); got != "in the repo" {
		t.Fatalf("repo session said %q", got)
	}
	if _, err := app.SendMessage(ctx, &localv1.SendMessageRequest{SessionId: r.SessionId, Text: "say again"}); err != nil {
		t.Fatal(err)
	}
	if got := waitTurn(r.SessionId, 2); got != "again" {
		t.Fatalf("repo follow-up said %q", got)
	}

	// On this Mac's own machine, in process.
	local, err := start(home.LocalMachineId, &sessionv1.StartSessionRequest{ProjectId: "demo", Agent: "claude", Prompt: "say here"})
	if err != nil {
		t.Fatal(err)
	}
	if got := waitTurn(local.SessionId, 1); got != "here" || local.CreatedBy != "owner" {
		t.Fatalf("local session said %q: %v", got, local)
	}
	if _, err := app.SendMessage(ctx, &localv1.SendMessageRequest{SessionId: local.SessionId, Text: "say here again"}); err != nil {
		t.Fatal(err)
	}
	if got := waitTurn(local.SessionId, 2); got != "here again" {
		t.Fatalf("local follow-up said %q", got)
	}

	// The agent can't send to the owner's session; the owner can't send to
	// the agent's.
	if res := w.cli(time.Minute, "session", "send", s.SessionId, "--message", "say hijack"); res.code == 0 {
		t.Fatalf("an agent sent to the owner's session: %s", res.stdout)
	}
	theirs, res := w.startSession(time.Minute, "--wait", "--prompt", "say mine")
	if res.code != 0 {
		t.Fatalf("the agent's session: %s", res.stdout+res.stderr)
	}
	if _, err := app.SendMessage(ctx, &localv1.SendMessageRequest{SessionId: theirs.SessionID, Text: "say hijack"}); connect.CodeOf(err) != connect.CodePermissionDenied ||
		!strings.Contains(err.Error(), "started by an agent") {
		t.Fatalf("the owner sent to an agent's session: %v", err)
	}

	// What the machine refuses, as it said it; nothing starts.
	count := func() int {
		res, err := app.ListSessions(ctx, &localv1.ListSessionsRequest{})
		if err != nil {
			t.Fatal(err)
		}
		return len(res.Sessions)
	}
	before := count()
	for _, c := range []struct {
		req  *sessionv1.StartSessionRequest
		code connect.Code
		say  string
	}{
		{&sessionv1.StartSessionRequest{Repo: "acme/private", Agent: "claude", Prompt: "say never"}, connect.CodeFailedPrecondition, "cannot reach GitHub repository acme/private"},
		{&sessionv1.StartSessionRequest{Repo: "acme/widget", BaseRef: "gone", Agent: "claude", Prompt: "say never"}, connect.CodeNotFound, "has no branch"},
		{&sessionv1.StartSessionRequest{ProjectId: "billing", Agent: "claude", Mode: "plan", Prompt: "say never"}, connect.CodeInvalidArgument, "offers no session modes"},
		{&sessionv1.StartSessionRequest{ProjectId: "nope", Agent: "claude", Prompt: "say never"}, connect.CodeNotFound, `project "nope" does not exist`},
	} {
		if _, err := start(far, c.req); connect.CodeOf(err) != c.code || !strings.Contains(err.Error(), c.say) {
			t.Errorf("%v: %v", c.req, err)
		}
	}
	if after := count(); after != before {
		t.Fatalf("a refused start made a session: %d, then %d", before, after)
	}
}
