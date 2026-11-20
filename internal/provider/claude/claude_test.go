package claude

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/provider"
)

func fixture(t *testing.T, name string) line {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var l line
	if err := json.Unmarshal(b, &l); err != nil {
		t.Fatal(err)
	}
	return l
}

func TestResultFailure(t *testing.T) {
	stderr, _ := os.ReadFile("testdata/claude-2.1.292-unknown-model.stderr.txt")
	for _, c := range []struct {
		name   string
		l      line
		stderr string
		want   sessionv1.FailureKind
		msg    string
	}{
		// Real Claude Code 2.1.292 result lines.
		{"resume not found", fixture(t, "claude-2.1.292-resume-not-found.result.json"), "", sessionv1.FailureKind_FAILURE_KIND_RESUME_REJECTED, "No conversation found"},
		{"unknown model", fixture(t, "claude-2.1.292-unknown-model.result.json"), string(stderr), sessionv1.FailureKind_FAILURE_KIND_MODEL_UNAVAILABLE, "issue with the selected model"},
		// Provider result shapes and the CLI's subtypes.
		{"context exhausted reported as success", line{Subtype: "success", TerminalReason: "prompt_too_long", Result: "Prompt is too long"}, "", sessionv1.FailureKind_FAILURE_KIND_CONTEXT_OVERFLOW, "Prompt is too long"},
		{"max turns", line{Subtype: "error_max_turns", IsError: true}, "", sessionv1.FailureKind_FAILURE_KIND_LIMIT_REACHED, "maximum number of turns"},
		{"rate limited", line{IsError: true, APIErrorStatus: 429, Result: "API Error: Request rejected"}, "", sessionv1.FailureKind_FAILURE_KIND_RATE_LIMIT, "Request rejected"},
		{"login only on stderr", line{IsError: true, Subtype: "error_during_execution"}, "warn: x\nInvalid API key · Please run /login\n", sessionv1.FailureKind_FAILURE_KIND_AUTH, "Please run /login"},
		{"unrecognized", line{IsError: true, Subtype: "error_during_execution"}, "", sessionv1.FailureKind_FAILURE_KIND_UNKNOWN, "error_during_execution"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := ResultFailure(c.l, c.stderr)
			if f == nil || f.Kind != c.want || !strings.Contains(f.Message, c.msg) {
				t.Fatalf("got %+v, want %v containing %q", f, c.want, c.msg)
			}
		})
	}
	if f := ResultFailure(line{Subtype: "success", Result: "done"}, ""); f != nil {
		t.Fatalf("success: %+v", f)
	}
}

// A refused --resume starts the process normally; the reason only appears
// in the result line, which must reach the kernel as a classified failure.
func TestStartReportsResumeRejection(t *testing.T) {
	scripted(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nread a\nread b\ncat " + filepath.Join(mustAbs(t, "testdata"), "claude-2.1.292-resume-not-found.result.json") +
		"\necho 'No conversation found with session ID: x' >&2\nexit 1\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := (&Provider{Binary: bin}).Start(context.Background(), provider.StartSpec{WorkDir: dir,
		Prompt: "hi", ResumeRef: "00000000-0000-4000-8000-000000000000", Env: os.Environ()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case e, ok := <-s.Events():
			if !ok {
				t.Fatal("events closed without a turn result")
			}
			if e.Kind != provider.EventTurnCompleted {
				continue
			}
			if !e.IsError || e.Failure == nil || e.Failure.Kind != sessionv1.FailureKind_FAILURE_KIND_RESUME_REJECTED || !strings.Contains(e.Text, "No conversation found") {
				t.Fatalf("turn: %+v failure %+v", e, e.Failure)
			}
			return
		case <-timeout:
			t.Fatal("timed out")
		}
	}
}

