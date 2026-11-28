package machine

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/samir2015dzh/aide/internal/provider"
)

// lister is a coding agent that, like Claude Code and Codex, tells its
// models only when started for them: asked counts how often it was.
type lister struct {
	version atomic.Value // string
	asked   atomic.Int32
	model   atomic.Value // string: the model it lists when asked
}

func (l *lister) Name() string { return "lister" }

func (l *lister) Discover(ctx context.Context) provider.Info {
	i := provider.Info{Name: "lister", Available: true, Version: l.version.Load().(string), LoggedIn: true}
	if provider.IsOnPurpose(ctx) {
		l.asked.Add(1)
		i.Models, i.AllModels = []provider.Model{{ID: l.model.Load().(string)}}, true
	} else {
		i.ModelsSkipped = true
	}
	return i
}

func (l *lister) Start(context.Context, provider.StartSpec) (provider.Session, error) {
	panic("not started in these tests")
}

// TestPassiveChecks (工单 #112): an agent is started for its models once —
// its first check — and then only on the owner's Check Again or for a new
// version of it; every other check (a page opening, a poll, a session
// about to start) starts nothing and shows what that found, also after
// aided starts again.
func TestPassiveChecks(t *testing.T) {
	l := &lister{}
	l.version.Store("1.0.0")
	l.model.Store("m-1")
	dir := t.TempDir()
	open := func() *Machine {
		n, err := Open(Config{DataDir: dir, Providers: map[string]provider.Provider{"lister": l}})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	models := func(n *Machine) string {
		n.mu.Lock()
		defer n.mu.Unlock()
		if ms := n.infos["lister"].Models; len(ms) == 1 {
			return ms[0].ID
		}
		return ""
	}
	n := open()
	<-n.CheckAgents(true, 0)
	if l.asked.Load() != 1 || models(n) != "m-1" {
		t.Fatalf("first check: asked %d, models %q", l.asked.Load(), models(n))
	}
	l.model.Store("m-2") // what it would list now
	for range 3 {
		<-n.CheckAgents(true, 0)
		n.Local().CheckAgents(false)
		<-n.CheckAgents(false, AgentsOnce)
	}
	if l.asked.Load() != 1 || models(n) != "m-1" {
		t.Fatalf("passive checks started it: asked %d, models %q", l.asked.Load(), models(n))
	}
	<-n.CheckAgentsOnPurpose()
	if l.asked.Load() != 2 || models(n) != "m-2" {
		t.Fatalf("Check Again: asked %d, models %q", l.asked.Load(), models(n))
	}
	n.Close()

	// Kept across a restart: the next aided starts nothing for it either.
	n = open()
	defer n.Close()
	<-n.CheckAgents(true, 0)
	if l.asked.Load() != 2 || models(n) != "m-2" {
		t.Fatalf("after a restart: asked %d, models %q", l.asked.Load(), models(n))
	}
	// A new version: asked once, then passive again.
	l.version.Store("1.1.0")
	l.model.Store("m-3")
	<-n.CheckAgents(true, 0)
	<-n.CheckAgents(true, 0)
	if l.asked.Load() != 3 || models(n) != "m-3" {
		t.Fatalf("a new version: asked %d, models %q", l.asked.Load(), models(n))
	}
}

// acpLike is an agent whose check cannot tell whether it is signed in, as
// an ACP agent's cannot.
type acpLike struct{}

func (acpLike) Name() string { return "acplike" }
func (acpLike) Discover(context.Context) provider.Info {
	return provider.Info{Name: "acplike", Available: true, Version: "1.0.0", LoginUnconfirmed: true}
}
func (acpLike) Start(context.Context, provider.StartSpec) (provider.Session, error) {
	panic("not started in these tests")
}

// TestSignInShownBySessions (工单 #112): an agent whose check cannot tell is
// unconfirmed, never signed in on a guess; a session that ran a turn makes
// it signed in, one that failed "not logged in" signed out, and that stands
// through later checks and a restart of aided.
func TestSignInShownBySessions(t *testing.T) {
	dir := t.TempDir()
	open := func() *Machine {
		n, err := Open(Config{DataDir: dir, Providers: map[string]provider.Provider{"acplike": acpLike{}}})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	state := func(n *Machine) (bool, bool) {
		<-n.CheckAgents(true, 0)
		n.mu.Lock()
		defer n.mu.Unlock()
		i := n.infos["acplike"]
		return i.LoggedIn, i.LoginUnconfirmed
	}
	n := open()
	if in, unconfirmed := state(n); in || !unconfirmed {
		t.Fatalf("before any session: in %v, unconfirmed %v", in, unconfirmed)
	}
	n.agentSignedIn("acplike", true, "")
	if in, unconfirmed := state(n); !in || unconfirmed {
		t.Fatalf("after a turn ran: in %v, unconfirmed %v", in, unconfirmed)
	}
	n.Close()
	n = open()
	defer func() { n.Close() }() // the last one opened
	if in, unconfirmed := state(n); !in || unconfirmed {
		t.Fatalf("after a restart: in %v, unconfirmed %v", in, unconfirmed)
	}
	n.agentSignedIn("acplike", false, "Not logged in · Please run /login")
	if in, unconfirmed := state(n); in || unconfirmed {
		t.Fatalf("after a turn failed not logged in: in %v, unconfirmed %v", in, unconfirmed)
	}
	// What it rests on is shown (工单 #180): when, and what the run said; also after a restart.
	n.Close()
	n = open()
	state(n)
	n.mu.Lock()
	seen := signInSeenProto(n.infos["acplike"].SignInSeen) // as the machine's status carries it
	n.mu.Unlock()
	if seen == nil || seen.SignedIn || seen.Why != "Not logged in · Please run /login" || seen.At == nil {
		t.Fatalf("its evidence: %+v", seen)
	}
}

// TestBareSignOutIsNoEvidence (工单 #180): a bare "out" an earlier version
// kept says neither when nor why: it no longer keeps the agent signed out
// (and its test run unoffered); a bare "in" still stands.
func TestBareSignOutIsNoEvidence(t *testing.T) {
	dir := t.TempDir()
	open := func() *Machine {
		n, err := Open(Config{DataDir: dir, Providers: map[string]provider.Provider{"acplike": acpLike{}}})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	for _, c := range []struct {
		kept            string
		in, unconfirmed bool
	}{{"out", false, true}, {"in", true, false}} {
		n := open()
		if err := n.st.Set(kAgentSignIn+"acplike", []byte(c.kept)); err != nil {
			t.Fatal(err)
		}
		<-n.CheckAgents(true, 0)
		n.mu.Lock()
		i := n.infos["acplike"]
		n.mu.Unlock()
		n.Close()
		if i.LoggedIn != c.in || i.LoginUnconfirmed != c.unconfirmed {
			t.Fatalf("kept %q: in %v, unconfirmed %v", c.kept, i.LoggedIn, i.LoginUnconfirmed)
		}
	}
}

// TestConfirmedSignOutNotResurrectedByCache (工单 #111): when a provider's own
// check confirms the user signed out (LoggedIn=false, LoginUnconfirmed=false),
// stale agent_signin/ cache from an earlier session must not resurrect it.
func TestConfirmedSignOutNotResurrectedByCache(t *testing.T) {
	var signedIn atomic.Bool
	signedIn.Store(true)
	p := &toggleProvider{signedIn: &signedIn}
	n, err := Open(Config{DataDir: t.TempDir(), Providers: map[string]provider.Provider{"antigravity": p}})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	<-n.CheckAgents(true, 0)
	n.agentSignedIn("antigravity", true, "")
	signedIn.Store(false)
	<-n.CheckAgents(true, 0)
	n.mu.Lock()
	i := n.infos["antigravity"]
	n.mu.Unlock()
	if i.LoggedIn || i.LoginUnconfirmed {
		t.Fatalf("confirmed sign-out resurrected by cache: %+v", i)
	}
}

type toggleProvider struct{ signedIn *atomic.Bool }

func (toggleProvider) Name() string { return "antigravity" }
func (p *toggleProvider) Discover(context.Context) provider.Info {
	return provider.Info{Name: "antigravity", Available: true, Version: "1.0.0", LoggedIn: p.signedIn.Load()}
}
func (toggleProvider) Start(context.Context, provider.StartSpec) (provider.Session, error) {
	panic("not started in these tests")
}
