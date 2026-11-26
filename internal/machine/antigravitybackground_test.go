package machine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/provider"
	"github.com/samir2015dzh/aide/internal/provider/acp"
)

// antigravityHarness runs Antigravity's ACP (the scripted agent) through
// the sessions; state is the agent's folder (its background tasks' too).
func antigravityHarness(t *testing.T, lim Limits) (h *harness, state string) {
	bin := fakeACP(t)
	state = t.TempDir()
	t.Setenv("FAKE_ACP_STATE", state)
	prov := &acp.Provider{Agent: acp.Agent{Name: "antigravity", Command: "fakeacp", Args: []string{"acp"}}, Binary: bin}
	return newHarnessWith(t, lim, map[string]provider.Provider{"antigravity": prov}), state
}

func (h *harness) startAntigravity(prompt string) string {
	h.t.Helper()
	s, err := h.s.Start(context.Background(), h.p, &sessionv1.StartSessionRequest{ProjectId: "demo", Agent: "antigravity", Mode: "danger-full-access", Prompt: prompt})
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { _, _ = h.s.Archive(context.Background(), h.p, s.SessionId) })
	return s.SessionId
}

// agentRunning: the session's agent process is still kept.
func (h *harness) agentRunning(id string) bool {
	h.s.mu.Lock()
	l := h.s.live[id]
	h.s.mu.Unlock()
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.sess != nil
}

// bgResult is the latest result of the call that launched task.
func (h *harness) bgResult(id, task string) *sessionv1.ToolResult {
	var call string
	var last *sessionv1.ToolResult
	for _, e := range h.events(id) {
		r := e.GetToolResult()
		if r == nil {
			continue
		}
		if call == "" && strings.Contains(r.Summary, "(Antigravity task conv/"+task+").") {
			call = r.CallId
		}
		if call != "" && r.CallId == call {
			last = r
		}
	}
	return last
}

func (h *harness) waitBg(id, task, has string) *sessionv1.ToolResult {
	h.t.Helper()
	var r *sessionv1.ToolResult
	h.waitFor(id, task+" "+has, func(*sessionv1.SessionEvent) bool {
		r = h.bgResult(id, task)
		return r != nil && strings.Contains(r.Summary, has)
	})
	return r
}

// 工单 #158 (hermesh 3580ee41): through the session, an Antigravity
// background task shows running after its turn, its log as it grows, its
// end (exit code) when the agent wakes by itself, the next task the wake
// launched running on; the session stays idle, the agent process is kept
// while something runs, and the next turn goes on.
func TestAntigravityBackgroundTasksInSession(t *testing.T) {
	lim := DefaultLimits()
	lim.IdleProcessTTL = 100 * time.Millisecond
	h, state := antigravityHarness(t, lim)
	id := h.startAntigravity(strings.Join([]string{
		"bg e2e go test ./e2e/...",
		"say Running the E2E tests in the background.",
		"wake g1 done e2e 2 FAIL e2e/storage_test.go ;; bg e2e2 go test ./e2e/... ;; say Retrying with a short TMPDIR.",
		"wake g2 done e2e2 0 PASS ok e2e ;; say E2E passed.",
	}, "\n"))
	h.wait(id, idle)
	if r := h.bgResult(id, "e2e"); r == nil || r.IsError || strings.Contains(r.Summary, "YOU MUST") {
		t.Fatalf("launched: %v\n%s", r, h.dump(id))
	}
	time.Sleep(500 * time.Millisecond) // past IdleProcessTTL
	if !h.agentRunning(id) {
		t.Fatal("the agent process went while its background task runs")
	}

	log := filepath.Join(state, "brain", ".system_generated", "tasks", "e2e.log")
	if err := os.WriteFile(log, []byte("started e2e\nrunning TestStorage 1/3...\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.waitBg(id, "e2e", "running TestStorage 1/3...")

	os.WriteFile(filepath.Join(state, "g1"), nil, 0o644)
	h.said(id, "Retrying with a short TMPDIR.")
	if r := h.waitBg(id, "e2e", "finished with exit code 2"); !r.IsError || !strings.Contains(r.Summary, "FAIL e2e/storage_test.go") {
		t.Fatalf("e2e's end: %v", r)
	}
	h.waitBg(id, "e2e2", "Running in the background")
	time.Sleep(300 * time.Millisecond)
	if r := h.bgResult(id, "e2e2"); strings.Contains(r.Summary, "finished") || !h.agentRunning(id) {
		t.Fatalf("e2e2, launched by the wake, runs on: %v", r)
	}
	if st := h.lastStatus(id); st != idle {
		t.Fatalf("a wake is no turn: %v", st)
	}

	os.WriteFile(filepath.Join(state, "g2"), nil, 0o644)
	if r := h.waitBg(id, "e2e2", "finished with exit code 0"); r.IsError || !strings.Contains(r.Summary, "PASS ok e2e") {
		t.Fatalf("e2e2's end: %v", r)
	}
	h.said(id, "E2E passed.")

	h.send(id, "history")
	h.wait(id, idle)
	if got := h.lastTurn(id).Result; got != "history: 2 turns" {
		t.Fatalf("the next turn: %q", got)
	}
}

// A task still running when its idle agent process is let go at last is
// said to be no longer followed, though the session has let the process go.
func TestAntigravityBackgroundTaskOutlivedInSession(t *testing.T) {
	lim := DefaultLimits()
	lim.IdleProcessTTL, lim.BackgroundProcessTTL = 50*time.Millisecond, time.Second
	h, _ := antigravityHarness(t, lim)
	id := h.startAntigravity("bg dev pnpm dev")
	h.wait(id, idle)
	if r := h.waitBg(id, "dev", "no longer followed"); !r.IsError {
		t.Fatalf("%v", r)
	}
	if h.agentRunning(id) {
		t.Fatal("kept past BackgroundProcessTTL")
	}
}