func mustAbs(t *testing.T, p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// Claude Code's init line names the session and the model it runs on
// (2.1.312: the full ID, e.g. "claude-haiku-5-5", whatever alias it was
// given); it does not name the reasoning effort.
func TestInitReportsModel(t *testing.T) {
	scripted(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nread init\nread prompt\n" +
		`echo '{"type":"system","subtype":"init","session_id":"c1e9","model":"claude-haiku-5-5","per_turn_effort_active":true}'` + "\nsleep 1\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := (&Provider{Binary: bin}).Start(context.Background(), provider.StartSpec{WorkDir: dir, Prompt: "hi", Model: "haiku", Env: os.Environ()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	select {
	case e := <-s.Events():
		if e.Kind != provider.EventRef || e.Ref != "c1e9" || e.Model != "claude-haiku-5-5" || e.Effort != "" {
			t.Fatalf("first event: %+v", e)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out")
	}
}

// A message steered after Claude finished the turn's model calls is run as
// one more Claude turn (two results, as Claude Code 2.1.292 does): Aide
// reports one turn, ending with the last result.
func TestSteerAfterLastToolIsOneTurn(t *testing.T) {
	scripted(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	replay := func(text string) string {
		return `echo '{"type":"user","message":{"role":"user","content":"` + text + `"},"isReplay":true}'` + "\n"
	}
	say := func(text string) string {
		return `echo '{"type":"assistant","message":{"content":[{"type":"text","text":"` + text + `"}]}}'` + "\n"
	}
	result := func(text string) string {
		return `echo '{"type":"result","subtype":"success","result":"` + text + `","usage":{"input_tokens":3,"output_tokens":2}}'` + "\n"
	}
	script := "#!/bin/sh\nread init\nread prompt\n" + replay("hi") + say("first") +
		"read steer\n" + result("first") + replay("more") + say("second") + result("second") + "sleep 1\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := (&Provider{Binary: bin}).Start(context.Background(), provider.StartSpec{WorkDir: dir, Prompt: "hi", Env: os.Environ()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	next := func() provider.Event {
		t.Helper()
		select {
		case e := <-s.Events():
			return e
		case <-time.After(10 * time.Second):
			t.Fatal("timed out")
		}
		return provider.Event{}
	}
	for e := next(); e.Kind != provider.EventAgentMessage; e = next() {
	}
	if err := s.Steer(context.Background(), provider.Input{Text: "more"}); err != nil {
		t.Fatal(err)
	}
	var msgs []string
	for {
		e := next()
		if e.Kind == provider.EventAgentMessage {
			msgs = append(msgs, e.Text)
		}
		if e.Kind == provider.EventTurnCompleted {
			if e.Text != "second" || e.IsError || e.Usage.InputTokens != 6 || len(msgs) != 1 {
				t.Fatalf("turn: %+v, messages %q", e, msgs)
			}
			break
		}
	}
	select {
	case e := <-s.Events():
		t.Fatalf("event after the turn: %+v", e)
	case <-time.After(300 * time.Millisecond):
	}
	if err := s.Steer(context.Background(), provider.Input{Text: "late"}); err == nil {
		t.Fatal("steer between turns must fail")
	}
}

// AskUserQuestion arrives as a permission request (with Aide as the
// permission host); the answer goes back as the tool's input with
// answers keyed by question text, as Claude Code 2.1.292 takes it.
func TestAskUserQuestion(t *testing.T) {
	scripted(t)
	if args := strings.Join(Args(provider.StartSpec{}), " "); !strings.Contains(args, "--permission-prompt-tool stdio") || strings.Contains(args, "--permission-prompts") {
		t.Fatal(args)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	ask := `{"type":"control_request","request_id":"perm-1","request":{"subtype":"can_use_tool","tool_name":"AskUserQuestion","tool_use_id":"tu1",` +
		`"input":{"questions":[{"question":"Which color?","header":"Color","multiSelect":false,"options":[{"label":"Red","description":"warm"},{"label":"Blue","description":"cool"}]}]}}}`
	script := "#!/bin/sh\nread init\nread prompt\n" +
		`echo '{"type":"user","message":{"role":"user","content":"hi"},"isReplay":true}'` + "\n" +
		"echo '" + ask + "'\nread resp\n" +
		`case "$resp" in *'"behavior":"allow"'*'"answers":{"Which color?":"Blue, deep"}'*) r=ok;; *) r=bad;; esac` + "\n" +
		`echo '{"type":"result","subtype":"success","result":"'$r'"}'` + "\nsleep 1\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := (&Provider{Binary: bin}).Start(context.Background(), provider.StartSpec{WorkDir: dir, Prompt: "hi", Env: os.Environ()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for {
		select {
		case e := <-s.Events():
			switch e.Kind {
			case provider.EventQuestion:
				q := e.Question
				if len(q.Items) != 1 || q.Items[0].Header != "Color" || q.Items[0].Text != "Which color?" || len(q.Items[0].Options) != 2 || !q.Items[0].AllowOther {
					t.Fatalf("question: %+v", q)
				}
				if err := s.Answer(context.Background(), q.ID, provider.Reply{Answers: []provider.Answer{{ItemID: "1", Selected: []string{"Blue"}, Text: "deep"}}}); err != nil {
					t.Fatal(err)
				}
			case provider.EventTurnCompleted:
				if e.Text != "ok" {
					t.Fatalf("the CLI got the wrong answer (%s)", e.Text)
				}
				return
			}
		case <-time.After(10 * time.Second):
			t.Fatal("timed out")
		}
	}
}

// Without a CLI install, the newest Claude Code the desktop app keeps is used.
func TestDesktopCLI(t *testing.T) {
	scripted(t)
	home := t.TempDir()
	base := filepath.Join(home, "Library/Application Support/Claude/claude-code")
	mk := func(version, build string) string {
		dir := filepath.Join(base, version, build, "claude.app/Contents/MacOS")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		bin := filepath.Join(dir, "claude")
		if err := os.WriteFile(bin, []byte("#!/bin/sh\necho '"+version+" (Claude Code)'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return bin
	}
	mk("2.1.99", "aaa")
	newest := mk("2.1.299", "bbb")
	os.MkdirAll(filepath.Join(base, "2.1.300", "ccc"), 0o755) // a download not finished: no binary yet

	if got := desktopCLI(home); got != newest {
		t.Fatalf("got %q, want %q", got, newest)
	}
	// The binary is the Claude Code it says (checked as a found one is).
	if info := (&Provider{Binary: newest}).Discover(context.Background()); !info.Available || info.Version != "2.1.299" {
		t.Fatalf("got %+v", info)
	}
}

// With --include-partial-messages the reply streams as drafts (stream_event
// text deltas, as Claude Code sends them) that add up to the assistant
// message; a subagent's deltas (parent_tool_use_id) are not the reply.
func TestDraftsAddUpToMessage(t *testing.T) {
	scripted(t)
	if !strings.Contains(strings.Join(Args(provider.StartSpec{}), " "), "--include-partial-messages") {
		t.Fatal("drafts need --include-partial-messages")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	delta := func(text, parent string) string {
		return `echo '{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"` + text + `"}},"parent_tool_use_id":` + parent + `}'` + "\n"
	}
	script := "#!/bin/sh\nread init\nread prompt\n" +
		`echo '{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}},"parent_tool_use_id":null}'` + "\n" +
		delta("Hel", "null") + delta("sub", `"toolu_1"`) + delta("lo", "null") +
		`echo '{"type":"assistant","message":{"content":[{"type":"text","text":"Hello"}]}}'` + "\n" +
		`echo '{"type":"result","subtype":"success","result":"Hello","usage":{"input_tokens":3,"output_tokens":2}}'` + "\nsleep 1\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := (&Provider{Binary: bin}).Start(context.Background(), provider.StartSpec{WorkDir: dir, Prompt: "hi", Env: os.Environ()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var drafts, msg string
	for msg == "" {
		select {
		case e := <-s.Events():
			switch e.Kind {
			case provider.EventAgentDraft:
				drafts += e.Text
			case provider.EventAgentMessage:
				msg = e.Text
			}
		case <-time.After(10 * time.Second):
			t.Fatal("timed out")
		}
	}
	if drafts != "Hello" || msg != "Hello" {
		t.Fatalf("drafts %q, message %q", drafts, msg)
	}
}

// The model and effort a session runs with go on the command line, for it
// alone; unset, Claude Code's own settings apply.
func TestModelArgs(t *testing.T) {
	args := strings.Join(Args(provider.StartSpec{Model: "opus", Effort: "low"}), " ")
	if !strings.Contains(args, "--model opus") || !strings.Contains(args, "--effort low") {
		t.Fatal(args)
	}
	if args := strings.Join(Args(provider.StartSpec{}), " "); !strings.Contains(args, `--settings {"crossSessionInbound":"refuse"}`) {
		t.Fatalf("Claude Code's own inbox must stay shut: %s", args)
	}
	if args := strings.Join(Args(provider.StartSpec{}), " "); strings.Contains(args, "--model") || strings.Contains(args, "--effort") {
		t.Fatal(args)
	}
}

// A managed workspace's section goes after Claude Code's own system prompt;
// without one, nothing is added.
func TestInstructionsArgs(t *testing.T) {
	args := Args(provider.StartSpec{Instructions: "# 受管工作区\nline two"})
	i := slices.Index(args, "--append-system-prompt")
	if i < 0 || args[i+1] != "# 受管工作区\nline two" {
		t.Fatal(args)
	}
	if slices.Contains(Args(provider.StartSpec{}), "--append-system-prompt") {
		t.Fatal("added without instructions")
	}
}

// scripted skips a test whose fake Claude Code is a shell script.
func scripted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake agent is a shell script")
	}
}

// On Windows the desktop app keeps Claude Code under its userData folder:
// %APPDATA%\Claude, the MSIX package's redirected copy of it, or
// %LOCALAPPDATA%\Claude-3p, at claude-code\<version>\claude.exe or one
// <hash> folder deeper. The newest version found in any of them is used.
func TestWindowsDesktopCLI(t *testing.T) {
	mk := func(t *testing.T, userData, version, hash string) string {
		dir := filepath.Join(userData, "claude-code", version, hash)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		bin := filepath.Join(dir, "claude.exe")
		if err := os.WriteFile(bin, nil, 0o755); err != nil {
			t.Fatal(err)
		}
		return bin
	}
	t.Run("none", func(t *testing.T) {
		if got := windowsDesktopCLI("", ""); got != "" {
			t.Fatalf("no variables: got %q", got)
		}
		appData, local := t.TempDir(), t.TempDir()
		if got := windowsDesktopCLI(appData, local); got != "" {
			t.Fatalf("no app: got %q", got)
		}
		if windowsApp(appData, local, nil, nil) {
			t.Fatal("no app: reported installed")
		}
		// An empty version folder (a download not finished) is no Claude
		// Code; and the app's data, which its uninstall leaves behind, is no
		// sign of the app.
		os.MkdirAll(filepath.Join(appData, "Claude", "claude-code", "2.1.300"), 0o755)
		if got := windowsDesktopCLI(appData, local); got != "" {
			t.Fatalf("empty version folder: got %q", got)
		}
		if windowsApp(appData, local, nil, nil) {
			t.Fatal("the app's data left behind taken for the app")
		}
		// Its installer's program is.
		stub := filepath.Join(local, "AnthropicClaude", "claude.exe")
		os.MkdirAll(filepath.Dir(stub), 0o755)
		os.WriteFile(stub, nil, 0o755)
		if !windowsApp(appData, local, nil, nil) {
			t.Fatal("app without its CLI: not reported")
		}
		if got := runningCLI([]string{stub}); got != "" {
			t.Fatalf("the installer's claude.exe taken for Claude Code: %q", got)
		}
	})
	t.Run("store package", func(t *testing.T) {
		appData, local := t.TempDir(), t.TempDir()
		resource := []string{"Claude_6.6395.1.0_neutral_split.language-en_pzs8sxrjxfjjc", "ClaudeNotes_1.0.0.0_x64__abc"}
		if windowsApp(appData, local, nil, resource) {
			t.Fatal("a resource package, or another app, taken for the app")
		}
		if !windowsApp(appData, local, nil, append(resource, "Claude_6.6395.1.0_x64__pzs8sxrjxfjjc")) {
			t.Fatal("the registered package: not reported")
		}
		os.MkdirAll(filepath.Join(local, "Packages", "AnthropicPBC.Claude_pzs8sxrjxfjjc", "LocalCache"), 0o755)
		if !windowsApp(appData, local, nil, nil) {
			t.Fatal("the package's folder: not reported")
		}
	})
	t.Run("both depths, newest", func(t *testing.T) {
		appData, local := t.TempDir(), t.TempDir()
		ud := filepath.Join(appData, "Claude")
		mk(t, ud, "2.1.99", "")
		newest := mk(t, ud, "2.1.299", "0f3a9c")
		if got := windowsDesktopCLI(appData, local); got != newest {
			t.Fatalf("got %q, want %q", got, newest)
		}
		flat := mk(t, ud, "2.1.300", "")
		if got := windowsDesktopCLI(appData, local); got != flat {
			t.Fatalf("got %q, want %q", got, flat)
		}
	})
	t.Run("msix and 3p", func(t *testing.T) {
		appData, local := t.TempDir(), t.TempDir()
		mk(t, filepath.Join(appData, "Claude"), "2.0.5", "")
		msix := mk(t, filepath.Join(local, "Packages", "Claude_pzs8sxrjxfjjc", "LocalCache", "Roaming", "Claude"), "2.1.10", "abc")
		if got := windowsDesktopCLI(appData, local); got != msix {
			t.Fatalf("got %q, want the MSIX one %q", got, msix)
		}
		threeP := mk(t, filepath.Join(local, "Claude-3p"), "2.1.11", "")
		if got := windowsDesktopCLI(appData, local); got != threeP {
			t.Fatalf("got %q, want the 3p one %q", got, threeP)
		}
		if got := windowsDesktopCLI("", local); got != threeP {
			t.Fatalf("without APPDATA: got %q, want %q", got, threeP)
		}
	})
	// The layouts the app's builds have been seen with (2026-11-20: the
	// Code page in use, and Claude Code found in none of the folders above):
	// the MSIX package with its Local redirected too, 3p in it with a
	// <build-id> folder (Anthropic's third-party docs), and the one asked of
	// the app for redirected Roaming folders.
	for _, c := range []struct{ name, userData string }{
		{"msix 3p", filepath.Join("Packages", "Claude_pzs8sxrjxfjjc", "LocalCache", "Roaming", "Claude-3p")},
		{"msix local", filepath.Join("Packages", "Claude_pzs8sxrjxfjjc", "LocalCache", "Local", "Claude")},
		{"msix local 3p", filepath.Join("Packages", "Claude_pzs8sxrjxfjjc", "LocalCache", "Local", "Claude-3p")},
		{"localappdata", "Claude"},
	} {
		t.Run(c.name, func(t *testing.T) {
			appData, local := t.TempDir(), t.TempDir()
			want := mk(t, filepath.Join(local, c.userData), "2.1.308", "b1f2")
			if got := windowsDesktopCLI(appData, local); got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
	// Its own folder per package under Claude-Data (seen on a computer with
	// the MSIX build 6.6395, 2026-11-20): found by the rule, not this path.
	t.Run("data folder per package", func(t *testing.T) {
		appData, local := t.TempDir(), t.TempDir()
		want := mk(t, filepath.Join(local, "Claude-Data", "claude_pzs8sxrjxfjjc"), "2.1.326", "d31a4581af92")
		if got := windowsDesktopCLI(appData, local); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
	t.Run("another Claude folder, two down", func(t *testing.T) {
		appData, local := t.TempDir(), t.TempDir()
		want := mk(t, filepath.Join(appData, "claude-next", "a", "b"), "3.0.1", "")
		if got := windowsDesktopCLI(appData, local); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
	t.Run("not another app's, nor its VM's", func(t *testing.T) {
		appData, local := t.TempDir(), t.TempDir()
		mk(t, filepath.Join(local, "OtherApp"), "9.9.9", "")
		vm := filepath.Join(local, "Claude", "claude-code-vm", "2.1.326")
		os.MkdirAll(vm, 0o755)
		os.WriteFile(filepath.Join(vm, "claude.exe"), nil, 0o755)
		mk(t, filepath.Join(local, "Claude", "a", "b", "c"), "2.1.1", "") // three down
		if got := windowsDesktopCLI(appData, local); got != "" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("the app running, its data not fetched yet", func(t *testing.T) {
		appData, local := t.TempDir(), t.TempDir()
		pkg := filepath.Join(t.TempDir(), "WindowsApps", "Claude_6.6395.1.0_x64__pzs8sxrjxfjjc", "app", "Claude.exe")
		if windowsApp(appData, local, nil, nil) {
			t.Fatal("no app: reported installed")
		}
		if !windowsApp(appData, local, []string{pkg}, nil) {
			t.Fatal("the app running from its MSIX package: not reported")
		}
		if got := runningCLI([]string{pkg}); got != "" {
			t.Fatalf("the app taken for Claude Code: %q", got)
		}
	})
	t.Run("roaming 3p", func(t *testing.T) {
		appData, local := t.TempDir(), t.TempDir()
		want := mk(t, filepath.Join(appData, "Claude-3p"), "2.1.308", "")
		if got := windowsDesktopCLI(appData, local); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}

// The Claude Code the app's Code page runs is found by its running program
// wherever the app keeps it; the app itself (an Electron app, app.asar
// beside it) is not Claude Code.
func TestRunningCLI(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "AnthropicClaude", "app-1.2.3", "claude.exe")
	cli := filepath.Join(root, "Somewhere", "claude-code", "9.9.9", "claude.exe")
	for _, f := range []string{app, cli, filepath.Join(filepath.Dir(app), "resources", "app.asar")} {
		os.MkdirAll(filepath.Dir(f), 0o755)
		os.WriteFile(f, nil, 0o755)
	}
	if got := runningCLI([]string{app}); got != "" {
		t.Fatalf("the desktop app taken for Claude Code: %q", got)
	}
	if got := runningCLI([]string{app, cli}); got != cli {
		t.Fatalf("got %q, want %q", got, cli)
	}
	if got := runningCLI([]string{filepath.Join(root, "gone", "claude.exe")}); got != "" {
		t.Fatalf("a program no longer there: %q", got)
	}
	if runtime.GOOS == "windows" {
		if got := runningCLI([]string{`C:\Program Files\WindowsApps\Claude_1.0.0.0_x64__pzs8sxrjxfjjc\app\claude.exe`}); got != "" {
			t.Fatalf("the MSIX app taken for Claude Code: %q", got)
		}
	}
}

// The app's execution alias on PATH (Microsoft\WindowsApps\Claude.exe)
// starts the app, so it is not looked in for claude.
func TestWithoutAppAliases(t *testing.T) {
	alias := filepath.Join("C", "Users", "u", "AppData", "Local", "Microsoft", "WindowsApps")
	other := filepath.Join("C", "Users", "u", ".local", "bin")
	path := strings.Join([]string{alias, other, filepath.Join("C", "Tools", "WindowsApps")}, string(filepath.ListSeparator))
	want := strings.Join([]string{other, filepath.Join("C", "Tools", "WindowsApps")}, string(filepath.ListSeparator))
	if got := withoutAppAliases(path); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
