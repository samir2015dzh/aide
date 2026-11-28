package machine

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/provider"
)

// claudeAppSession writes what the Claude desktop app and Claude Code keep
// of a session they had without Aide: the app's record and the transcript.
func claudeAppSession(t *testing.T, home, config, id, cliID, cwd string) {
	t.Helper()
	dir := filepath.Join(home, "Library", "Application Support", "Claude", "claude-code-sessions", "acct", "org")
	os.MkdirAll(dir, 0o755)
	now := time.Now().UnixMilli()
	rec, _ := json.Marshal(map[string]any{"sessionId": id, "cliSessionId": cliID, "title": "Rust 迁移难点", "cwd": cwd, "createdAt": now, "lastActivityAt": now})
	if err := os.WriteFile(filepath.Join(dir, id+".json"), rec, 0o600); err != nil {
		t.Fatal(err)
	}
	tdir := filepath.Join(config, "projects", "-x")
	os.MkdirAll(tdir, 0o755)
	line, _ := json.Marshal(map[string]any{"type": "user", "sessionId": cliID, "timestamp": time.Now().Format(time.RFC3339Nano), "message": map[string]any{"role": "user", "content": "看看迁移"}})
	os.WriteFile(filepath.Join(tdir, cliID+".jsonl"), append(line, '\n'), 0o600)
}

// TestContinueClaudeSession (工单 #181): a Claude Code session of the app's,
// in a worktree of a project's repository, goes on in Aide: a session of
// Aide's that resumes Claude Code's own id in that worktree, the owner's
// message its turn; it stays in the app's list saying so, and going on
// with it again is that session. A folder no project holds, or
// gone, is said so, with the folder.
func TestContinueClaudeSession(t *testing.T) {
	claude := &recorder{}
	h := newHarnessWith(t, DefaultLimits(), map[string]provider.Provider{"claude": claude})
	home, config := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", config)
	proj, _ := h.n.st.Project("demo")
	wt := filepath.Join(t.TempDir(), "wt")
	if out, err := exec.Command("git", "-C", proj.Path, "worktree", "add", "-q", "-b", "rust", wt).CombinedOutput(); err != nil {
		t.Fatalf("worktree: %v %s", err, out)
	}
	claudeAppSession(t, home, config, "local_rust", "cli-rust", wt)
	owner := &Principal{Kind: PrincipalOwner}
	ctx := context.Background()

	s, err := h.n.ContinueClaudeSession(ctx, owner, &machinev1.ContinueClaudeSessionRequest{SessionId: "local_rust", Prompt: "say go on", IdempotencyKey: "k1"})
	if err != nil {
		t.Fatal(err)
	}
	if s.ProjectId != "demo" || s.Agent != "claude" || s.Title != "Rust 迁移难点" {
		t.Fatalf("session: %+v", s)
	}
	for deadline := time.Now().Add(15 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		got, err := h.s.Get(owner, s.SessionId)
		if err == nil && got.Status == sessionv1.SessionStatus_SESSION_STATUS_IDLE {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not idle: %v %v", got, err)
		}
	}
	spec := claude.spec(0)
	real, _ := filepath.EvalSymlinks(wt)
	if spec.ResumeRef != "cli-rust" || spec.NewRef != "" || (spec.WorkDir != wt && spec.WorkDir != real) {
		t.Fatalf("started with: resume %q new %q in %q", spec.ResumeRef, spec.NewRef, spec.WorkDir)
	}
	// Still listed — its earlier conversation is only there — saying the session it goes on in; going on again is that one.
	list, err := h.n.ClaudeSessions()
	if err != nil {
		t.Fatal(err)
	}
	listed := ""
	for _, cs := range list.Sessions {
		if cs.SessionId == "local_rust" {
			listed = cs.ContinuedIn
		}
	}
	if listed != s.SessionId {
		t.Fatalf("listed as going on in %q, want %q", listed, s.SessionId)
	}
	again, err := h.n.ContinueClaudeSession(ctx, owner, &machinev1.ContinueClaudeSessionRequest{SessionId: "local_rust", Prompt: "say twice"})
	if err != nil || again.SessionId != s.SessionId || claude.count() != 1 {
		t.Fatalf("again: %v %v, starts %d", again, err, claude.count())
	}

	// A folder no project holds: said so, with the folder.
	lone := t.TempDir()
	claudeAppSession(t, home, config, "local_lone", "cli-lone", lone)
	_, err = h.n.ContinueClaudeSession(ctx, owner, &machinev1.ContinueClaudeSessionRequest{SessionId: "local_lone", Prompt: "hi"})
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), lone) || !strings.Contains(err.Error(), "add that folder as a project") {
		t.Fatalf("no project: %v", err)
	}
	// A folder that is gone.
	gone := filepath.Join(t.TempDir(), "gone")
	claudeAppSession(t, home, config, "local_gone", "cli-gone", gone)
	if _, err = h.n.ContinueClaudeSession(ctx, owner, &machinev1.ContinueClaudeSessionRequest{SessionId: "local_gone", Prompt: "hi"}); !strings.Contains(fmtErr(err), "no longer here") {
		t.Fatalf("gone: %v", err)
	}
	// Owner only.
	if _, err := (&adminService{n: h.n}).ContinueClaudeSession(context.WithValue(ctx, principalKey{}, h.p), &machinev1.ContinueClaudeSessionRequest{SessionId: "local_lone", Prompt: "hi"}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("a client: %v", err)
	}
}

func fmtErr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
