package machine

import (
	"context"
	"testing"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	"github.com/samir2015dzh/aide/internal/provider"
)

// After the owner's rescan, a hidden agent shows again only on live evidence
// that something changed: it runs now where it did not when hidden (a
// launcher left behind, then the agent installed), or it runs another
// program. One the owner hid while it ran stays hidden while it runs the same.
func TestShowsAgain(t *testing.T) {
	cases := []struct {
		name string
		h    hiddenAgent
		now  provider.Info
		want bool
	}{
		{"broken when hidden, runs now", hiddenAgent{Binary: "/bin/copilot", Available: false}, provider.Info{Available: true, Binary: "/bin/copilot"}, true},
		{"ran when hidden, runs the same", hiddenAgent{Binary: "/bin/codex", Available: true}, provider.Info{Available: true, Binary: "/bin/codex"}, false},
		{"ran when hidden, another program now", hiddenAgent{Binary: "/old/codex", Available: true}, provider.Info{Available: true, Binary: "/new/codex"}, true},
		{"still broken", hiddenAgent{Binary: "/bin/copilot", Available: false}, provider.Info{Available: false, Binary: "/bin/copilot", Problem: provider.ProblemFailed}, false},
		{"gone", hiddenAgent{Binary: "/bin/copilot", Available: false}, provider.Info{Problem: provider.ProblemNotFound}, false},
	}
	for _, c := range cases {
		if got := showsAgain(c.h, c.now); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// The owner hides an agent the machine lists: its status says so, a routine
// look leaves it hidden, it stays hidden across a restart, and it shows again
// when the owner shows it, or rescans and it then runs where it did not.
// An agent the machine doesn't know can't be hidden.
func TestHideAgent(t *testing.T) {
	h := newHarnessWith(t, DefaultLimits(), map[string]provider.Provider{"codex": &recorder{}})
	n := h.n
	ctx := context.Background()
	set := func(agent string, hidden bool) error {
		_, err := n.execAdmin(ctx, &identityv1.AdminCommand{Command: &identityv1.AdminCommand_SetAgentHidden{SetAgentHidden: &identityv1.SetAgentHidden{Agent: agent, Hidden: hidden}}})
		return err
	}
	hidden := func() bool {
		t.Helper()
		for _, a := range n.Status().Agents {
			if a.Name == "codex" {
				return a.Hidden
			}
		}
		t.Fatal("codex not listed")
		return false
	}
	found := func(i provider.Info) {
		n.mu.Lock()
		n.infos["codex"] = i
		n.mu.Unlock()
	}

	// A launcher with no agent behind it, as the machine last found it.
	found(provider.Info{Name: "codex", Binary: "/bin/codex", Problem: provider.ProblemFailed})
	if hidden() {
		t.Fatal("hidden before the owner hid it")
	}
	if err := set("codex", true); err != nil {
		t.Fatal(err)
	}
	if !hidden() {
		t.Fatal("not hidden once hidden")
	}
	// A restart reads what was kept.
	n.loadHiddenAgents()
	if !hidden() {
		t.Fatal("shown again after a restart")
	}
	// The owner's rescan while it is still broken: hidden.
	n.rescanHidden()
	if !hidden() {
		t.Fatal("shown again by a rescan that found it still broken")
	}
	// Installed for real: the rescan shows it again.
	found(provider.Info{Name: "codex", Available: true, Binary: "/bin/codex", Version: "1.2.3"})
	n.rescanHidden()
	if hidden() {
		t.Fatal("still hidden after a rescan found it running")
	}

	// Hidden while it runs: a rescan that finds it running the same keeps it hidden; the owner shows it.
	if err := set("codex", true); err != nil {
		t.Fatal(err)
	}
	n.rescanHidden()
	if !hidden() {
		t.Fatal("shown again though nothing changed")
	}
	if err := set("codex", false); err != nil {
		t.Fatal(err)
	}
	if hidden() {
		t.Fatal("still hidden once shown")
	}

	if err := set("no-such-agent", true); err == nil {
		t.Fatal("hid an agent the machine doesn't know")
	}
}
