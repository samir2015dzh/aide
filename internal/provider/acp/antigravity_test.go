package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/samir2015dzh/aide/internal/provider"
)

func TestAntigravityDiscovery(t *testing.T) {
	for _, goos := range []string{"darwin", "linux", "windows"} {
		a := antigravityAgent(goos)
		want := "agy_acp_server.par"
		if goos == "windows" {
			want = "agy_acp_server.exe"
		}
		if a.Command != want || a.Paths[0] != ".local/share/antigravity-acp/"+want || (goos == "linux") != slices.Equal(a.Args, []string{"--uid="}) || len(a.Auth) != 0 {
			t.Fatalf("%s: %+v", goos, a)
		}
	}
	dir := t.TempDir()
	t.Setenv("GEMINI_HOME", dir)
	p := &Provider{Agent: antigravityAgent("darwin"), Binary: fakeBin}
	if i := p.Discover(context.Background()); !i.Available || i.LoggedIn || i.LoginUnconfirmed {
		t.Fatalf("installed without settings.json must be confirmed signed out: %+v", i)
	}
	os.Mkdir(filepath.Join(dir, "antigravity-acp"), 0o700)
	file := filepath.Join(dir, "antigravity-acp", "settings.json")
	for _, c := range []struct {
		config      string
		ready       bool
		unconfirmed bool
	}{
		{`{}`, false, false},
		{`invalid`, false, true},
		{`{"auth":{"type":""}}`, false, false},
		{`{"auth":{"type":"oauth-personal"}}`, true, false},
		{`{"auth":{"type":"oauth-business"}}`, true, false},
	} {
		os.WriteFile(file, []byte(c.config), 0o600)
		if i := p.Discover(context.Background()); !i.Available || i.LoggedIn != c.ready || i.LoginUnconfirmed != c.unconfirmed {
			t.Fatalf("%s: %+v", c.config, i)
		}
	}
	// Removing settings.json after login falls back to signed out (not unconfirmed).
	os.Remove(file)
	if i := p.Discover(context.Background()); !i.Available || i.LoggedIn || i.LoginUnconfirmed {
		t.Fatalf("after logout (settings removed): %+v", i)
	}
}

