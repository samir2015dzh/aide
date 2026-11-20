package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samir2015dzh/aide/internal/provider/acp/dshnative"
)

// The bridge plugin as DSH Desktop runs it (apps/dsh-bridge, loaded by Node
// with DSH's services faked) and aided's client of it speak the same
// protocol: hello, state, a prompt in queue mode under Aide's request id,
// cancel, a DSH that never takes a message failing as a timeout, and the
// rendezvous gone when DSH disposes the plugin (the token's refusal is in
// the plugin's own tests). On Windows the endpoint is a named pipe.
func TestDSHBridgePlugin(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	entry, _ := filepath.Abs(filepath.Join("apps", "dsh-bridge", "src", "index.ts")) // TestMain runs from the repository root
	script, _ := filepath.Abs(filepath.Join("e2e", "testdata", "dsh-bridge-host.mjs"))
	home, err := os.MkdirTemp("", "hmdsh-") // short: a Unix socket's path is
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })

	cmd := exec.Command(node, script, entry)
	cmd.Env = append(os.Environ(), "DSH_HOME="+home)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	// Node's own errors, kept for the failure message as well as shown.
	var stderr lockedBuffer
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
	began := time.Now()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	t.Cleanup(func() { cmd.Process.Kill(); <-exited })
	lines := make(chan map[string]any, 16)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			var m map[string]any
			json.Unmarshal(sc.Bytes(), &m)
			lines <- m
		}
		close(lines)
	}()
	// what failed to come, and everything known of the Host process then:
	// how long it ran, whether it ended and how, and what Node printed.
	fail := func(what string, ended error) {
		t.Helper()
		state := "still running"
		if ended != nil || cmd.ProcessState != nil {
			state = fmt.Sprintf("ended (%v)", ended)
		}
		t.Fatalf("no %s from the plugin after %s; the Host is %s; node's stderr: %q",
			what, time.Since(began).Round(time.Millisecond), state, stderr.String())
	}
	// The next line within d, failing at once if the Host ends first.
	nextWithin := func(what string, d time.Duration) map[string]any {
		t.Helper()
		select {
		case m, ok := <-lines:
			if !ok {
				select {
				case err := <-exited:
					exited <- err
					fail(what, err)
				case <-time.After(5 * time.Second):
					fail(what, nil)
				}
			}
			return m
		case err := <-exited:
			exited <- err
			fail(what, err)
		case <-time.After(d):
			fail(what, nil)
		}
		return nil
	}
	next := func(what string) map[string]any { t.Helper(); return nextWithin(what, 15*time.Second) }
	// Readiness comes in two steps, each with its own signal:
	//  1. Node runs: the Host script's first line, {"node": version}, written
	//     before it loads anything. How long that takes is the computer's, not
	//     the plugin's: a fresh Windows runner scans the first node.exe it
	//     starts before it runs, which once took longer than the 15 s this
	//     test allowed for everything (windows-e2e, 2026-11-19). So it may take
	//     up to 2 minutes while Node is alive; Node ending fails at once, with
	//     what it printed.
	//  2. The plugin is ready: {"ready": true}, once DSH's apply() returned:
	//     the plugin loaded (its TypeScript stripped by Node) and its
	//     rendezvous is up. That is the plugin's own work: 15 s, as before.
	hello := nextWithin("start of node", 2*time.Minute)
	if hello["node"] == nil {
		t.Fatalf("plugin: %v", hello)
	}
	nodeStarted := time.Since(began)
	if m := next("start"); m["ready"] != true {
		t.Fatalf("plugin: %v", m)
	}
	ready := time.Since(began) - nodeStarted
	timing := fmt.Sprintf("node %v started in %s; the plugin was ready %s later", hello["node"], nodeStarted.Round(time.Millisecond), ready.Round(time.Millisecond))
	t.Log(timing)
	// In CI, also where a passing run shows it (the job's summary).
	if f := os.Getenv("GITHUB_STEP_SUMMARY"); f != "" {
		if w, err := os.OpenFile(f, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644); err == nil {
			fmt.Fprintf(w, "TestDSHBridgePlugin: %s\n\n", timing)
			w.Close()
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var b *dshnative.Bridge
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		if b, err = dshnative.DialBridge(ctx, home); err == nil || time.Now().After(deadline) {
			break
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if b.Hello.Protocol != 1 || b.Hello.Version == "" || !b.Hello.Services.Agents {
		t.Fatalf("hello: %+v", b.Hello)
	}
	if live, status, err := b.State(ctx, "s_live"); err != nil || !live || status != "running" {
		t.Fatalf("state: %v %q %v", live, status, err)
	}
	if live, _, err := b.State(ctx, "s_cold"); err != nil || live {
		t.Fatalf("state of a session the Host doesn't run: %v %v", live, err)
	}
	if err := b.Prompt(ctx, "s_cold", "aide-01h", "继续，按计划"); err != nil {
		t.Fatal(err)
	}
	m := next("prompt")
	p, _ := m["prompt"].(map[string]any)
	if p["sessionId"] != "s_cold" || p["requestId"] != "aide-01h" || p["mode"] != "queue" || m["signal"] != true ||
		!strings.Contains(asJSON(p["content"]), `"text":"继续，按计划"`) {
		t.Fatalf("DSH was asked %v", m)
	}
	if ok, err := b.Cancel(ctx, "s_live"); err != nil || !ok {
		t.Fatalf("cancel: %v %v", ok, err)
	}
	if m := next("cancel"); m["cancel"] != "s_live" || asJSON(m["reason"]) != `{"kind":"user"}` {
		t.Fatalf("DSH was asked %v", m)
	}
	if ok, err := b.Cancel(ctx, "s_cold"); err != nil || ok {
		t.Fatalf("cancel of a session the Host doesn't run: %v %v", ok, err)
	}
	var be *dshnative.BridgeError
	if err := b.Prompt(ctx, "s_live", "aide-02", "  "); !errors.As(err, &be) || be.Code != "bad-request" {
		t.Fatalf("an empty message: %v", err)
	}
	// The plugin gives up on DSH after 20 s and says so.
	pctx, pcancel := context.WithTimeout(ctx, 25*time.Second)
	err = b.Prompt(pctx, "s_live", "aide-03", "hang")
	pcancel()
	if !errors.As(err, &be) || be.Code != "timeout" {
		t.Fatalf("a DSH that never takes it: %v", err)
	}

	// Disposed with DSH: the rendezvous goes, and nothing answers.
	stdin.Close()
	for m := next("disposal"); m["disposed"] != true; m = next("disposal") {
	}
	if _, err := os.Stat(filepath.Join(dshnative.BridgeDir(home), "bridge.json")); !os.IsNotExist(err) {
		t.Fatalf("bridge.json after disposal: %v", err)
	}
	if _, err := dshnative.DialBridge(ctx, home); !errors.Is(err, dshnative.ErrBridgeNotInstalled) {
		t.Fatalf("after disposal: %v", err)
	}
}

// lockedBuffer is a bytes.Buffer safe to write from os/exec's copying goroutine
// while the test reads it.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func asJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
