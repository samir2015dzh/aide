package e2e

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
)

// TestSessionRollback: an agent rolls its session back with the CLI as
// a user would, after three turns, to the first: the worktree and branch
// are as turn 1 left them, the discarded turns stay in the timeline with
// a rolled_back event after them, and the session goes on from turn 2.
// It is refused without confirmation, while a turn runs, and in place;
// the owner's app rolls back through the local API.
func TestSessionRollback(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	w := newOwnerMac(t, []string{"AIDE_FAKE_PROVIDERS=1"}, &localv1.GrantSpec{})
	ctx := context.Background()
	cli := func(args ...string) result { return w.cli(time.Minute, args...) }
	sessionDoc := func(r result) map[string]any {
		t.Helper()
		doc := jsonDoc(t, r)
		s, _ := doc["session"].(map[string]any)
		if s == nil {
			t.Fatalf("no session: %+v", r)
		}
		return s
	}
	commits := func(id string) map[float64]string { // turn → commit, of the turns kept
		out := map[float64]string{}
		for _, e := range jsonDoc(t, cli("session", "events", id, "-o", "json", "--limit", "500"))["events"].([]any) {
			ev := e.(map[string]any)
			switch ev["type"] {
			case "turn_completed":
				out[ev["turn"].(float64)] = ev["commit"].(string)
			case "rolled_back":
				for turn := range out {
					if turn > ev["to_turn"].(float64) {
						delete(out, turn)
					}
				}
			}
		}
		return out
	}

	s, r := w.startSession(time.Minute, "--wait", "--prompt", "write a.txt one\nsay first")
	if r.code != 0 {
		t.Fatalf("start: %+v", r)
	}
	id := s.SessionID
	for _, msg := range []string{"write b.txt two\nsay second", "write a.txt changed\nwrite c.txt three\nsay third"} {
		if _, r := w.send(time.Minute, id, msg); r.code != 0 {
			t.Fatalf("send: %+v", r)
		}
	}
	turns := commits(id)
	branch := sessionDoc(cli("session", "get", id, "-o", "json"))["branch"].(string)
	if len(turns) != 3 || gitOut(t, w.repo, "rev-parse", branch) != turns[3] {
		t.Fatalf("after three turns: %v, branch %s", turns, branch)
	}

	// Without --yes it says what would be lost and does nothing.
	r = cli("session", "rollback", id, "--turn", "1", "-o", "json")
	if e, _ := jsonDoc(t, r)["error"].(map[string]any); r.code != 2 || e["kind"] != "usage" ||
		!strings.Contains(e["message"].(string), "discards every message and change after it (turns 2 to 3)") || !strings.Contains(e["hint"].(string), "--yes") {
		t.Fatalf("rollback without --yes: %+v", r)
	}
	if gitOut(t, w.repo, "rev-parse", branch) != turns[3] {
		t.Fatal("rolled back without --yes")
	}

	r = cli("session", "rollback", id, "--turn", "1", "--yes", "-o", "json")
	if r.code != 0 {
		t.Fatalf("rollback: %+v", r)
	}
	if got := sessionDoc(r); got["status"] != "idle" || got["turns"].(float64) != 1 || got["result"] != "first" {
		t.Fatalf("session after the rollback: %v", got)
	}
	wt := w.worktree(id)
	// checked out again, with the line endings git gives files here (CRLF on Windows)
	if b, _ := os.ReadFile(filepath.Join(wt, "a.txt")); strings.ReplaceAll(string(b), "\r\n", "\n") != "one\n" {
		t.Fatalf("a.txt: %q", b)
	}
	for _, f := range []string{"b.txt", "c.txt"} {
		if _, err := os.Stat(filepath.Join(wt, f)); !os.IsNotExist(err) {
			t.Fatalf("%s survived the rollback: %v", f, err)
		}
	}
	if head := gitOut(t, w.repo, "rev-parse", branch); head != turns[1] {
		t.Fatalf("branch at %s, want turn 1's %s", head, turns[1])
	}
	if st := gitOut(t, wt, "status", "--porcelain"); st != "" {
		t.Fatalf("worktree not clean: %s", st)
	}
	if gitOut(t, w.repo, "rev-parse", "HEAD") != w.repoHead {
		t.Fatal("the rollback moved the project's own HEAD")
	}
	ev := cli("session", "events", id)
	if i, j := strings.Index(ev.stdout, "third"), strings.Index(ev.stdout, "rolled back to turn 1"); i < 0 || j < i {
		t.Fatalf("timeline after the rollback:\n%s", ev.stdout)
	}

	// The session goes on from turn 2, in a fresh conversation told why.
	if s, r := w.send(time.Minute, id, "write d.txt four\nsay fourth"); r.code != 0 || s.Result != "fourth" {
		t.Fatalf("after the rollback: %+v", r)
	}
	turns = commits(id)
	if len(turns) != 2 || gitOut(t, w.repo, "rev-parse", branch) != turns[2] || gitOut(t, w.repo, "rev-parse", turns[2]+"^") != turns[1] {
		t.Fatalf("turns after going on: %v", turns)
	}
	if b, _ := os.ReadFile(filepath.Join(wt, "d.txt")); string(b) != "four\n" {
		t.Fatalf("d.txt: %q", b)
	}
	if d := cli("session", "diff", id, "--turn", "3"); d.code == 0 {
		t.Fatalf("a discarded turn's diff: %+v", d)
	}

	// Not while a turn runs.
	if r := cli("session", "send", id, "--message", "sleep 60s"); r.code != 0 {
		t.Fatalf("send: %+v", r)
	}
	eventually(t, 30*time.Second, "turn running", func() bool { return w.get(id).Status == "running" })
	r = cli("session", "rollback", id, "--turn", "1", "--yes", "-o", "json")
	if e, _ := jsonDoc(t, r)["error"].(map[string]any); r.code != 2 || e["kind"] != "invalid_state" || !strings.Contains(e["message"].(string), "interrupt it first") {
		t.Fatalf("rollback during a turn: %+v", r)
	}
	cli("session", "interrupt", id)
	eventually(t, 30*time.Second, "idle", func() bool { return w.get(id).Status == "idle" })

	// The owner's app, through the local API: back to the base.
	res, err := w.app.client.RollbackSession(ctx, &localv1.RollbackSessionRequest{SessionId: id, Turn: 0})
	if err != nil || res.Session.GetProgress().GetTurnCount() != 0 {
		t.Fatalf("owner rollback: %v %v", res, err)
	}
	if head := gitOut(t, w.repo, "rev-parse", branch); head != w.repoHead {
		t.Fatalf("branch at %s after rolling every turn back, want %s", head, w.repoHead)
	}
	if _, err := os.Stat(filepath.Join(wt, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("a.txt after rolling every turn back: %v", err)
	}

	// In place: refused, saying why, and the directory is left alone.
	machines, err := w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	dir := gitRepo(t, map[string]string{"notes.md": "mine\n"})
	if _, err := w.app.client.AddProject(ctx, &localv1.AddProjectRequest{MachineId: machines.Machines[0].MachineId, ProjectId: "live", Path: dir,
		Workspace: identityv1.Workspace_WORKSPACE_IN_PLACE}); err != nil {
		t.Fatal(err)
	}
	r = cli("session", "start", "--project", "live", "--wait", "-o", "json", "--prompt", "write todo.txt x\nsay x")
	if r.code != 0 {
		t.Fatalf("start in place: %+v", r)
	}
	ip := sessionDoc(r)["session_id"].(string)
	if _, r := w.send(time.Minute, ip, "say y"); r.code != 0 {
		t.Fatalf("send in place: %+v", r)
	}
	r = cli("session", "rollback", ip, "--turn", "1", "--yes", "-o", "json")
	if e, _ := jsonDoc(t, r)["error"].(map[string]any); r.code != 2 || e["kind"] != "invalid_state" || !strings.Contains(e["message"].(string), "works in place") {
		t.Fatalf("rollback in place: %+v", r)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "todo.txt")); string(b) != "x\n" {
		t.Fatalf("todo.txt in place: %q", b)
	}
}
