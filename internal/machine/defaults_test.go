package machine

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"connectrpc.com/connect"
	identityv1 "github.com/hermeshai/hermesh/gen/go/hermesh/identity/v1"
	machinev1 "github.com/hermeshai/hermesh/gen/go/hermesh/machine/v1"
	sessionv1 "github.com/hermeshai/hermesh/gen/go/hermesh/session/v1"
	"github.com/hermeshai/hermesh/internal/provider"
)

// TestAgentDefaults: a Codex session started without a model runs with the
// machine's defaults; one naming a model keeps it, with the default effort
// and speed; a session's process started again keeps what the session
// started with, whatever the defaults are by then. Only the machine's
// coding agents take defaults, and only Codex a speed.
func TestAgentDefaults(t *testing.T) {
	codex := &recorder{}
	h := newHarnessWith(t, DefaultLimits(), map[string]provider.Provider{"codex": codex})
	ctx := context.Background()
	set := func(d *identityv1.AgentDefaults) error {
		_, err := h.n.execAdmin(ctx, &identityv1.AdminCommand{Command: &identityv1.AdminCommand_SetAgentDefaults{SetAgentDefaults: d}})
		return err
	}
	start := func(model string) string {
		t.Helper()
		s, err := h.s.Start(ctx, h.p, &sessionv1.StartSessionRequest{ProjectId: "demo", Agent: "codex", Prompt: "say one", Model: model})
		if err != nil {
			t.Fatal(err)
		}
		h.wait(s.SessionId, idle)
		return s.SessionId
	}
	want := func(i int, model, effort string, speed identityv1.AgentSpeed) {
		t.Helper()
		if s := codex.spec(i); s.Model != model || s.Effort != effort || s.Speed != speed {
			t.Fatalf("start %d ran with %q/%q/%v, want %q/%q/%v", i, s.Model, s.Effort, s.Speed, model, effort, speed)
		}
	}

	start("")
	want(0, "", "", identityv1.AgentSpeed_AGENT_SPEED_UNSPECIFIED) // the agent's own configuration

	std := identityv1.AgentSpeed_AGENT_SPEED_STANDARD
	if err := set(&identityv1.AgentDefaults{Agent: "codex", Model: "gpt-5.6-luna", Effort: "low", Speed: std}); err != nil {
		t.Fatal(err)
	}
	for _, a := range h.n.Status().Agents {
		if a.Name == "codex" && a.Defaults.GetModel() != "gpt-5.6-luna" {
			t.Fatalf("status: %v", a)
		}
	}
	id := start("")
	want(1, "gpt-5.6-luna", "low", std)
	start("gpt-6-astra")
	want(2, "gpt-6-astra", "low", std)

	if err := set(&identityv1.AgentDefaults{Agent: "codex"}); err != nil {
		t.Fatal(err)
	}
	h.stopProcess(id)
	h.send(id, "again")
	h.wait(id, idle)
	if codex.spec(-1).ResumeRef == "" {
		t.Fatal("not resumed")
	}
	want(-1, "gpt-5.6-luna", "low", std)

	for _, d := range []*identityv1.AgentDefaults{
		{Agent: "nosuch", Model: "m"},
		{Agent: "claude", Speed: identityv1.AgentSpeed_AGENT_SPEED_FAST},
		{Agent: "codex", Model: "two words"},
		{Agent: "codex", Effort: "High"},
		{Agent: "codex", Speed: 9},
	} {
		if set(d) == nil {
			t.Errorf("%v accepted", d)
		}
	}
}

