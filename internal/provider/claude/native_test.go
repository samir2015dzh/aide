package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
)

var nativeT0 = time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)

// writeRecord writes the app's record of a session in dir.
func writeRecord(t *testing.T, dir string, rec map[string]any) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(rec)
	if err := os.WriteFile(filepath.Join(dir, rec["sessionId"].(string)+".json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// writeTranscript writes Claude Code's transcript of id in config, in a
// project folder named as Claude Code names it (not as Aide reads it).
func writeTranscript(t *testing.T, config, project, id string, lines ...map[string]any) {
	t.Helper()
	dir := filepath.Join(config, "projects", project)
	os.MkdirAll(dir, 0o755)
	var b []byte
	for i, l := range lines {
		l["sessionId"], l["timestamp"] = id, nativeT0.Add(time.Duration(i)*time.Second).Format(time.RFC3339Nano)
		j, _ := json.Marshal(l)
		b = append(append(b, j...), '\n')
	}
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// gitCheckout makes dir a git checkout with remote origin url ("" none).
func gitCheckout(t *testing.T, dir, url string) {
	t.Helper()
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	cfg := "[core]\n\trepositoryformatversion = 0\n"
	if url != "" {
		cfg += "[remote \"upstream\"]\n\turl = https://github.com/someone/else.git\n[remote \"origin\"]\n\turl = " + url + "\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"
	}
	os.WriteFile(filepath.Join(dir, ".git", "config"), []byte(cfg), 0o644)
}

func conversationLines() []map[string]any {
	return []map[string]any{
		{"type": "user", "message": map[string]any{"role": "user", "content": "看一下测试为什么挂"}},
		{"type": "user", "isMeta": true, "message": map[string]any{"role": "user", "content": "<local-command-caveat>x</local-command-caveat>"}},
		{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "thinking", "thinking": "hm"}}}},
		{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": "t1", "name": "Bash", "input": map[string]any{"command": "go test ./..."}}}}},
		{"type": "user", "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": "FAIL x", "is_error": true}}}},
		{"type": "assistant", "isSidechain": true, "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "a subagent's"}}}},
		{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "缺了一个夹具。"}}}},
		{"type": "api-request", "message": map[string]any{}},
		{"type": "user", "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "<system-reminder>r</system-reminder>"}, map[string]any{"type": "text", "text": "补上"}}}},
	}
}

func describeEvents(evs []*sessionv1.SessionEvent) string {
	var out []string
	for _, e := range evs {
		switch k := e.Kind.(type) {
		case *sessionv1.SessionEvent_UserMessage:
			out = append(out, "user: "+k.UserMessage.Text)
		case *sessionv1.SessionEvent_AgentMessage:
			out = append(out, "claude: "+k.AgentMessage.Text)
		case *sessionv1.SessionEvent_ToolCall:
			out = append(out, "tool: "+k.ToolCall.Name+" "+k.ToolCall.Summary)
		case *sessionv1.SessionEvent_ToolResult:
			out = append(out, fmt.Sprint("result: ", k.ToolResult.Summary, " ", k.ToolResult.IsError))
		}
	}
	return strings.Join(out, "; ")
}

func listedClaude(p Places, skip func(string, string) bool) string {
	var out []string
	for _, s := range List(p, skip) {
		out = append(out, strings.Join([]string{s.SessionId, s.Title, s.Repo, fmt.Sprint(s.Archived), s.Status, s.Error}, " | "))
	}
	return strings.Join(out, "\n")
}

