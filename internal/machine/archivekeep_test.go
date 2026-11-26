package machine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
)

// archiveReason is the reason of the session's last archived status.
func (h *harness) archiveReason(id string) string {
	why := ""
	for _, e := range h.events(id) {
		if st := e.GetStatus(); st != nil && st.Status == sessionv1.SessionStatus_SESSION_STATUS_ARCHIVED {
			why = st.Reason
		}
	}
	return why
}

// A worktree holding changes the last turn's snapshot lacks is kept when
// the session is archived, and the session says so; unarchiving goes on
// in it, and the next turn's snapshot takes the changes.
func TestArchiveKeepsUnsavedWorktree(t *testing.T) {
	h := newHarness(t, DefaultLimits())
	id := h.start("write a.txt one\nsay one")
	h.wait(id, idle)
	wt := h.s.ws.Path(id)
	if err := os.WriteFile(filepath.Join(wt, "a.txt"), []byte("edited by hand\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sess, err := h.s.Archive(context.Background(), h.p, id)
	if err != nil {
		t.Fatal(err)
	}
	if sess.Status != sessionv1.SessionStatus_SESSION_STATUS_ARCHIVED || !sess.WorkspaceKept {
		t.Fatalf("archive returned %v, workspace_kept %v", sess.Status, sess.WorkspaceKept)
	}
	if !h.s.ws.Exists(id) {
		t.Fatal("the worktree with unsaved changes was removed")
	}
	if got, err := h.s.Get(h.p, id); err != nil || !got.WorkspaceKept {
		t.Fatalf("Get after archive: kept %v, %v", got.GetWorkspaceKept(), err)
	}
	if why := h.archiveReason(id); !strings.Contains(why, "worktree kept: 2 paths") {
		t.Fatalf("archived status says %q", why)
	}

	sess, err = h.s.Unarchive(context.Background(), h.p, id)
	if err != nil {
		t.Fatal(err)
	}
	if sess.WorkspaceKept {
		t.Fatal("an unarchived session still says its worktree is kept")
	}
	if b, _ := os.ReadFile(filepath.Join(wt, "a.txt")); string(b) != "edited by hand\n" {
		t.Fatalf("a.txt after unarchive: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(wt, "new.txt")); string(b) != "new\n" {
		t.Fatalf("new.txt after unarchive: %q", b)
	}
	h.send(id, "write b.txt two\nsay two")
	h.wait(id, idle)
	diff, err := h.s.Diff(context.Background(), h.p, &sessionv1.GetDiffRequest{SessionId: id})
	if err != nil || !strings.Contains(diff.GetDiff(), "new.txt") || !strings.Contains(diff.GetDiff(), "edited by hand") {
		t.Fatalf("the kept changes are not in the next turn's snapshot: %v %v", diff.GetDiff(), err)
	}
	// Committed now: archiving frees the worktree.
	if sess, err := h.s.Archive(context.Background(), h.p, id); err != nil || sess.WorkspaceKept || h.s.ws.Exists(id) {
		t.Fatalf("second archive: kept %v, exists %v, %v", sess.GetWorkspaceKept(), h.s.ws.Exists(id), err)
	}
}

// A worktree whose files all are in the last turn's snapshot goes when the
// session is archived, as before; ignored files are not the snapshot's,
// and unarchiving checks the branch out again.
func TestArchiveFreesSavedWorktree(t *testing.T) {
	h := newHarness(t, DefaultLimits())
	id := h.start("write a.txt one\nsay one")
	h.wait(id, idle)
	proj, _ := h.s.st.Project("demo")
	if err := os.WriteFile(filepath.Join(proj.Path, ".git", "info", "exclude"), []byte("scratch.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt := h.s.ws.Path(id)
	if err := os.WriteFile(filepath.Join(wt, "scratch.log"), []byte("log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sess, err := h.s.Archive(context.Background(), h.p, id)
	if err != nil {
		t.Fatal(err)
	}
	if sess.WorkspaceKept || h.s.ws.Exists(id) {
		t.Fatalf("a saved worktree: kept %v, exists %v", sess.WorkspaceKept, h.s.ws.Exists(id))
	}
	if why := h.archiveReason(id); strings.Contains(why, "kept") {
		t.Fatalf("archived status says %q", why)
	}
	if _, err := h.s.Unarchive(context.Background(), h.p, id); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(wt, "a.txt")); string(b) != "one\n" {
		t.Fatalf("a.txt after unarchive: %q", b)
	}
}

// HEAD moved off the session branch to a commit of its own: that commit
// is not the last snapshot, so the worktree is kept.
func TestArchiveKeepsWorktreeOffItsBranch(t *testing.T) {
	h := newHarness(t, DefaultLimits())
	id := h.start("write a.txt one\nsay one")
	h.wait(id, idle)
	wt := h.s.ws.Path(id)
	os.WriteFile(filepath.Join(wt, "a.txt"), []byte("detached\n"), 0o644)
	for _, args := range [][]string{{"checkout", "-q", "--detach"}, {"commit", "-q", "-am", "by hand"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = wt
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	sess, err := h.s.Archive(context.Background(), h.p, id)
	if err != nil || !sess.WorkspaceKept || !h.s.ws.Exists(id) {
		t.Fatalf("archive: kept %v, exists %v, %v", sess.GetWorkspaceKept(), h.s.ws.Exists(id), err)
	}
	if why := h.archiveReason(id); !strings.Contains(why, "a.txt") {
		t.Fatalf("archived status says %q", why)
	}
}

// Deleting a session is for good: a worktree archiving kept goes too.
func TestDeleteRemovesKeptWorktree(t *testing.T) {
	h := newHarness(t, DefaultLimits())
	id := h.start("say one")
	h.wait(id, idle)
	os.WriteFile(filepath.Join(h.s.ws.Path(id), "new.txt"), []byte("new\n"), 0o644)
	if sess, err := h.s.Archive(context.Background(), h.p, id); err != nil || !sess.WorkspaceKept {
		t.Fatalf("archive: kept %v, %v", sess.GetWorkspaceKept(), err)
	}
	if err := h.s.Delete(context.Background(), &Principal{Kind: PrincipalOwner}, id); err != nil {
		t.Fatal(err)
	}
	if h.s.ws.Exists(id) {
		t.Fatal("the kept worktree outlived its deleted session")
	}
}