// TestSessionModel: a session shows the model the agent reported over the
// one Hermesh passed it (an alias), and the effort Hermesh passed over the
// agent's own; both are kept across a restart.
func TestSessionModel(t *testing.T) {
	st, err := OpenStore(filepath.Join(t.TempDir(), "hermeshd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := &Sessions{machineID: "m"}
	for i, c := range []struct {
		row                   sessionRow
		wantModel, wantEffort string
	}{
		{sessionRow{Model: "opus", Effort: "high", AgentModel: "claude-opus-5-5"}, "claude-opus-5-5", "high"},
		{sessionRow{AgentModel: "gpt-6-astra", AgentEffort: "xhigh"}, "gpt-6-astra", "xhigh"},
		{sessionRow{Effort: "low", AgentModel: "gpt-6-astra", AgentEffort: "xhigh"}, "gpt-6-astra", "low"},
		{sessionRow{Model: "sonnet"}, "sonnet", ""}, // not started yet
		{sessionRow{}, "", ""},
	} {
		r := c.row
		r.ID, r.Progress = fmt.Sprint("s", i), &sessionv1.SessionProgress{}
		if err := putSession(st.db, &r); err != nil {
			t.Fatal(err)
		}
		got, err := st.Session(r.ID)
		if err != nil {
			t.Fatal(err)
		}
		if p := s.toProto(got); p.Model != c.wantModel || p.Effort != c.wantEffort {
			t.Errorf("%+v shows %q/%q, want %q/%q", c.row, p.Model, p.Effort, c.wantModel, c.wantEffort)
		}
	}
}

// catalog is an agent that lists every model it takes, as Codex does.
type catalog struct{ recorder }

func (c *catalog) Discover(ctx context.Context) provider.Info {
	i := c.recorder.Discover(ctx)
	i.Models = []provider.Model{{ID: "gpt-big", Efforts: []string{"low", "high", "xhigh"}}, {ID: "gpt-small", Efforts: []string{"low", "high"}}}
	i.AllModels = true
	return i
}

// TestModelAndEffort: a session naming a model the agent does not list, or
// an effort its model does not take, is refused before it starts, with the
// ones there are; listed ones start. Without a model, any listed model's
// effort is taken. An agent that lists only some of its models (Claude
// Code's aliases) takes any model; one that lists none takes no effort.
func TestModelAndEffort(t *testing.T) {
	codex := &catalog{}
	h := newHarnessWith(t, DefaultLimits(), map[string]provider.Provider{"codex": codex})
	ctx := context.Background()
	start := func(agent, model, effort string) (*sessionv1.Session, error) {
		return h.s.Start(ctx, h.p, &sessionv1.StartSessionRequest{ProjectId: "demo", Agent: agent, Prompt: "say one", Model: model, Effort: effort})
	}
	for _, c := range []struct{ agent, model, effort, refusal string }{
		{"codex", "aurora", "", `codex on this machine has no model "aurora" (one of: gpt-big, gpt-small)`},
		{"codex", "gpt-small", "xhigh", `codex's model gpt-small has no reasoning effort "xhigh" (one of: low, high)`},
		{"codex", "", "max", `codex has no reasoning effort "max" (one of: low, high, xhigh)`},
		{"fake", "", "high", "fake on this machine takes no reasoning effort"},
	} {
		if _, err := start(c.agent, c.model, c.effort); connect.CodeOf(err) != connect.CodeInvalidArgument || !strings.Contains(err.Error(), c.refusal) {
			t.Fatalf("%+v: %v", c, err)
		}
	}
	h.s.mu.Lock()
	made := len(h.s.live)
	h.s.mu.Unlock()
	if made != 0 {
		t.Fatal("a session was made for a refused model or effort")
	}
	for _, c := range []struct{ agent, model, effort string }{{"codex", "gpt-small", "high"}, {"codex", "", "xhigh"}, {"fake", "aurora", ""}} {
		s, err := start(c.agent, c.model, c.effort)
		if err != nil {
			t.Fatalf("%+v: %v", c, err)
		}
		h.wait(s.SessionId, idle)
	}
	for i, effort := range []string{"high", "xhigh"} {
		if s := codex.spec(i); s.Effort != effort {
			t.Fatalf("start %d ran with effort %q, want %q", i, s.Effort, effort)
		}
	}
}

// offering is an ACP agent that says what its new sessions offer.
type offering struct {
	recorder
	mu     sync.Mutex
	offers provider.Offers
}

func (o *offering) Start(ctx context.Context, spec provider.StartSpec) (provider.Session, error) {
	s, err := o.recorder.Start(ctx, spec)
	if err != nil || spec.ResumeRef != "" {
		return s, err
	}
	o.mu.Lock()
	offers := o.offers
	o.mu.Unlock()
	events := make(chan provider.Event, 64)
	events <- provider.Event{Kind: provider.EventRef, Ref: spec.NewRef, Offers: &offers}
	go func() {
		for e := range s.Events() {
			events <- e
		}
		close(events)
	}()
	return &withEvents{s, events}, nil
}

type withEvents struct {
	provider.Session
	events chan provider.Event
}

func (w *withEvents) Events() <-chan provider.Event { return w.events }

// TestACPOffers: an ACP agent tells what it offers (models, their reasoning
// efforts, modes) only in a session: its first new session here shows them
// in the machine's status, kept across restarts, with the efforts of each
// model it ever ran; then sessions and defaults can name its modes, and one
// it does not offer is refused before a session starts.
func TestACPOffers(t *testing.T) {
	acp := &offering{offers: provider.Offers{Models: []provider.Model{{ID: `["p","a"]`, Name: "A (p)", Efforts: []string{"low"}}, {ID: `["p","b"]`, Name: "B (p)"}},
		Modes: []provider.Mode{{ID: "ask", Name: "Ask"}, {ID: "code", Name: "Code"}}, Own: provider.Settings{Model: `["p","a"]`, Mode: "ask"}}}
	h := newHarnessWith(t, DefaultLimits(), map[string]provider.Provider{"kimi": acp})
	ctx := context.Background()
	start := func(model, effort, mode string) error {
		s, err := h.s.Start(ctx, h.p, &sessionv1.StartSessionRequest{ProjectId: "demo", Agent: "kimi", Prompt: "say one", Model: model, Effort: effort, Mode: mode})
		if err != nil {
			return err
		}
		h.wait(s.SessionId, idle)
		return nil
	}
	set := func(d *identityv1.AgentDefaults) error {
		_, err := h.n.execAdmin(ctx, &identityv1.AdminCommand{Command: &identityv1.AdminCommand_SetAgentDefaults{SetAgentDefaults: d}})
		return err
	}
	status := func() *machinev1.AgentStatus {
		for _, a := range h.n.Status().Agents {
			if a.Name == "kimi" {
				return a
			}
		}
		return nil
	}
	if err := start("", "", "code"); err == nil || !strings.Contains(err.Error(), "kimi on this machine offers no session modes") {
		t.Fatalf("a mode before any session: %v", err)
	}
	if err := set(&identityv1.AgentDefaults{Agent: "kimi", Mode: "code"}); err == nil {
		t.Fatal("a default mode before any session")
	}
	if err := start("", "", ""); err != nil {
		t.Fatal(err)
	}
	acp.mu.Lock()
	acp.offers.Models = []provider.Model{{ID: `["p","a"]`, Name: "A (p)"}, {ID: `["p","b"]`, Name: "B (p)", Efforts: []string{"high"}}}
	acp.mu.Unlock()
	if err := start(`["p","b"]`, "high", ""); err != nil {
		t.Fatal(err)
	}
	if a := status(); len(a.Models) != 2 || strings.Join(a.Models[0].Efforts, ",") != "low" || strings.Join(a.Models[1].Efforts, ",") != "high" || a.Models[1].Name != "B (p)" ||
		len(a.Modes) != 2 || a.Modes[1].Id != "code" || a.Modes[1].Name != "Code" || a.Own.GetMode() != "ask" || a.Own.GetModel() != `["p","a"]` {
		t.Fatalf("status: %v", a)
	}
	if err := start("", "", "yolo"); err == nil || !strings.Contains(err.Error(), `kimi on this machine has no mode "yolo" (one of: ask, code)`) {
		t.Fatalf("a mode it does not offer: %v", err)
	}
	if err := start("", "", "code"); err != nil || acp.spec(-1).Mode != "code" {
		t.Fatalf("mode: %v %+v", err, acp.spec(-1))
	}
	if err := set(&identityv1.AgentDefaults{Agent: "kimi", Model: `["p","b"]`, Effort: "high", Mode: "code"}); err != nil {
		t.Fatal(err)
	}
	if a := status(); a.Defaults.GetMode() != "code" {
		t.Fatalf("defaults: %v", a.Defaults)
	}
	if err := start("", "", ""); err != nil {
		t.Fatal(err)
	}
	if s := acp.spec(-1); s.Model != `["p","b"]` || s.Effort != "high" || s.Mode != "code" {
		t.Fatalf("with the defaults: %+v", s)
	}
	h.n.mu.Lock()
	h.n.offers = map[string]provider.Offers{}
	h.n.mu.Unlock()
	h.n.loadDefaults() // as when hermeshd starts again
	if o := h.n.offers["kimi"]; len(o.Models) != 2 || strings.Join(o.Models[0].Efforts, ",") != "low" || len(o.Modes) != 2 {
		t.Fatalf("stored: %+v", o)
	}
}

// presets is an ACP agent that tells its modes without a session (DSH's
// permission presets), its models only in one.
type presets struct{ offering }

func (p *presets) Discover(ctx context.Context) provider.Info {
	i := p.offering.Discover(ctx)
	i.Modes, i.Own.Mode = []provider.Mode{{ID: "read-only", Name: "read-only"}, {ID: "workspace-write", Name: "workspace-write"}}, "workspace-write"
	return i
}

// TestModesBeforeASession: an agent's modes it tells without a session can
// be named before its first; once a session offered its models, the
// machine shows both.
func TestModesBeforeASession(t *testing.T) {
	dsh := &presets{offering{offers: provider.Offers{Models: []provider.Model{{ID: `["p","a"]`, Name: "A (p)"}}, Own: provider.Settings{Model: `["p","a"]`},
		Modes: []provider.Mode{{ID: "read-only", Name: "read-only"}, {ID: "workspace-write", Name: "workspace-write"}}}}}
	h := newHarnessWith(t, DefaultLimits(), map[string]provider.Provider{"deepseek": dsh})
	ctx := context.Background()
	status := func() *machinev1.AgentStatus {
		<-h.n.CheckAgents(false, AgentsOnce)
		for _, a := range h.n.Status().Agents {
			if a.Name == "deepseek" {
				return a
			}
		}
		return nil
	}
	if a := status(); len(a.Modes) != 2 || len(a.Models) != 0 || a.Own.GetMode() != "workspace-write" {
		t.Fatalf("before a session: %v", a)
	}
	s, err := h.s.Start(ctx, h.p, &sessionv1.StartSessionRequest{ProjectId: "demo", Agent: "deepseek", Prompt: "say one", Mode: "read-only"})
	if err != nil {
		t.Fatal(err)
	}
	h.wait(s.SessionId, idle)
	if dsh.spec(-1).Mode != "read-only" {
		t.Fatalf("mode: %+v", dsh.spec(-1))
	}
	h.n.mu.Lock()
	i := h.n.withOffers("deepseek", dsh.Discover(ctx)) // checked again
	h.n.mu.Unlock()
	if len(i.Modes) != 2 || len(i.Models) != 1 || i.Own.Model != `["p","a"]` || i.Own.Mode != "workspace-write" || !i.SomeEfforts {
		t.Fatalf("after a session: %+v", i)
	}
	if a := status(); len(a.Modes) != 2 || len(a.Models) != 1 || a.Own.GetModel() != `["p","a"]` {
		t.Fatalf("status after a session: %v", a)
	}
}
