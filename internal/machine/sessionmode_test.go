package machine

import (
	"context"
	"os"
	"strings"
	"testing"

	"connectrpc.com/connect"

	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/provider"
	"github.com/samir2015dzh/aide/internal/provider/acp"
	"github.com/samir2015dzh/aide/internal/reason"
)

// 工单 #167: a mode changed while an Antigravity session lives decides its
// next tool call — Aide's own gate enforces it — and the session says it
// runs in it; one of DeepSeek Harness, whose preset is set as its process
// starts, is refused and says why.
func TestSetModeWhileItRuns(t *testing.T) {
	h, _ := antigravityHarness(t, Limits{})
	ctx := context.Background()
	s, err := h.s.Start(ctx, h.p, &sessionv1.StartSessionRequest{ProjectId: "demo", Agent: "antigravity", Mode: "workspace-write", Prompt: "shell echo one"})
	if err != nil {
		t.Fatal(err)
	}
	id := s.SessionId
	t.Cleanup(func() { _, _ = h.s.Archive(context.Background(), h.p, id) })
	h.wait(id, idle)
	// The last tool call's result: refused by the gate, the agent says "rejected".
	shell := func() *sessionv1.ToolResult {
		t.Helper()
		var last *sessionv1.ToolResult
		for _, e := range h.events(id) {
			if r := e.GetToolResult(); r != nil {
				last = r
			}
		}
		if last == nil {
			t.Fatal("no tool call")
		}
		return last
	}
	if r := shell(); !r.IsError {
		t.Fatalf("workspace-write let a shell command run: %v", r)
	}
	if !h.agentRunning(id) {
		t.Fatal("the agent's process went: the mode would only apply at its next start")
	}

	// The owner loosens it while it lives: the next call runs.
	got, err := h.s.SetMode(ctx, h.p, id, "danger-full-access")
	if err != nil || got.Mode != "danger-full-access" {
		t.Fatalf("set mode: %v %v", got.GetMode(), err)
	}
	h.send(id, "shell echo two")
	h.wait(id, idle)
	if r := shell(); r.IsError || !strings.Contains(r.Summary, "two") {
		t.Fatalf("danger-full-access refused the next call: %v", r)
	}
	// And tightens it again: the next one is refused.
	if _, err := h.s.SetMode(ctx, h.p, id, "read-only"); err != nil {
		t.Fatal(err)
	}
	h.send(id, "shell echo three")
	h.wait(id, idle)
	if r := shell(); !r.IsError || strings.Contains(r.Summary, "three") {
		t.Fatalf("read-only let a shell command run: %v", r)
	}
	if g, _ := h.s.Get(h.p, id); g.Mode != "read-only" {
		t.Fatalf("the session says it runs in %q", g.Mode)
	}
	// A mode the agent does not have is refused, naming the ones it has.
	if _, err := h.s.SetMode(ctx, h.p, id, "yolo"); connect.CodeOf(err) != connect.CodeInvalidArgument || !strings.Contains(err.Error(), "danger-full-access") {
		t.Fatalf("an unknown mode: %v", err)
	}
}

func TestSetModeDSHIsFixed(t *testing.T) {
	bin := fakeACP(t)
	t.Setenv("FAKE_ACP_STATE", t.TempDir())
	t.Setenv("FAKE_ACP_PRESETS", "")
	prov := &acp.Provider{Agent: acp.Agent{Name: "deepseek", Command: "fakeacp", Args: []string{"--profile", "acp"}, DSHPresets: true}, Binary: bin}
	h := newHarnessWith(t, Limits{}, map[string]provider.Provider{"deepseek": prov})
	ctx := context.Background()
	s, err := h.s.Start(ctx, h.p, &sessionv1.StartSessionRequest{ProjectId: "demo", Agent: "deepseek", Prompt: "say hi"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = h.s.Archive(context.Background(), h.p, s.SessionId) })
	h.wait(s.SessionId, idle)
	_, err = h.s.SetMode(ctx, h.p, s.SessionId, "read-only")
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || reason.Of(err) != "mode_fixed" {
		t.Fatalf("a DSH session's mode changed: %v", err)
	}
	// Archived: read-only, nothing to change.
	if _, err := h.s.Archive(ctx, h.p, s.SessionId); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.SetMode(ctx, h.p, s.SessionId, "read-only"); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("an archived session's mode: %v", err)
	}
}

// Another ACP agent with modes takes the change over ACP while it runs, as
// it took the one it started with (工单 #167).
func TestSetModeOverACP(t *testing.T) {
	bin := fakeACP(t)
	log := t.TempDir() + "/acp.log"
	t.Setenv("FAKE_ACP_STATE", t.TempDir())
	t.Setenv("FAKE_ACP_MODES", "ask,auto")
	t.Setenv("FAKE_ACP_LOG", log)
	prov := &acp.Provider{Agent: acp.Agent{Name: "gemini", Command: "fakeacp", Args: []string{"acp"}}, Binary: bin}
	h := newHarnessWith(t, Limits{}, map[string]provider.Provider{"gemini": prov})
	ctx := context.Background()
	s, err := h.s.Start(ctx, h.p, &sessionv1.StartSessionRequest{ProjectId: "demo", Agent: "gemini", Prompt: "say hi"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = h.s.Archive(context.Background(), h.p, s.SessionId) })
	h.wait(s.SessionId, idle)
	if got, err := h.s.SetMode(ctx, h.p, s.SessionId, "auto"); err != nil || got.Mode != "auto" {
		t.Fatalf("set mode: %v %v", got.GetMode(), err)
	}
	if b, _ := os.ReadFile(log); !strings.Contains(string(b), "session/set_mode") {
		t.Fatalf("the agent was not told: %q", b)
	}
}
