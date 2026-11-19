package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	localv1 "github.com/hermeshai/hermesh/gen/go/hermesh/local/v1"
)

// A Claude session an agent starts through Hermesh is listed in the Claude
// desktop app's own index of Claude Code sessions (WO #8), in the account
// the app uses, pointing at the session's transcript and directory, and
// named for its project and who started it. The app's own records are
// left as they are.
func TestClaudeSessionListedInDesktopApp(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in Claude Code is a shell script")
	}
	home := t.TempDir()
	index := filepath.Join(home, "Library", "Application Support", "Claude", "claude-code-sessions")
	acct := filepath.Join(index, "acct-1", "org-1")
	if err := os.MkdirAll(acct, 0o700); err != nil {
		t.Fatal(err)
	}
	own := filepath.Join(acct, "local_aaaaaaaa-0000-4000-8000-000000000000.json")
	ownRecord := `{"sessionId":"local_aaaaaaaa-0000-4000-8000-000000000000","cliSessionId":"11111111-0000-4000-8000-000000000000","title":"mine"}`
	if err := os.WriteFile(own, []byte(ownRecord), 0o600); err != nil {
		t.Fatal(err)
	}
	// Claude Code as it starts a new session: it names the session it was
	// given and answers each message.
	claude := filepath.Join(t.TempDir(), "claude")
	script := `#!/bin/sh
case "$1" in
--version) echo "2.1.312 (Claude Code)"; exit 0;;
auth) echo '{"loggedIn": true}'; exit 0;;
esac
id=
while [ $# -gt 0 ]; do case "$1" in --session-id|--resume) id=$2;; esac; shift; done
read init
echo '{"type":"system","subtype":"init","session_id":"'$id'","model":"claude-haiku-5-5"}'
while read prompt; do
	echo '{"type":"user","message":{"role":"user","content":"x"},"isReplay":true}'
	echo '{"type":"assistant","message":{"content":[{"type":"text","text":"done"}]}}'
	echo '{"type":"result","subtype":"success","result":"done"}'
done
`
	if err := os.WriteFile(claude, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	w := newOwnerMac(t, []string{"HERMESH_CLAUDE_BIN=" + claude, "HOME=" + home}, &localv1.GrantSpec{})

	s, r := w.startSession(2*time.Minute, "--agent", "claude", "--wait", "--prompt", "hello")
	if r.code != 0 || s.Status != "idle" || s.Ref == "" {
		t.Fatalf("session: %+v\n%s", s, r.stdout+r.stderr)
	}
	b, err := os.ReadFile(filepath.Join(acct, "local_"+s.Ref+".json"))
	if err != nil {
		t.Fatalf("not listed: %v\n%s", err, w.owner.output())
	}
	var rec map[string]any
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	cwd, _ := filepath.EvalSymlinks(w.worktree(s.SessionID))
	for k, want := range map[string]any{"sessionId": "local_" + s.Ref, "cliSessionId": s.Ref, "cwd": cwd, "originCwd": cwd,
		"title": "demo · bot via Aide · hello", "titleSource": "user", "isArchived": false, "permissionMode": "default"} {
		if rec[k] != want {
			t.Errorf("%s: got %v, want %v\n%s", k, rec[k], want, b)
		}
	}
	if c, _ := rec["createdAt"].(float64); time.Since(time.UnixMilli(int64(c))) > 5*time.Minute {
		t.Errorf("createdAt %v", rec["createdAt"])
	}
	if got, _ := os.ReadFile(own); string(got) != ownRecord {
		t.Fatalf("the app's own record changed: %s", got)
	}
	if ents, _ := os.ReadDir(acct); len(ents) != 2 {
		t.Fatalf("files in the account folder: %v", ents)
	}
	if !strings.Contains(w.owner.output(), `msg="session listed in the Claude desktop app`) {
		t.Fatalf("not in the machine's log:\n%s", w.owner.output())
	}

	// The session goes on in the same Claude session: listed once.
	if s2, r := w.send(time.Minute, s.SessionID, "again"); r.code != 0 || s2.Ref != s.Ref {
		t.Fatalf("send: %+v\n%s", s2, r.stdout+r.stderr)
	}
	if ents, _ := os.ReadDir(acct); len(ents) != 2 {
		t.Fatalf("files in the account folder: %v", ents)
	}
}
