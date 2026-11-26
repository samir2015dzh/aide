package acp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samir2015dzh/aide/internal/provider"
)

// events reads a session's events as a test waits on them.
type events struct {
	t   *testing.T
	s   provider.Session
	all []provider.Event
}

// until returns the first event, read or to come, that matches.
func (r *events) until(what string, match func(provider.Event) bool) provider.Event {
	r.t.Helper()
	for _, e := range r.all {
		if match(e) {
			return e
		}
	}
	deadline := time.After(20 * time.Second)
	for {
		select {
		case e, ok := <-r.s.Events():
			if !ok {
				r.t.Fatalf("events closed waiting for %s: %v", what, r.s.Err())
			}
			r.all = append(r.all, e)
			if match(e) {
				return e
			}
		case <-deadline:
			r.t.Fatalf("timed out waiting for %s; events: %+v", what, r.all)
		}
	}
}

// quiet reads events for d, failing on one that matches.
func (r *events) quiet(what string, d time.Duration, bad func(provider.Event) bool) {
	r.t.Helper()
	deadline := time.After(d)
	for {
		select {
		case e, ok := <-r.s.Events():
			if !ok {
				return
			}
			r.all = append(r.all, e)
			if bad(e) {
				r.t.Fatalf("%s: %+v", what, e)
			}
		case <-deadline:
			return
		}
	}
}

// result matches a Background result of the call that launched task
// whose text has all of has.
func (r *events) result(task string, has ...string) func(provider.Event) bool {
	return func(e provider.Event) bool {
		if e.Kind != provider.EventToolResult || !e.Background || e.CallID != r.call(task) {
			return false
		}
		for _, h := range has {
			if !strings.Contains(e.Summary, h) {
				return false
			}
		}
		return true
	}
}

// call is the id of the call that launched task ("" before it is seen).
func (r *events) call(task string) string {
	for _, e := range r.all {
		if e.Kind == provider.EventToolResult && strings.Contains(e.Summary, "(Antigravity task conv/"+task+").") {
			return e.CallID
		}
	}
	return ""
}

func said(text string) func(provider.Event) bool {
	return func(e provider.Event) bool { return e.Kind == provider.EventAgentMessage && e.Text == text }
}

func fastBackground(t *testing.T) {
	poll, quiet, linger := bgPoll, wakeQuiet, bgLinger
	bgPoll, wakeQuiet, bgLinger = 30*time.Millisecond, 100*time.Millisecond, 300*time.Millisecond
	t.Cleanup(func() { bgPoll, wakeQuiet, bgLinger = poll, quiet, linger })
}

func (f *fixture) startAntigravity(prompt string) provider.Session {
	f.t.Helper()
	f.p.Agent = antigravityAgent("darwin")
	s, err := f.p.Start(context.Background(), provider.StartSpec{WorkDir: f.dir, TempDir: f.t.TempDir(), Prompt: prompt,
		Mode: "danger-full-access", Env: f.env, Gate: f.gate.gate})
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { s.Close() })
	return s
}

