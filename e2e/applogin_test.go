package e2e

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
)

// Work order #39: on a computer whose Aide app is logged in, the CLI logs
// in through the app — from a terminal with nothing to type, and from the
// app's own button — with no account or password; it is an agent of its
// own, listed in the app under the name the CLI shows. Logging it out (the
// button, or aide auth logout) revokes it; the app's own login is not
// touched. No password lands in the CLI's files.
func TestCLILogsInThroughTheApp(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	appCLI := t.TempDir() // the CLI home the app's button logs in
	w := newOwnerMac(t, []string{"AIDE_FAKE_PROVIDERS=1", "AIDE_HOME=" + appCLI}, &localv1.GrantSpec{})
	ctx := context.Background()
	aide := filepath.Join(w.bin, "aide")
	clients := func() map[string]*localv1.Client {
		out := map[string]*localv1.Client{}
		res, err := w.app.client.ListClients(ctx, &localv1.ListClientsRequest{})
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range res.Clients {
			out[c.ClientId] = c
		}
		return out
	}
	type status struct {
		Auth struct {
			Identity *struct {
				Client   string `json:"client"`
				ClientID string `json:"client_id"`
			}
		}
	}
	readStatus := func(env []string) status {
		var s status
		r := run(t, env, time.Minute, aide, "auth", "status", "-o", "json")
		json.Unmarshal([]byte(r.stdout), &s)
		return s
	}

	// From a terminal: `aide auth login`, nothing else, no account, no password.
	term := []string{"AIDE_HOME=" + t.TempDir(), "AIDE_APP_SOCKET=" + w.sock}
	r := run(t, term, time.Minute, aide, "auth", "login", "--name", "desk CLI")
	if r.code != 0 || !strings.Contains(r.stdout, "through the Aide app on this computer") {
		t.Fatalf("login from a terminal: %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	s := readStatus(term)
	if s.Auth.Identity == nil || s.Auth.Identity.Client != "desk CLI" {
		t.Fatalf("auth status after: %+v", s)
	}
	if c := clients()[s.Auth.Identity.ClientID]; c == nil || c.Label != "desk CLI" || c.Revoked {
		t.Fatalf("the app's list: %+v", c)
	}
	if p := run(t, term, time.Minute, aide, "project", "list"); p.code != 0 || !strings.Contains(p.stdout, "demo") {
		t.Fatalf("project list: %d\n%s%s", p.code, p.stdout, p.stderr)
	}
	// Logging out revokes it in the app too.
	if r := run(t, term, time.Minute, aide, "auth", "logout"); r.code != 0 || !strings.Contains(r.stdout, "the Aide app revoked") {
		t.Fatalf("logout: %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	eventually(t, 30*time.Second, "revoked in the app", func() bool { c := clients()[s.Auth.Identity.ClientID]; return c == nil || c.Revoked })

	// From the app's button: the CLI beside aided, in the user's CLI home.
	cli, err := w.app.client.GetLocalCLI(ctx, &localv1.GetLocalCLIRequest{})
	if err != nil || !cli.Found || cli.LoggedIn {
		t.Fatalf("before: %+v %v", cli, err)
	}
	cli, err = w.app.client.LogInLocalCLI(ctx, &localv1.LogInLocalCLIRequest{})
	if err != nil || !cli.LoggedIn || !cli.SameOwner || cli.GrantId == "" || !strings.HasPrefix(cli.Label, "client@") {
		t.Fatalf("logged in from the app: %+v %v\n%s", cli, err, w.owner.output())
	}
	byCLI := readStatus([]string{"AIDE_HOME=" + appCLI})
	if byCLI.Auth.Identity == nil || byCLI.Auth.Identity.Client != cli.Label {
		t.Fatalf("the CLI says %+v, the app %q", byCLI.Auth.Identity, cli.Label)
	}
	// Again: already logged in, nothing new.
	if again, err := w.app.client.LogInLocalCLI(ctx, &localv1.LogInLocalCLIRequest{}); err != nil || again.ClientId != cli.ClientId {
		t.Fatalf("again: %+v %v", again, err)
	}
	for _, f := range []string{"state.json", "client.key"} {
		b, _ := os.ReadFile(filepath.Join(appCLI, f))
		if len(b) == 0 || strings.Contains(string(b), testPassword) {
			t.Fatalf("%s: %d bytes, password in it: %v", f, len(b), strings.Contains(string(b), testPassword))
		}
	}
	out, err := w.app.client.LogOutLocalCLI(ctx, &localv1.LogOutLocalCLIRequest{})
	if err != nil || out.LoggedIn {
		t.Fatalf("logged out from the app: %+v %v", out, err)
	}
	eventually(t, 30*time.Second, "revoked in the app", func() bool { c := clients()[cli.ClientId]; return c == nil || c.Revoked })
	// The app itself is still logged in.
	if st, err := w.app.client.GetStatus(ctx, &localv1.GetStatusRequest{}); err != nil || st.SignedOut || st.OwnerId == "" {
		t.Fatalf("the app after: %+v %v", st, err)
	}
	if strings.Contains(w.owner.output(), testPassword) {
		t.Fatal("the password is in aided's log")
	}
}

// TestCLIInChinese (work order #38): on a Chinese system the CLI's login
// and auth status read in Chinese end to end; with the language set to
// English they read as before; -o json is the same either way, field by
// field.
func TestCLIInChinese(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	w := newOwnerMac(t, []string{"AIDE_FAKE_PROVIDERS=1"}, &localv1.GrantSpec{})
	aide := filepath.Join(w.bin, "aide")
	home := []string{"AIDE_HOME=" + t.TempDir(), "AIDE_APP_SOCKET=" + w.sock}
	zh := append(append([]string{}, home...), "AIDE_LANG=", "LC_ALL=", "LC_MESSAGES=", "LANG=zh_CN.UTF-8")
	en := append(append([]string{}, home...), "AIDE_LANG=en")

	r := run(t, zh, time.Minute, aide, "auth", "login")
	if r.code != 0 || !strings.Contains(r.stdout, "已经由本机 Aide 应用登录到") || !strings.Contains(r.stdout, "无需密码") {
		t.Fatalf("login in Chinese: %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	s := run(t, zh, time.Minute, aide, "auth", "status")
	for _, want := range []string{"配置目录", "✓ 身份", "客户端", "属主", "✓ 授权", "全部机器、全部项目", "✓ 中继", "✓ 授权同步", "已是最新"} {
		if !strings.Contains(s.stdout, want) {
			t.Errorf("auth status lacks %q:\n%s", want, s.stdout)
		}
	}
	for _, english := range []string{"identity", "grant ", "relay ", "up to date", "all machines", "doesn't expire", "config "} {
		if strings.Contains(s.stdout, english) {
			t.Errorf("auth status still says %q:\n%s", english, s.stdout)
		}
	}
	if e := run(t, en, time.Minute, aide, "auth", "status"); !strings.Contains(e.stdout, "✓ identity") || !strings.Contains(e.stdout, "up to date") {
		t.Fatalf("auth status in English:\n%s", e.stdout)
	}
	// The JSON is the same document in both, but for the round trip time.
	ms := regexp.MustCompile(`· \d+ ms`)
	jz := ms.ReplaceAllString(run(t, zh, time.Minute, aide, "auth", "status", "-o", "json").stdout, "")
	je := ms.ReplaceAllString(run(t, en, time.Minute, aide, "auth", "status", "-o", "json").stdout, "")
	if jz != je || !strings.Contains(je, `"check": "identity"`) {
		t.Fatalf("json differs:\n%s\n%s", jz, je)
	}
	// Tables: the header in Chinese, the columns where they were.
	if p := run(t, zh, time.Minute, aide, "project", "list"); !strings.Contains(p.stdout, "项目") || !strings.Contains(p.stdout, "工作区") || strings.Contains(p.stdout, "WORKSPACE") {
		t.Fatalf("project list in Chinese:\n%s%s", p.stdout, p.stderr)
	}
	if r := run(t, en, 2*time.Minute, aide, "session", "start", "--project", "demo", "--title", "running", "--prompt", "say hi", "--wait"); r.code != 0 {
		t.Fatalf("session start: %s%s", r.stdout, r.stderr)
	}
	sl := run(t, zh, time.Minute, aide, "session", "list")
	if !strings.Contains(sl.stdout, "会话") || !strings.Contains(sl.stdout, "状态") || !strings.Contains(sl.stdout, "已完成") || !strings.Contains(sl.stdout, " running ") {
		t.Fatalf("session list in Chinese (its title, running, stays):\n%s", sl.stdout)
	}
	if h := run(t, zh, time.Minute, aide, "auth", "--help"); !strings.Contains(h.stdout, "可用命令") || !strings.Contains(h.stdout, "检查本客户端的登录") {
		t.Fatalf("help in Chinese:\n%s", h.stdout)
	}
}
