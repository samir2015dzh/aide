package acp

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hermeshai/hermesh/internal/provider"
)

// A custom agent named by its path: the handshake starts it, it answers
// initialize with what it is, and opens a session in the folder given.
func TestHandshake(t *testing.T) {
	dir := t.TempDir()
	p := &Provider{Agent: Agent{Name: "mine", Command: fakeBin, Custom: true, Env: []string{"FAKE_ACP_STATE=" + t.TempDir()}}}
	g, err := p.Handshake(context.Background(), os.Environ(), dir)
	if err != nil || !g.Session || g.Title != "Fake ACP" || g.Version != "1.2.3" {
		t.Fatalf("handshake: %+v %v", g, err)
	}
	// Discover is the handshake without a session: it needs no --version.
	if info := p.Discover(context.Background()); !info.Available || info.Version != "1.2.3" || info.Binary != fakeBin {
		t.Fatalf("discover: %+v", info)
	}
}

// One that needs a sign-in it was not told how to do says which methods
// it offers; one Hermesh knows how to sign in opens its session.
func TestHandshakeAuth(t *testing.T) {
	env := append(os.Environ(), "FAKE_ACP_AUTH=login", "FAKE_ACP_STATE="+t.TempDir())
	p := &Provider{Agent: Agent{Name: "mine", Command: fakeBin, Custom: true}}
	g, err := p.Handshake(context.Background(), env, t.TempDir())
	if strings.Join(g.Auth, ",") != "other,login" {
		t.Fatalf("auth methods: %+v %v", g, err)
	}
	p.Auth = []string{"login"}
	if g, err := p.Handshake(context.Background(), env, t.TempDir()); err != nil || !g.Session {
		t.Fatalf("signed in: %+v %v", g, err)
	}
}

// What fails says why: a command that is not there, one that ends at once
// (with what it printed), one that never answers.
func TestHandshakeFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-ins are shell scripts")
	}
	ctx := context.Background()
	p := &Provider{Agent: Agent{Name: "mine", Command: "hermesh-no-such-agent", Custom: true}, PathEnv: t.TempDir()}
	if _, err := p.Handshake(ctx, os.Environ(), t.TempDir()); err == nil || !strings.Contains(err.Error(), "hermesh-no-such-agent not found") {
		t.Fatalf("missing: %v", err)
	}
	if info := p.Discover(ctx); info.Available || !strings.Contains(info.Detail, "not found") {
		t.Fatalf("discover missing: %+v", info)
	}
	bin := t.TempDir()
	quits := filepath.Join(bin, "quits")
	os.WriteFile(quits, []byte("#!/bin/sh\necho 'unknown option: --acp' >&2\nexit 2\n"), 0o755)
	p = &Provider{Agent: Agent{Name: "mine", Command: quits, Args: []string{"--acp"}, Custom: true}}
	_, err := p.Handshake(ctx, os.Environ(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "initialize: it ended before answering (exit status 2: unknown option: --acp)") {
		t.Fatalf("quits: %v", err)
	}
	silent := filepath.Join(bin, "silent")
	os.WriteFile(silent, []byte("#!/bin/sh\nexec sleep 60\n"), 0o755)
	p = &Provider{Agent: Agent{Name: "mine", Command: silent, Custom: true}}
	tctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	if _, err := p.Handshake(tctx, os.Environ(), t.TempDir()); err == nil || !strings.Contains(err.Error(), "initialize: no answer in time") {
		t.Fatalf("silent: %v", err)
	}
}

// A relative command is found in the agent's own folder, and its process
// starts there; the session's folder is still the cwd it is given.
func TestCustomDir(t *testing.T) {
	home := t.TempDir()
	name := "agent"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	b, err := os.ReadFile(fakeBin)
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(home, "bin"), 0o755)
	if err := os.WriteFile(filepath.Join(home, "bin", name), b, 0o755); err != nil {
		t.Fatal(err)
	}
	f := newFixture(t)
	f.p = &Provider{Agent: Agent{Name: "mine", Command: filepath.Join("bin", "agent"), Custom: true, Dir: home, Env: []string{"FAKE_ACP_STATE=" + t.TempDir(), "MINE_TOKEN=s3"}}}
	s, err := f.start("cwd\nenv MINE_TOKEN", "", "")
	if err != nil {
		t.Fatal(err)
	}
	evs, _ := collect(t, s)
	// The process's folder as the system names it: on macOS with its links
	// resolved, on Windows perhaps in the short form TEMP gives (RUNNER~1).
	resolved, _ := filepath.EvalSymlinks(home)
	msgs := of(evs, provider.EventAgentMessage)
	want := func(dir string) string { return "cwd: " + f.dir + " " + dir + "env: MINE_TOKEN=s3" }
	if len(msgs) != 1 || msgs[0].Text != want(home) && msgs[0].Text != want(resolved) {
		t.Fatalf("messages: %+v (want the session's folder %s, the process's %s)", msgs, f.dir, resolved)
	}
}