// Layout 1, unlike the owner's computer: the classic installer's app on
// Windows (%APPDATA%\Claude, two accounts), Claude Code's config moved
// with CLAUDE_CONFIG_DIR, the folder a git worktree whose repository's
// origin is on GitHub over SSH; a session archived in the app's index
// file, one deleted, one listed under both accounts, one with no
// transcript on this machine, and one Aide started (skipped).
func TestClaudeSessionsInstallerLayout(t *testing.T) {
	home, appData, local := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	config := filepath.Join(t.TempDir(), "claude config")
	idx := filepath.Join(appData, "Claude", "claude-code-sessions")
	a1, a2 := filepath.Join(idx, "acct-1", "org-1"), filepath.Join(idx, "acct-2", "org-9")

	repo := filepath.Join(t.TempDir(), "src", "app")
	gitCheckout(t, repo, "git@github.com:octo/app.git")
	wt := filepath.Join(t.TempDir(), "wt", "feature")
	os.MkdirAll(filepath.Join(repo, ".git", "worktrees", "feature"), 0o755)
	os.WriteFile(filepath.Join(repo, ".git", "worktrees", "feature", "commondir"), []byte("../..\n"), 0o644)
	os.MkdirAll(wt, 0o755)
	os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+filepath.Join(repo, ".git", "worktrees", "feature")+"\n"), 0o644)
	sub := filepath.Join(wt, "pkg", "x") // a folder inside it
	os.MkdirAll(sub, 0o755)

	ms := func(d time.Duration) int64 { return nativeT0.Add(d).UnixMilli() }
	writeRecord(t, a1, map[string]any{"sessionId": "local_aaa", "cliSessionId": "cli-aaa", "title": "修测试", "cwd": sub, "createdAt": ms(0), "lastActivityAt": ms(time.Hour),
		"model": "claude-opus-5-5", "postTurnSummary": map[string]any{"status_category": "blocked"}})
	writeRecord(t, a1, map[string]any{"sessionId": "local_old", "cliSessionId": "cli-old", "title": "旧的", "cwd": repo, "createdAt": ms(-time.Hour), "lastActivityAt": ms(-time.Minute)})
	os.WriteFile(filepath.Join(a1, "archived-sessions.idx"), []byte(`{"v":1,"archived":["local_old"]}`), 0o644)
	writeRecord(t, a1, map[string]any{"sessionId": "local_gone", "cliSessionId": "cli-gone", "title": "删了", "cwd": repo, "createdAt": ms(0), "lastActivityAt": ms(0)})
	os.WriteFile(filepath.Join(a1, "deleted_gone"), []byte("1763110800000"), 0o644)
	writeRecord(t, a1, map[string]any{"sessionId": "local_aide", "cliSessionId": "cli-aide", "title": "demo · Aide · x", "cwd": repo, "createdAt": ms(0), "lastActivityAt": ms(2 * time.Hour)})
	// The same session under the other account, older: listed once, as the newer says.
	writeRecord(t, a2, map[string]any{"sessionId": "local_aaa", "cliSessionId": "cli-aaa", "title": "old title", "cwd": sub, "createdAt": ms(0), "lastActivityAt": ms(time.Minute)})
	writeRecord(t, a2, map[string]any{"sessionId": "local_far", "cliSessionId": "cli-far", "title": "别的电脑上的", "cwd": "/nowhere", "createdAt": ms(-3 * time.Hour), "lastActivityAt": ms(-2 * time.Hour)})

	writeTranscript(t, config, "-src-app-wt-feature", "cli-aaa", conversationLines()...)
	writeTranscript(t, config, "-src-app", "cli-old", map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": "hi"}})

	p := PlacesFor([]string{"APPDATA=" + appData, "LOCALAPPDATA=" + local, "CLAUDE_CONFIG_DIR=" + config})
	if len(p.Indexes) != 1 || p.Indexes[0] != idx || p.Config != config {
		t.Fatalf("places: %+v", p)
	}
	skip := func(cliID, _ string) bool { return cliID == "cli-aide" }
	want := "local_aaa | 修测试 | octo/app | false | blocked | \n" +
		"local_old | 旧的 | octo/app | true |  | \n" +
		"local_far | 别的电脑上的 |  | false |  | no Claude Code transcript of it on this machine"
	if got := listedClaude(p, skip); got != want {
		t.Fatalf("listed:\n%s\nwant:\n%s", got, want)
	}
	s, evs, err := Read(p, "local_aaa", skip)
	if err != nil || s.Title != "修测试" {
		t.Fatalf("read: %v %v", s, err)
	}
	if got, want := describeEvents(evs), `user: 看一下测试为什么挂; tool: Bash {"command":"go test ./..."}; result: FAIL x true; claude: 缺了一个夹具。; user: 补上`; got != want {
		t.Fatalf("conversation:\n%s\nwant:\n%s", got, want)
	}
	for i := 1; i < len(evs); i++ {
		if evs[i].Seq <= evs[i-1].Seq {
			t.Fatalf("seq not increasing: %d then %d", evs[i-1].Seq, evs[i].Seq)
		}
	}
	if _, _, err := Read(p, "local_aide", skip); err != ErrNoSession {
		t.Fatalf("Aide's own: %v", err)
	}
	if _, _, err := Read(p, "local_gone", skip); err != ErrNoSession {
		t.Fatalf("deleted: %v", err)
	}
}