func TestAntigravityPermissionModeAndResume(t *testing.T) {
	f := newFixture(t, "FAKE_ACP_REQUIRE_MODE=1", "FAKE_ACP_RESUME=resume")
	f.p.Agent = antigravityAgent("darwin")
	s, err := f.start("write hello.txt yes", "", "")
	if err != nil {
		t.Fatal(err)
	}
	evs, done := collect(t, s)
	if done.IsError {
		t.Fatal(done)
	}
	ref := of(evs, provider.EventRef)[0].Ref
	s.Close()
	s, err = f.start("history", ref, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, done := collect(t, s); done.IsError || done.Text != "history: 2 turns" {
		t.Fatal(done)
	}
}

func TestAntigravityToolFields(t *testing.T) {
	for _, input := range []string{`{"CommandLine":"printf acp-shell-ok"}`, `{"command_line":"printf acp-shell-ok"}`} {
		a := Classify(toolCall{Kind: "execute", Title: "Run command?", RawInput: json.RawMessage(input)})
		if a.Kind != provider.ToolShell || a.Command != "printf acp-shell-ok" {
			t.Fatalf("%+v", a)
		}
	}
	if got := rawText(json.RawMessage(`{"combinedOutput":"acp-shell-ok","exitCode":0}`)); got != "acp-shell-ok" {
		t.Fatal(got)
	}
}

func TestACPLogin(t *testing.T) {
	t.Setenv("FAKE_ACP_AUTH", "oauth-personal")
	state := t.TempDir()
	t.Setenv("FAKE_ACP_STATE", state)
	p := &Provider{Agent: antigravityAgent("darwin"), Binary: fakeBin}
	var out bytes.Buffer
	if err := p.Login(context.Background(), "oauth-personal", &out); err != nil {
		t.Fatal(err)
	}
	files, _ := os.ReadDir(state)
	if len(files) != 0 {
		t.Fatal("login must not create a model session")
	}
	if err := p.Login(context.Background(), "not-offered", &out); err == nil || !strings.Contains(err.Error(), "oauth-personal") {
		t.Fatalf("method validation: %v", err)
	}
	t.Setenv("FAKE_ACP_AUTH_WAIT", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out.Reset()
	if err := p.Login(ctx, "oauth-personal", &out); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancel: %v", err)
	}
	if !strings.Contains(out.String(), "https://example.test/login") {
		t.Fatalf("login link was not forwarded: %s", out.String())
	}
}

func TestAntigravityAuthHint(t *testing.T) {
	f := newFixture(t, "FAKE_ACP_AUTH=oauth-personal")
	f.p.Agent = antigravityAgent("darwin")
	if _, err := f.start("say never", "", ""); err == nil || !strings.Contains(err.Error(), "aide agent login antigravity") {
		t.Fatalf("auth hint: %v", err)
	}
}

// 工单 #111 升级补充: first-class Antigravity permission presets (read-only,
// workspace-write, danger-full-access) and #117 unified "Provider · Model ID" naming.
func TestAntigravityPermissionPresetsAndNaming(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GEMINI_HOME", dir)
	p := &Provider{Agent: antigravityAgent("darwin"), Binary: fakeBin}
	info := p.Discover(context.Background())
	if !info.Available || len(info.Modes) != 3 || info.Modes[0].ID != "read-only" || info.Modes[1].ID != "workspace-write" || info.Modes[2].ID != "danger-full-access" || info.Own.Mode != "workspace-write" {
		t.Fatalf("antigravity modes: %+v", info)
	}
	// read-only gate blocks edits and shell commands, allows reads/searches.
	ro := antigravityPermissionGate("read-only", dir, func(context.Context, provider.ToolAction) provider.Decision {
		return provider.Decision{Allow: true}
	})
	if d := ro(context.Background(), provider.ToolAction{Kind: provider.ToolRead}); !d.Allow {
		t.Fatalf("read-only must allow read: %+v", d)
	}
	if d := ro(context.Background(), provider.ToolAction{Kind: provider.ToolEdit, Paths: []string{"a.txt"}}); d.Allow {
		t.Fatalf("read-only must deny edit: %+v", d)
	}
	if d := ro(context.Background(), provider.ToolAction{Kind: provider.ToolShell, Command: "rm -rf /"}); d.Allow {
		t.Fatalf("read-only must deny shell: %+v", d)
	}
	// workspace-write allows in-workspace edits, blocks path escapes.
	ww := antigravityPermissionGate("workspace-write", dir, func(context.Context, provider.ToolAction) provider.Decision {
		return provider.Decision{Allow: true}
	})
	if d := ww(context.Background(), provider.ToolAction{Kind: provider.ToolEdit, Paths: []string{filepath.Join(dir, "ok.txt")}}); !d.Allow {
		t.Fatalf("workspace-write must allow inside workdir: %+v", d)
	}
	if d := ww(context.Background(), provider.ToolAction{Kind: provider.ToolEdit, Paths: []string{"../escape.txt"}}); d.Allow {
		t.Fatalf("workspace-write must deny escape: %+v", d)
	}
	// Invalid mode fails fast with truthful error naming available presets.
	f := newFixture(t, "FAKE_ACP_REQUIRE_MODE=1")
	f.p.Agent = antigravityAgent("darwin")
	if _, err := f.p.Start(context.Background(), provider.StartSpec{WorkDir: dir, Prompt: "say hi", Mode: "yolo"}); err == nil || !strings.Contains(err.Error(), "read-only, workspace-write, danger-full-access") {
		t.Fatalf("invalid mode error: %v", err)
	}
	// #117 unified naming on real vendor models & grouped options.
	raw := json.RawMessage(`{"configOptions":[{"id":"model","category":"model","type":"select","currentValue":"gemini-2.5-pro","options":[{"value":"gemini-2.5-pro","name":"Gemini 2.5 Pro"},{"value":"claude-sonnet-4-5","name":"Claude Sonnet 4.5"},{"name":"Google","options":[{"value":"gemini-3-flash","name":"Gemini 3 Flash"}]}]}]}`)
	ms, cur, state := modelOptions(raw)
	if state != "known" || cur != "gemini-2.5-pro" || len(ms) != 3 {
		t.Fatalf("modelOptions: state=%s cur=%s ms=%+v", state, cur, ms)
	}
	if ms[0].Name != "Google · gemini-2.5-pro" || ms[1].Name != "Anthropic · claude-sonnet-4-5" || ms[2].Name != "Google · gemini-3-flash" {
		t.Fatalf("#117 names: %+v", ms)
	}
}

// 审计 C: a session started without a mode gets the advertised default
// (workspace-write), not a pass-through; workspace-write denies shell
// commands and network fetches, which Aide cannot confine.
func TestAntigravityEmptyModeIsWorkspaceWrite(t *testing.T) {
	f := newFixture(t, "FAKE_ACP_REQUIRE_MODE=1")
	f.p.Agent = antigravityAgent("darwin")
	s, err := f.start("shell echo should-not-run\nwrite inside.txt ok", "", "")
	if err != nil {
		t.Fatal(err)
	}
	evs, _ := collect(t, s)
	calls := of(evs, provider.EventToolCall)
	if len(calls) != 2 || calls[0].Decision.Allow || !strings.Contains(calls[0].Decision.Reason, "workspace-write") || !calls[1].Decision.Allow {
		t.Fatalf("empty mode must enforce workspace-write: %+v", calls)
	}
	for _, r := range of(evs, provider.EventToolResult) {
		if strings.Contains(r.Summary, "should-not-run") {
			t.Fatalf("denied shell command ran: %+v", r)
		}
	}
	if _, err := os.Stat(filepath.Join(f.dir, "inside.txt")); err != nil {
		t.Fatal("an in-workspace edit must be allowed:", err)
	}
}

func TestAntigravityWorkspaceWriteGate(t *testing.T) {
	ws, _ := filepath.EvalSymlinks(t.TempDir())
	outside, _ := filepath.EvalSymlinks(t.TempDir())
	allow := func(context.Context, provider.ToolAction) provider.Decision { return provider.Decision{Allow: true} }
	for _, mode := range []string{"", "workspace-write"} {
		g := antigravityPermissionGate(mode, ws, allow)
		check := func(name string, a provider.ToolAction, want bool) {
			t.Helper()
			if d := g(context.Background(), a); d.Allow != want || (!want && d.Reason == "") {
				t.Fatalf("mode %q, %s: %+v", mode, name, d)
			}
		}
		check("shell", provider.ToolAction{Kind: provider.ToolShell, Command: "rm -rf ~"}, false)
		check("network", provider.ToolAction{Kind: provider.ToolNetwork, Summary: "fetch https://example.com"}, false)
		check("edit without paths", provider.ToolAction{Kind: provider.ToolEdit}, false)
		check("edit named in input", provider.ToolAction{Kind: provider.ToolEdit, Raw: json.RawMessage(`{"TargetFile":"a.txt"}`)}, true)
		check("edit outside in input", provider.ToolAction{Kind: provider.ToolEdit, Raw: json.RawMessage(`{"file_path":"` + filepath.ToSlash(filepath.Join(outside, "x")) + `"}`)}, false)
		check("read", provider.ToolAction{Kind: provider.ToolRead, Paths: []string{outside}}, true)
		check("new file in new folder", provider.ToolAction{Kind: provider.ToolEdit, Paths: []string{"new/dir/a.txt"}}, true)
	}
	g := antigravityPermissionGate("", ws, allow)
	// A symlink in the workspace to a folder outside it: writes through it
	// land outside.
	if err := os.Symlink(outside, filepath.Join(ws, "link")); err != nil {
		t.Skip("symlinks:", err)
	}
	if d := g(context.Background(), provider.ToolAction{Kind: provider.ToolEdit, Paths: []string{"link/new.txt"}}); d.Allow {
		t.Fatalf("edit through a symlink out of the workspace: %+v", d)
	}
	// A dangling symlink pointing outside.
	os.Symlink(filepath.Join(outside, "missing.txt"), filepath.Join(ws, "dangling"))
	if d := g(context.Background(), provider.ToolAction{Kind: provider.ToolEdit, Paths: []string{filepath.Join(ws, "dangling")}}); d.Allow {
		t.Fatalf("edit through a dangling symlink out of the workspace: %+v", d)
	}
	// The workspace itself reached through a symlink still counts as inside.
	alias := filepath.Join(outside, "ws-alias")
	os.Symlink(ws, alias)
	g = antigravityPermissionGate("", alias, allow)
	if d := g(context.Background(), provider.ToolAction{Kind: provider.ToolEdit, Paths: []string{filepath.Join(ws, "ok.txt")}}); !d.Allow {
		t.Fatalf("workspace via symlink: %+v", d)
	}
	if d := antigravityPermissionGate("danger-full-access", ws, allow)(context.Background(), provider.ToolAction{Kind: provider.ToolShell}); !d.Allow {
		t.Fatalf("danger-full-access: %+v", d)
	}
}

// 审计 C: a denied call the agent runs without asking permission cannot be
// blocked; it must be reported as such and its turn cancelled.
func TestDeniedCallRunWithoutAsking(t *testing.T) {
	f := newFixture(t)
	f.gate.deny = true
	s, err := f.start("unasked rm -rf scratch\nsay after", "", "")
	if err != nil {
		t.Fatal(err)
	}
	evs, done := collect(t, s)
	errs := of(evs, provider.EventError)
	if len(errs) != 1 || !strings.Contains(errs[0].Text, "without asking permission") || !strings.Contains(errs[0].Text, "denied by test") {
		t.Fatalf("errors: %+v", errs)
	}
	if !done.IsError || done.Text != "interrupted" {
		t.Fatalf("the turn must be cancelled: %+v", done)
	}
	for _, m := range of(evs, provider.EventAgentMessage) {
		if strings.Contains(m.Text, "after") {
			t.Fatalf("the turn went on: %+v", m)
		}
	}
}
