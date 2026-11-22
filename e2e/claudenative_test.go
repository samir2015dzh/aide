package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
)

// claudeAppSession writes a session as the Claude desktop app lists it
// (its record in index/acct/ws) and Claude Code's transcript of it (in
// config/projects/<folder>), one prompt and one reply.
func claudeAppSession(t *testing.T, index, config, id, title, cwd string, at time.Time, prompt, reply string) {
	t.Helper()
	dir := filepath.Join(index, "acct", "ws")
	os.MkdirAll(dir, 0o755)
	rec, _ := json.Marshal(map[string]any{"sessionId": "local_" + id, "cliSessionId": id, "title": title, "cwd": cwd,
		"createdAt": at.Add(-time.Minute).UnixMilli(), "lastActivityAt": at.UnixMilli(), "model": "claude-opus-5-5",
		"postTurnSummary": map[string]any{"status_category": "completed"}})
	if err := os.WriteFile(filepath.Join(dir, "local_"+id+".json"), rec, 0o600); err != nil {
		t.Fatal(err)
	}
	proj := filepath.Join(config, "projects", "-"+strings.Trim(strings.NewReplacer("/", "-", `\`, "-", ":", "-").Replace(cwd), "-"))
	os.MkdirAll(proj, 0o755)
	var b []byte
	for _, l := range []map[string]any{
		{"type": "user", "entrypoint": "claude-desktop", "message": map[string]any{"role": "user", "content": prompt}},
		{"type": "assistant", "entrypoint": "claude-desktop", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": reply}}}},
	} {
		l["sessionId"], l["timestamp"], l["cwd"] = id, at.Format(time.RFC3339Nano), cwd
		j, _ := json.Marshal(l)
		b = append(append(b, j...), '\n')
	}
	if err := os.WriteFile(filepath.Join(proj, id+".jsonl"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestClaudeNativeSessions: the owner sees the Claude Code sessions the
// Claude desktop app has, on each machine, by title, with the GitHub
// repository its folder's origin names (none when it has none), and opens
// one: its conversation read from Claude Code's transcript, from the
// remote machine end to end and from this computer's own in process. The
// two machines keep them as the app does in two different ways. A machine
// gone keeps its sessions listed, lost.
func TestClaudeNativeSessions(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	now := time.Now().Truncate(time.Millisecond)
	// This computer: an MSIX app's Claude-Data\claude_<id>, Claude Code's
	// config in ~/.claude, the folder a checkout of a GitHub repository.
	home, local := t.TempDir(), t.TempDir()
	repo := filepath.Join(t.TempDir(), "site")
	os.MkdirAll(filepath.Join(repo, ".git"), 0o755)
	os.WriteFile(filepath.Join(repo, ".git", "config"), []byte("[remote \"origin\"]\n\turl = https://github.com/octo/site.git\n"), 0o644)
	claudeAppSession(t, filepath.Join(local, "Claude-Data", "claude_pubid", "claude-code-sessions"), filepath.Join(home, ".claude"),
		"c-site", "改首页", repo, now.Add(-time.Hour), "首页标题改一下", "改好了。")
	// Windows finds the home folder by USERPROFILE, the others by HOME.
	w := newOwnerMac(t, []string{"HOME=" + home, "USERPROFILE=" + home, "LOCALAPPDATA=" + local}, &localv1.GrantSpec{})
	ctx := context.Background()

	// Another computer, a machine only: the installer's %APPDATA%\Claude,
	// Claude Code's config moved, a folder with no git at all.
	st, err := w.app.client.GetStatus(ctx, &localv1.GetStatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	appData, config, notes := t.TempDir(), filepath.Join(t.TempDir(), "cc"), filepath.Join(t.TempDir(), "笔记")
	os.MkdirAll(notes, 0o755)
	claudeAppSession(t, filepath.Join(appData, "Claude", "claude-code-sessions"), config, "c-notes", "整理笔记", notes, now, "列一下待办", "三件。")
	work, _ := w.otherMac("work-pc", []string{"APPDATA=" + appData, "CLAUDE_CONFIG_DIR=" + config})
	workDaemon := w.logs[len(w.logs)-1]
	w.logInMachineOnly(work, st, "work-pc")

	var list *localv1.ListClaudeSessionsResponse
	eventually(t, time.Minute, "both machines' sessions", func() bool {
		list, err = w.app.client.ListClaudeSessions(ctx, &localv1.ListClaudeSessionsRequest{})
		return err == nil && len(list.Unreachable) == 0 && len(list.Sessions) == 2
	})
	var got []string
	for _, e := range list.Sessions {
		got = append(got, fmt.Sprint(e.MachineName, " ", e.Session.SessionId, " ", e.Session.Title, " ", e.Session.Repo, " ", e.Session.Status, " ", e.Lost))
	}
	if want := "work-pc local_c-notes 整理笔记  completed false\nowner-mac local_c-site 改首页 octo/site completed false"; strings.Join(got, "\n") != want {
		t.Fatalf("listed:\n%s\nwant:\n%s", strings.Join(got, "\n"), want)
	}
	if len(list.Indexes) != 2 {
		t.Fatalf("indexes: %v", list.Indexes)
	}

	read := func(machine, id string) string {
		t.Helper()
		res, err := w.app.client.ReadClaudeSession(ctx, &localv1.ReadClaudeSessionRequest{MachineId: machine, SessionId: id, View: sessionv1.EventView_EVENT_VIEW_FULL})
		if err != nil {
			t.Fatalf("read %s on %s: %v", id, machine, err)
		}
		var out []string
		for _, e := range res.Events {
			switch k := e.Kind.(type) {
			case *sessionv1.SessionEvent_UserMessage:
				out = append(out, "user: "+k.UserMessage.Text)
			case *sessionv1.SessionEvent_AgentMessage:
				out = append(out, "claude: "+k.AgentMessage.Text)
			}
		}
		if res.Session.Error != "" {
			t.Fatalf("read %s on %s: %s", id, machine, res.Session.Error)
		}
		return res.Session.Title + ": " + strings.Join(out, "; ")
	}
	if got := read("work-pc", "local_c-notes"); got != "整理笔记: user: 列一下待办; claude: 三件。" {
		t.Fatalf("work-pc's: %s", got)
	}
	if got := read("owner-mac", "local_c-site"); got != "改首页: user: 首页标题改一下; claude: 改好了。" {
		t.Fatalf("this computer's: %s", got)
	}

	// work-pc goes: its session stays, lost; this computer's as ever.
	workDaemon.stop()
	eventually(t, time.Minute, "work-pc's session lost", func() bool {
		list, err = w.app.client.ListClaudeSessions(ctx, &localv1.ListClaudeSessionsRequest{})
		return err == nil && len(list.Unreachable) == 1 && len(list.Sessions) == 2
	})
	for _, e := range list.Sessions {
		if lost := e.MachineName == "work-pc"; e.Lost != lost {
			t.Errorf("%s %s: lost %v", e.MachineName, e.Session.SessionId, e.Lost)
		}
	}
}

// TestClaudeSessionAddress: a Claude app session takes messages at the
// address of its conversation under this computer's CLI agent once that
// agent's hooks reported the conversation (its SessionStart); before, and
// for the others, it has none (docs/agent-messages.md §15.4).
func TestClaudeSessionAddress(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	now := time.Now().Truncate(time.Millisecond)
	home, local, cliHome := t.TempDir(), t.TempDir(), t.TempDir()
	index, config := filepath.Join(local, "Claude-Data", "claude_pubid", "claude-code-sessions"), filepath.Join(home, ".claude")
	work := t.TempDir()
	claudeAppSession(t, index, config, "c-site", "改首页", work, now.Add(-time.Hour), "首页标题改一下", "改好了。")
	claudeAppSession(t, index, config, "c-notes", "整理笔记", work, now, "列一下待办", "三件。")
	// This computer's aided finds the CLI agent's home where the agent keeps it.
	w := newOwnerMac(t, []string{"HOME=" + home, "USERPROFILE=" + home, "LOCALAPPDATA=" + local, "AIDE_HOME=" + cliHome}, &localv1.GrantSpec{})
	env := []string{"AIDE_HOME=" + cliHome}
	w.loginAgent(env, "desk", &localv1.GrantSpec{})
	ctx := context.Background()
	addresses := func() map[string]string {
		got := map[string]string{}
		if list, err := w.app.client.ListClaudeSessions(ctx, &localv1.ListClaudeSessionsRequest{}); err == nil {
			for _, e := range list.Sessions {
				got[e.Session.SessionId] = e.Session.Address
			}
		}
		return got
	}
	eventually(t, time.Minute, "the sessions listed", func() bool { return len(addresses()) == 2 })
	if a := addresses(); a["local_c-site"] != "" || a["local_c-notes"] != "" {
		t.Fatalf("addresses before any hook ran: %v", a)
	}

	hookRun(t, w, env, "claude", map[string]any{"session_id": "c-site", "hook_event_name": "SessionStart", "source": "startup"})
	var me string
	var c struct {
		Contacts []struct {
			Address string
			You     bool
		}
	}
	json.Unmarshal([]byte(run(t, append(slices.Clone(w.cliEnv), env...), time.Minute, filepath.Join(w.bin, "aide"), "message", "contacts", "-o", "json").stdout), &c)
	for _, x := range c.Contacts {
		if x.You {
			me = x.Address
		}
	}
	if !strings.HasPrefix(me, "c_") {
		t.Fatalf("the agent's address: %q", me)
	}
	if a := addresses(); a["local_c-site"] != me+"/c-site" || a["local_c-notes"] != "" {
		t.Fatalf("addresses once c-site's hook ran: %v, want %s/c-site and none", a, me)
	}
	list, err := w.app.client.ListClaudeSessions(ctx, &localv1.ListClaudeSessionsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.app.client.ReadClaudeSession(ctx, &localv1.ReadClaudeSessionRequest{MachineId: list.Sessions[0].MachineId, SessionId: "local_c-site"})
	if err != nil || res.Session.GetAddress() != me+"/c-site" {
		t.Fatalf("read: %v %v", res.GetSession(), err)
	}
}
