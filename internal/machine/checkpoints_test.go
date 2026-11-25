package machine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/provider"
	"github.com/samir2015dzh/aide/internal/provider/acp"
)

type noRollbackProvider struct {
	provider.Provider
	reason string
}

func (n *noRollbackProvider) CanRollbackConversation() (bool, string) {
	return false, n.reason
}

func harnessRepoPath(t *testing.T, h *harness) string {
	t.Helper()
	projs, err := h.s.st.Projects()
	if err != nil || len(projs) == 0 {
		t.Fatalf("projects: %+v err=%v", projs, err)
	}
	return projs[0].Path
}

func initTestGitDir(t *testing.T, dir string) {
	t.Helper()
	ctx := context.Background()
	if _, err := git(ctx, dir, "init"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("init\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := git(ctx, dir, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := git(ctx, dir, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "init"); err != nil {
		t.Fatal(err)
	}
}

// TestCheckpointsGitMultiTurnAndRollback verifies #113 end-to-end on a real git
// project: per-turn hidden refs under refs/aide/checkpoints/<session>/turn-<N>,
// user branch history unpolluted, untracked non-ignored files included and
// gitignored files untouched, and consistent conversation + file rollback.
func TestCheckpointsGitMultiTurnAndRollback(t *testing.T) {
	h := newHarness(t, DefaultLimits())
	ctx := context.Background()
	repo := harnessRepoPath(t, h)

	// Add a .gitignore entry on the repo and commit it on main before starting.
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("*.ignored\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := git(ctx, repo, "add", ".gitignore"); err != nil {
		t.Fatal(err)
	}
	if _, err := git(ctx, repo, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "add gitignore"); err != nil {
		t.Fatal(err)
	}
	userHeadBefore, err := git(ctx, repo, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	userLogBefore, err := git(ctx, repo, "log", "--oneline", "HEAD")
	if err != nil {
		t.Fatal(err)
	}

	id := h.start("write a.txt v1\nsay turn1")
	h.wait(id, idle)

	// Drop an ignored file inside the session worktree; checkpoints must not
	// capture or delete it on rollback.
	ignoredPath := filepath.Join(h.s.ws.Path(id), "local.ignored")
	if err := os.WriteFile(ignoredPath, []byte("keep-me\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	h.send(id, "write b.txt v2\nsay turn2")
	h.wait(id, idle)
	h.send(id, "say turn3-no-edits")
	h.wait(id, idle)

	// User branch must remain completely untouched by checkpoints.
	userHeadAfter, _ := git(ctx, repo, "rev-parse", "HEAD")
	userLogAfter, _ := git(ctx, repo, "log", "--oneline", "HEAD")
	if strings.TrimSpace(userHeadAfter) != strings.TrimSpace(userHeadBefore) || userLogAfter != userLogBefore {
		t.Fatalf("user branch polluted: before=%q after=%q\nlog before:\n%s\nlog after:\n%s", userHeadBefore, userHeadAfter, userLogBefore, userLogAfter)
	}

	resp, err := h.s.ListCheckpoints(ctx, h.p, id)
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Enabled || resp.DisabledReason != "" {
		t.Fatalf("expected enabled checkpoints, got enabled=%v reason=%q", resp.Enabled, resp.DisabledReason)
	}
	if !resp.CanRollback || resp.RollbackRefusedReason != "" {
		t.Fatalf("expected rollback allowed, got canRollback=%v reason=%q", resp.CanRollback, resp.RollbackRefusedReason)
	}
	if resp.Policy.GetMaxCount() != DefaultCheckpointMaxCount || resp.Policy.GetMaxAgeDays() != DefaultCheckpointMaxAgeDays || resp.Policy.GetUntrackedPolicy() != CheckpointUntrackedPolicy {
		t.Fatalf("unexpected policy: %+v", resp.Policy)
	}
	if len(resp.Checkpoints) != 3 {
		t.Fatalf("checkpoints count=%d, want 3: %+v", len(resp.Checkpoints), resp.Checkpoints)
	}
	for i, cp := range resp.Checkpoints {
		wantTurn := uint32(i + 1)
		wantRef := checkpointRefName(id, wantTurn)
		if cp.Turn != wantTurn || cp.Ref != wantRef || cp.Commit == "" || cp.CreatedAt == nil {
			t.Fatalf("cp[%d] unexpected: %+v", i, cp)
		}
	}
	if !resp.Checkpoints[0].Changed || !resp.Checkpoints[1].Changed || resp.Checkpoints[2].Changed {
		t.Fatalf("changed flags = [%v, %v, %v], want [true, true, false]",
			resp.Checkpoints[0].Changed, resp.Checkpoints[1].Changed, resp.Checkpoints[2].Changed)
	}

	// Verify gitignored file was not committed into checkpoint turn-2.
	lsTree, err := git(ctx, repo, "ls-tree", "-r", "--name-only", resp.Checkpoints[1].Ref)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lsTree, "a.txt") || !strings.Contains(lsTree, "b.txt") || strings.Contains(lsTree, "local.ignored") {
		t.Fatalf("unexpected tree contents in turn-2 checkpoint:\n%s", lsTree)
	}

	// Roll back to turn 1: files, conversation state, and checkpoint refs must align.
	sess, err := h.rollback(id, 1)
	if err != nil {
		t.Fatal(err)
	}
	if sess.Progress.TurnCount != 1 {
		t.Fatalf("turn count after rollback=%d, want 1", sess.Progress.TurnCount)
	}
	if _, err := os.Stat(filepath.Join(h.s.ws.Path(id), "b.txt")); !os.IsNotExist(err) {
		t.Fatalf("b.txt should be gone after rollback to turn 1")
	}
	if b, err := os.ReadFile(ignoredPath); err != nil || string(b) != "keep-me\n" {
		t.Fatalf("gitignored file should remain untouched, got %q err=%v", b, err)
	}
	afterRB, err := h.s.ListCheckpoints(ctx, h.p, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterRB.Checkpoints) != 1 || afterRB.Checkpoints[0].Turn != 1 {
		t.Fatalf("checkpoints after rollback to turn 1: %+v", afterRB.Checkpoints)
	}
}

// TestCheckpointsRefusedWhenProviderCannotRollback verifies that when a provider
// does not support conversation rollback (such as ACP providers), Aide refuses
// rollback with a clear explanation and leaves workspace files untouched.
func TestCheckpointsRefusedWhenProviderCannotRollback(t *testing.T) {
	// First assert real ACP provider reports unsupported conversation rollback.
	acpProv := &acp.Provider{Agent: acp.Agent{Name: "antigravity"}}
	if ok, msg := acpProv.CanRollbackConversation(); ok || !strings.Contains(msg, "conversation rollback") {
		t.Fatalf("acp.Provider.CanRollbackConversation() = (%v, %q), want false with explanation", ok, msg)
	}

	h := newHarness(t, DefaultLimits())
	ctx := context.Background()
	baseProv := h.s.provider
	h.s.provider = func(name string) provider.Provider {
		p := baseProv(name)
		if p == nil {
			return nil
		}
		return &noRollbackProvider{
			Provider: p,
			reason:   "provider does not support conversation rollback; refusing file-only rollback",
		}
	}

	id := h.start("write f.txt one\nsay 1")
	h.wait(id, idle)
	h.send(id, "write f.txt two\nsay 2")
	h.wait(id, idle)

	list, err := h.s.ListCheckpoints(ctx, h.p, id)
	if err != nil {
		t.Fatal(err)
	}
	if !list.Enabled || list.CanRollback || !strings.Contains(list.RollbackRefusedReason, "conversation rollback") {
		t.Fatalf("unexpected capability report: enabled=%v canRollback=%v reason=%q",
			list.Enabled, list.CanRollback, list.RollbackRefusedReason)
	}

	_, err = h.rollback(id, 1)
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "conversation rollback") {
		t.Fatalf("expected FailedPrecondition refusal, got %v", err)
	}
	// Files and turns must remain completely untouched.
	b, readErr := os.ReadFile(filepath.Join(h.s.ws.Path(id), "f.txt"))
	if readErr != nil || strings.ReplaceAll(string(b), "\r\n", "\n") != "two\n" {
		t.Fatalf("file was modified despite refused rollback: %q err=%v", b, readErr)
	}
	row, _ := h.s.st.Session(id)
	if row.Progress.TurnCount != 2 {
		t.Fatalf("turn count changed after refused rollback: %d", row.Progress.TurnCount)
	}
}

// TestCheckpointsNonGitAndInPlaceExplicitlyDisabled verifies non-git projects
// and in-place git projects never silently omit checkpoints: ListCheckpoints
// reports enabled=false with an explicit reason.
func TestCheckpointsNonGitAndInPlaceExplicitlyDisabled(t *testing.T) {
	h := newHarness(t, DefaultLimits())
	ctx := context.Background()

	plainDir := t.TempDir()
	h.inPlaceProject("plain", plainDir)
	id := h.startIn("plain", "write note.txt hi\nsay ok")
	h.wait(id, idle)

	resp, err := h.s.ListCheckpoints(ctx, h.p, id)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Enabled || resp.CanRollback || !strings.Contains(resp.DisabledReason, "non-git") {
		t.Fatalf("non-git project checkpoint report: enabled=%v canRollback=%v reason=%q",
			resp.Enabled, resp.CanRollback, resp.DisabledReason)
	}

	// Explicit in-place git repo also explains why checkpoints are disabled.
	gitInPlace := t.TempDir()
	initTestGitDir(t, gitInPlace)
	h.inPlaceProject("git-inplace", gitInPlace)
	id2 := h.startIn("git-inplace", "say hi")
	h.wait(id2, idle)

	resp2, err := h.s.ListCheckpoints(ctx, h.p, id2)
	if err != nil {
		t.Fatal(err)
	}
	if resp2.Enabled || resp2.CanRollback || !strings.Contains(resp2.DisabledReason, "in-place") {
		t.Fatalf("in-place git checkpoint report: enabled=%v canRollback=%v reason=%q",
			resp2.Enabled, resp2.CanRollback, resp2.DisabledReason)
	}
}

// TestCheckpointsRetentionAndManualClear verifies automatic max_count pruning
// and manual ClearCheckpoints (both keep_latest > 0 and keep_latest = 0).
func TestCheckpointsRetentionAndManualClear(t *testing.T) {
	h := newHarness(t, DefaultLimits())
	h.s.checkpoints = CheckpointRetention{MaxCount: 2, MaxAgeDays: 14}
	ctx := context.Background()
	repo := harnessRepoPath(t, h)

	id := h.start("write c.txt 1\nsay 1")
	h.wait(id, idle)
	h.send(id, "write c.txt 2\nsay 2")
	h.wait(id, idle)
	h.send(id, "write c.txt 3\nsay 3")
	h.wait(id, idle)

	resp, err := h.s.ListCheckpoints(ctx, h.p, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Checkpoints) != 2 || resp.Checkpoints[0].Turn != 2 || resp.Checkpoints[1].Turn != 3 {
		t.Fatalf("expected retention MaxCount=2 to keep turns [2,3], got %+v", resp.Checkpoints)
	}

	// Verify age pruning helper directly.
	if removed, err := h.s.ws.PruneCheckpoints(ctx, repo, id, CheckpointRetention{MaxCount: 20, MaxAgeDays: 1}, time.Now().Add(48*time.Hour)); err != nil || removed != 2 {
		t.Fatalf("age prune removed=%d err=%v, want 2", removed, err)
	}

	// Re-create two checkpoints and verify manual ClearCheckpoints.
	c2, _ := h.s.st.TurnCommit(id, 2)
	c3, _ := h.s.st.TurnCommit(id, 3)
	_ = h.s.ws.WriteCheckpoint(ctx, repo, id, 2, c2)
	_ = h.s.ws.WriteCheckpoint(ctx, repo, id, 3, c3)

	clr, err := h.s.ClearCheckpoints(ctx, h.p, id, 1)
	if err != nil {
		t.Fatal(err)
	}
	if clr.Removed != 1 || clr.Remaining != 1 {
		t.Fatalf("clear keep_latest=1: %+v", clr)
	}
	clrAll, err := h.s.ClearCheckpoints(ctx, h.p, id, 0)
	if err != nil {
		t.Fatal(err)
	}
	if clrAll.Removed != 1 || clrAll.Remaining != 0 {
		t.Fatalf("clear keep_latest=0: %+v", clrAll)
	}
	after, _ := h.s.ListCheckpoints(ctx, h.p, id)
	if len(after.Checkpoints) != 0 {
		t.Fatalf("expected 0 checkpoints after clear all, got %d", len(after.Checkpoints))
	}
	var _ *sessionv1.CheckpointPolicy = after.Policy
}