// 工单 #158 (hermesh 3580ee41): Antigravity's auto-backgrounded commands
// show as running, their logs as they grow, and their ends as Antigravity
// records them, through wakes that only say a task still runs, kill one or
// launch the next; never inferred from a wake.
func TestAntigravityBackgroundTasks(t *testing.T) {
	fastBackground(t)
	f := newFixture(t)
	gate := func(name string) {
		if err := os.WriteFile(filepath.Join(f.state, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := f.startAntigravity(strings.Join([]string{
		"shell echo instant",
		"bg t10 go test -race ./...",
		"bg daemon pnpm dev",
		"say Tests running in the background.",
		"wake w1 still t10 ;; say Still waiting on the race suite.",
		"wake w2 done t10 1 --- FAIL: TestRace ;; kill daemon ;; bg t11 go test -race ./x/... ;; say Fixed and re-running.",
		"wake w3 done t11 0 --- PASS: TestRaceFixed ;; say All passed.",
	}, "\n"))
	r := &events{t: t, s: s}
	r.until("the first turn", func(e provider.Event) bool { return e.Kind == provider.EventTurnCompleted })
	var sync, launched int
	for _, e := range of(r.all, provider.EventToolResult) {
		switch {
		case !e.Background && strings.Contains(e.Summary, "instant"):
			sync++
		case e.Background:
			launched++
			if !strings.HasPrefix(e.Summary, "[Aide] Running in the background (Antigravity task conv/") || strings.Contains(e.Summary, "YOU MUST") || e.IsError {
				t.Fatalf("launch: %+v", e)
			}
		}
	}
	if sync != 1 || launched != 2 || !s.(*session).Background() {
		t.Fatalf("a command that ended is no background task; two were launched: %+v", r.all)
	}

	// Its log, as it grows.
	log10 := filepath.Join(f.state, "brain", ".system_generated", "tasks", "t10.log")
	if err := os.WriteFile(log10, []byte("started t10\n正在执行并发压测 50/100...\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.until("the log as it grows", r.result("t10", "Running in the background", "50/100"))

	// A wake that says it still runs ends nothing.
	gate("w1")
	r.until("the wake's reply", said("Still waiting on the race suite."))
	r.quiet("still running is no end", 300*time.Millisecond, r.result("t10", "finished"))

	// A wake that ends t10 (exit code 1), kills the daemon and launches t11:
	// t11 runs on.
	gate("w2")
	failed := r.until("t10's end", r.result("t10", "finished with exit code 1", "--- FAIL: TestRace"))
	if !failed.IsError {
		t.Fatalf("exit code 1 is an error: %+v", failed)
	}
	r.until("the daemon stopped", r.result("daemon", "The agent stopped this background task"))
	r.until("t11 launched", r.result("t11", "Running in the background"))
	r.until("the wake's reply", said("Fixed and re-running."))
	r.quiet("t11 launched by the wake still runs", 300*time.Millisecond, r.result("t11", "finished"))

	gate("w3")
	passed := r.until("t11's end", r.result("t11", "finished with exit code 0", "--- PASS: TestRaceFixed"))
	if passed.IsError {
		t.Fatalf("exit code 0: %+v", passed)
	}
	r.until("the wake's reply", said("All passed."))
	if !s.(*session).Background() {
		t.Fatal("the agent was at work by itself a moment ago")
	}
	for deadline := time.Now().Add(5 * time.Second); s.(*session).Background(); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("nothing runs in the background now")
		}
	}
	if failed.CallID == passed.CallID || r.call("t10") == r.call("daemon") {
		t.Fatalf("each task is its own call: %+v", r.all)
	}

	// The next turn: wakes are not turns.
	r.all = nil
	if err := s.Send(context.Background(), provider.Input{Text: "history"}); err != nil {
		t.Fatal(err)
	}
	done := r.until("the next turn", func(e provider.Event) bool { return e.Kind == provider.EventTurnCompleted })
	if done.IsError || done.Text != "history: 2 turns" {
		t.Fatalf("%+v", done)
	}
}

// A task still running when the agent's process goes is said to be no
// longer followed, not left shown running.
func TestAntigravityBackgroundTaskOutlivedByProcess(t *testing.T) {
	fastBackground(t)
	f := newFixture(t)
	s := f.startAntigravity("bg dev pnpm dev")
	r := &events{t: t, s: s}
	r.until("the turn", func(e provider.Event) bool { return e.Kind == provider.EventTurnCompleted })
	go s.Close()
	e := r.until("the task's end", r.result("dev", "no longer followed"))
	if !e.IsError {
		t.Fatalf("%+v", e)
	}
}

// Other agents' output that reads like Antigravity's is only output.
func TestBackgroundTextFromAnotherAgent(t *testing.T) {
	f := newFixture(t)
	s, err := f.start("bg t1 make", "", "")
	if err != nil {
		t.Fatal(err)
	}
	evs, _ := collect(t, s)
	rs := of(evs, provider.EventToolResult)
	if len(rs) != 1 || rs[0].Background || !strings.HasPrefix(rs[0].Summary, "Tool is running as a background task") {
		t.Fatalf("%+v", rs)
	}
}

// Aide reads only Antigravity's own task files: a log path the agent names
// elsewhere, or a symlink out of its folders, is not read.
func TestBackgroundFilesStayAntigravitys(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	tasks := filepath.Join(dir, ".system_generated", "tasks")
	os.MkdirAll(tasks, 0o755)
	log := filepath.Join(tasks, "a.log")
	os.WriteFile(log, []byte("ok\n"), 0o644)
	if p := systemFile(fileURI(log), "tasks", ".log"); p != log {
		t.Fatalf("its log: %q", p)
	}
	for _, p := range []string{filepath.Join(dir, "a.log"), filepath.Join(dir, ".system_generated", "a.log"), filepath.Join(tasks, "a.txt")} {
		if got := systemFile(fileURI(p), "tasks", ".log"); got != "" {
			t.Fatalf("%s: %q", p, got)
		}
	}
	if got := systemFile("https://example.com/.system_generated/tasks/a.log", "tasks", ".log"); got != "" {
		t.Fatal(got)
	}
	if tail, err := readTail(log, 2); err != nil || tail != "k\n" {
		t.Fatalf("%q %v", tail, err)
	}
	secret := filepath.Join(dir, "secret.txt")
	os.WriteFile(secret, []byte("secret"), 0o600)
	link := filepath.Join(tasks, "link.log")
	if err := os.Symlink(secret, link); err != nil {
		t.Skip("symlinks:", err)
	}
	if tail, err := readTail(link, 100); err == nil {
		t.Fatalf("read through a symlink out of the task folder: %q", tail)
	}
}
