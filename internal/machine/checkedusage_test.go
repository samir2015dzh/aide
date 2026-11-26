package machine

import (
	"context"
	"sync"
	"testing"
	"time"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	"github.com/samir2015dzh/aide/internal/provider"
	"github.com/samir2015dzh/aide/internal/provider/claude"
)

// askingAgent is a coding agent whose check answers its plan usage when the
// check may ask it (provider.AskUsage), and says whether it was.
type askingAgent struct {
	name  string
	usage provider.PlanUsage
	mu    sync.Mutex
	asked []bool
}

func (a *askingAgent) Name() string { return a.name }
func (a *askingAgent) Discover(ctx context.Context) provider.Info {
	a.mu.Lock()
	a.asked = append(a.asked, provider.AsksUsage(ctx) && provider.IsOnPurpose(ctx))
	a.mu.Unlock()
	i := provider.Info{Name: a.name, Available: true, LoggedIn: true, Version: "1.0.0"}
	if provider.AsksUsage(ctx) && provider.IsOnPurpose(ctx) {
		u := a.usage
		i.PlanUsage = &u
	}
	return i
}
func (a *askingAgent) Start(context.Context, provider.StartSpec) (provider.Session, error) {
	return nil, nil
}
func (a *askingAgent) wasAsked() []bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]bool(nil), a.asked...)
}

func agentRow(st *machinev1.MachineStatus, name string) *machinev1.AgentStatus {
	for _, a := range st.Agents {
		if a.Name == name {
			return a
		}
	}
	return nil
}

// TestCheckedClaudeUsage (工单 #143): what Claude Code's check answered of
// its plan usage is taken as 工单 #86's reading, not kept beside it: the
// status says it with its source, and the chain's next refresh waits its
// quarter of an hour from it (the endpoint is not asked). An answer older
// than the reading held, or one without windows, changes nothing.
func TestCheckedClaudeUsage(t *testing.T) {
	n := openTestMachine(t)
	f := &fakeUsage{out: []claude.Usage{{Windows: []claude.Window{{Key: "five_hour", Utilization: 1}}, FetchedAt: time.Now()}}}
	n.mu.Lock()
	n.usageSrc = f
	n.mu.Unlock()
	at := time.Now()
	n.noteCheckedUsage("claude", provider.Info{PlanUsage: &provider.PlanUsage{Plan: "max", At: at,
		Windows: []provider.UsageWindow{{Key: "five_hour", Utilization: 39, ResetsAt: at.Add(time.Hour)}, {Key: "seven_day", Utilization: 64}}}})
	u := claudeAgent(n.Status()).GetUsage()
	if u == nil || u.Source != "claude_code_check" || u.Plan != "max" || len(u.Windows) != 2 || u.Windows[0].Utilization != 39 || u.Stale {
		t.Fatalf("usage = %+v", u)
	}
	time.Sleep(20 * time.Millisecond)
	if c := f.count(); c != 0 {
		t.Fatalf("the endpoint asked %d times past a fresh reading", c)
	}
	n.noteCheckedUsage("claude", provider.Info{PlanUsage: &provider.PlanUsage{At: at.Add(-time.Minute), Windows: []provider.UsageWindow{{Key: "five_hour", Utilization: 5}}}})
	n.noteCheckedUsage("claude", provider.Info{PlanUsage: &provider.PlanUsage{At: time.Now(), Unavailable: provider.UsageNone}})
	if u := claudeAgent(n.Status()).GetUsage(); u.Windows[0].Utilization != 39 || u.Unavailable != "" {
		t.Fatalf("changed by an older or empty answer: %+v", u)
	}
}

// TestCheckedAgentUsage (工单 #143): Codex's plan usage is what its checks
// answered, in the same message: unset until one with windows; a failed ask
// keeps the last, stale; one with no window clears it; kept across a restart,
// stale until asked again.
func TestCheckedAgentUsage(t *testing.T) {
	dir := t.TempDir()
	codex := &askingAgent{name: "codex", usage: provider.PlanUsage{Plan: "pro", At: time.Now(), Windows: []provider.UsageWindow{{Key: "seven_day", Utilization: 61}}}}
	open := func() *Machine {
		n, err := Open(Config{DataDir: dir, ClaudeUsage: true, Providers: map[string]provider.Provider{"codex": codex}})
		if err != nil {
			t.Fatal(err)
		}
		n.mu.Lock()
		n.usageSrc, n.usageCLI, n.usageCache = &fakeUsage{}, nil, nil // nothing of this computer's is read
		n.mu.Unlock()
		return n
	}
	n := open()
	n.noteCheckedUsage("codex", provider.Info{PlanUsage: &provider.PlanUsage{Unavailable: provider.UsageFailed}})
	n.Discover(context.Background()) // passive, its first: on purpose once, and asked
	if got := codex.wasAsked(); len(got) != 1 || !got[0] {
		t.Fatalf("asked: %v", got)
	}
	u := agentRow(n.Status(), "codex").GetUsage()
	if u == nil || u.Source != "codex_check" || u.Plan != "pro" || len(u.Windows) != 1 || u.Windows[0].Key != "seven_day" || u.Stale {
		t.Fatalf("usage = %+v", u)
	}
	n.Discover(context.Background()) // passive, a version asked before: not asked
	if got := codex.wasAsked(); len(got) != 2 || got[1] {
		t.Fatalf("asked passively: %v", got)
	}
	n.noteCheckedUsage("codex", provider.Info{PlanUsage: &provider.PlanUsage{Unavailable: provider.UsageFailed, Detail: "codex account/rateLimits/read: timed out"}})
	if u := agentRow(n.Status(), "codex").GetUsage(); !u.Stale || u.Windows[0].Utilization != 61 || u.Detail == "" {
		t.Fatalf("after a failed ask = %+v", u)
	}
	n.Close()

	n = open()
	if u := n.agentUsage["codex"]; u == nil || !u.Stale || u.Windows[0].Utilization != 61 {
		t.Fatalf("kept across a restart = %+v", u)
	}
	n.noteCheckedUsage("codex", provider.Info{PlanUsage: &provider.PlanUsage{Unavailable: provider.UsageNone}})
	n.Discover(context.Background())
	if u := agentRow(n.Status(), "codex").GetUsage(); u != nil {
		t.Fatalf("after an answer with no window = %+v", u)
	}
	n.Close()
	n = open()
	defer n.Close() // Windows removes no file still open
	if u := n.agentUsage["codex"]; u != nil {
		t.Fatalf("a cleared reading came back = %+v", u)
	}
}

// TestCheckAsksUsageOnlyWhenRead: a machine that does not read plan usage
// (aided's switch off) does not ask an agent's check for it.
func TestCheckAsksUsageOnlyWhenRead(t *testing.T) {
	codex := &askingAgent{name: "codex"}
	n, err := Open(Config{DataDir: t.TempDir(), Providers: map[string]provider.Provider{"codex": codex}})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	<-n.CheckAgentsOnPurpose()
	if got := codex.wasAsked(); len(got) != 1 || got[0] {
		t.Fatalf("asked: %v", got)
	}
	if u := agentRow(n.Status(), "codex").GetUsage(); u != nil {
		t.Fatalf("usage = %+v", u)
	}
}