// Layout 2, unlike the owner's computer: an MSIX app whose data is
// redirected into its package (Packages\Claude_<id>\LocalCache\Roaming\
// Claude), Claude Code's config in the default ~/.claude, a folder that is
// no git checkout, and one whose checkout has no origin (the owner's own
// case: identity left empty, never guessed).
func TestClaudeSessionsMSIXRedirectedLayout(t *testing.T) {
	home, appData, local := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	idx := filepath.Join(local, "Packages", "Claude_zz9", "LocalCache", "Roaming", "Claude", "claude-code-sessions")
	dir := filepath.Join(idx, "u", "w")
	plain, noOrigin := filepath.Join(t.TempDir(), "笔记"), filepath.Join(t.TempDir(), "proj")
	os.MkdirAll(plain, 0o755)
	gitCheckout(t, noOrigin, "")
	writeRecord(t, dir, map[string]any{"sessionId": "local_1", "cliSessionId": "c1", "title": "整理笔记", "cwd": plain, "createdAt": nativeT0.UnixMilli(), "lastActivityAt": nativeT0.UnixMilli(), "isArchived": true})
	writeRecord(t, dir, map[string]any{"sessionId": "local_2", "cliSessionId": "c2", "title": "无 origin", "originCwd": noOrigin, "createdAt": nativeT0.UnixMilli()})
	writeTranscript(t, filepath.Join(home, ".claude"), "D------", "c1", conversationLines()[:1]...)
	writeTranscript(t, filepath.Join(home, ".claude"), "x", "c2", conversationLines()[:1]...)

	p := PlacesFor([]string{"APPDATA=" + appData, "LOCALAPPDATA=" + local})
	if len(p.Indexes) != 1 || p.Indexes[0] != idx || p.Config != filepath.Join(home, ".claude") {
		t.Fatalf("places: %+v", p)
	}
	want := "local_1 | 整理笔记 |  | true |  | \nlocal_2 | 无 origin |  | false |  | "
	if got := listedClaude(p, nil); got != want {
		t.Fatalf("listed:\n%s\nwant:\n%s", got, want)
	}
	if s, evs, err := Read(p, "c2", nil); err != nil || s.Cwd != noOrigin || describeEvents(evs) != "user: 看一下测试为什么挂" {
		t.Fatalf("read by Claude Code's id: %v %v %v", s, describeEvents(evs), err)
	}
	// No app at all: nothing, and no index read.
	if p := PlacesFor([]string{"APPDATA=" + t.TempDir(), "LOCALAPPDATA=" + t.TempDir()}); len(p.Indexes) != 0 || len(List(p, nil)) != 0 {
		t.Fatalf("no app: %+v", p)
	}
}

func TestGithubRepo(t *testing.T) {
	for url, want := range map[string]string{
		"https://github.com/octo/app.git":            "octo/app",
		"https://github.com/octo/app":                "octo/app",
		"https://x-access-token:abc@github.com/o/r/": "o/r",
		"git@github.com:octo/my.app.git":             "octo/my.app",
		"ssh://git@github.com/octo/app.git":          "octo/app",
		"ssh://git@github.com:443/octo/app.git":      "octo/app",
		"git://github.com/octo/app":                  "octo/app",
		"https://gitlab.com/octo/app.git":            "",
		"https://github.com.evil.example/octo/app":   "",
		"https://github.com/octo/app/tree/main":      "",
		"":                                           "",
		"/srv/git/app.git":                           "",
	} {
		if got := githubRepo(url); got != want {
			t.Errorf("%q: %q, want %q", url, got, want)
		}
	}
}
