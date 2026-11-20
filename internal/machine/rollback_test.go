package machine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect"

	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
)

func (h *harness) rollback(id string, turn uint32) (*sessionv1.Session, error) {
	return h.s.Rollback(context.Background(), h.p, id, turn)
}

// Rolling back puts the worktree and branch where the turn left them,
// forgets the later turns (leaving a RolledBack event after them), and
// the next turn starts a fresh agent conversation that is told so.
func TestRollbackToAnEarlierTurn(t *testing.T) {
	h := newHarness(t, DefaultLimits())
	id := h.start("write a.txt one\nsay 1")
	h.wait(id, idle)
	h.send(id, "write b.txt two\nsay 2")
	h.wait(id, idle)
	h.send(id, "write a.txt changed\nwrite c.txt three\nsay 3")
	h.wait(id, idle)
	first, err := h.s.st.TurnCommit(id, 1)
	if err != nil || first == "" {
		t.Fatalf("turn 1 commit %q: %v", first, err)
	}
	third, _ := h.s.st.TurnCommit(id, 3)
	refs := h.rec.count()
	owner := &Principal{Kind: PrincipalOwner}
	if _, err := h.s.MarkSeen(context.Background(), owner, &sessionv1.MarkSessionSeenRequest{SessionId: id, Opened: true}); err != nil {
		t.Fatal(err)
	}

	sess, err := h.rollback(id, 1)
	if err != nil {
		t.Fatal(err)
	}
	if sess.Status != idle || sess.Progress.TurnCount != 1 || sess.ProviderSessionRef != "" || sess.Result != "1" {
		t.Fatalf("after the rollback: %v", sess)
	}
	// The session's changes are turn 1's again, as Aide counts them from its base.
	row, err := h.s.st.Session(id)
	if err != nil {
		t.Fatal(err)
	}
	want, err := diffStat(context.Background(), h.s.ws.Path(id), row.BaseCommit, first)
	if err != nil {
		t.Fatal(err)
	}
	if d := sess.Progress.DiffStat; d.GetFilesChanged() != want.FilesChanged || d.GetInsertions() != want.Insertions || d.GetDeletions() != want.Deletions {
		t.Fatalf("diff stat: %v, want %v", d, want)
	}
	wt := h.s.ws.Path(id)
	// checked out again, with the line endings git gives files here (CRLF on Windows)
	if b, _ := os.ReadFile(filepath.Join(wt, "a.txt")); strings.ReplaceAll(string(b), "\r\n", "\n") != "one\n" {
		t.Fatalf("a.txt: %q", b)
	}
	for _, f := range []string{"b.txt", "c.txt"} {
		if _, err := os.Stat(filepath.Join(wt, f)); !os.IsNotExist(err) {
			t.Fatalf("%s is still there: %v", f, err)
		}
	}
	if head, _ := resolveRef(context.Background(), wt, "refs/heads/"+sess.Branch); head != first {
		t.Fatalf("branch at %s, want %s", head, first)
	}
	if sess.Seen.GetTurns() != 1 {
		t.Fatalf("seen after the rollback: %v", sess.Seen)
	}
	if c, _ := h.s.st.TurnCommit(id, 2); c != "" {
		t.Fatalf("turn 2 is still recorded: %s", c)
	}
	var rb *sessionv1.RolledBack
	for _, e := range h.events(id) {
		if r := e.GetRolledBack(); r != nil {
			rb = r
		}
	}
	if rb == nil || rb.Turn != 1 || rb.LastTurn != 3 || rb.Commit != first || rb.DiscardedCommit != third || rb.By != "c_test" {
		t.Fatalf("rolled back event: %v", rb)
	}

	h.send(id, "write d.txt four\nsay 4")
	h.wait(id, idle)
	if h.rec.count() != refs+1 {
		t.Fatalf("starts: %d, want %d", h.rec.count(), refs+1)
	}
	spec := h.rec.spec(-1)
	if spec.ResumeRef != "" || !strings.Contains(spec.Prompt, "rolled back to the end of turn 1: turns 2 to 3 were discarded") {
		t.Fatalf("the next turn's start: resume %q, prompt %q", spec.ResumeRef, spec.Prompt)
	}
	if tc := h.lastTurn(id); tc.Turn != 2 || tc.IsError {
		t.Fatalf("the next turn: %v", tc)
	}
	if b, _ := os.ReadFile(filepath.Join(wt, "d.txt")); string(b) != "four\n" {
		t.Fatalf("d.txt: %q", b)
	}
	h.stopProcess(id) // the next turn resumes the conversation, untold
	h.send(id, "say 5")
	h.wait(id, idle)
	if spec := h.rec.spec(-1); spec.ResumeRef == "" || strings.Contains(spec.Prompt, "rolled back") {
		t.Fatalf("turn 3's start: resume %q, prompt %q", spec.ResumeRef, spec.Prompt)
	}
}

func TestRollbackRefused(t *testing.T) {
	h := newHarness(t, DefaultLimits())
	id := h.start("write n.txt one\nsay 1")
	h.wait(id, idle)
	if _, err := h.rollback(id, 1); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("to the last turn: %v", err)
	}
	h.send(id, "sleep 10s")
	h.wait(id, sessionv1.SessionStatus_SESSION_STATUS_RUNNING)
	if _, err := h.rollback(id, 0); connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "interrupt it first") {
		t.Fatalf("while running: %v", err)
	}
	h.s.Interrupt(context.Background(), h.p, id)
	h.wait(id, idle)
	if _, err := h.rollback(id, 0); err != nil {
		t.Fatalf("to the base: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.s.ws.Path(id), "n.txt")); !os.IsNotExist(err) {
		t.Fatalf("n.txt after rolling every turn back: %v", err)
	}

	dir := t.TempDir()
	h.inPlaceProject("notes", dir)
	ip := h.startIn("notes", "write x.txt x\nsay x")
	h.wait(ip, idle)
	h.send(ip, "say y")
	h.wait(ip, idle)
	if _, err := h.rollback(ip, 1); connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "works in place") {
		t.Fatalf("in place: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "x.txt")); string(b) != "x\n" {
		t.Fatalf("the in-place directory was touched: %q", b)
	}
}
