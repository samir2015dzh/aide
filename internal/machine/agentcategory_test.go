package machine

import (
	"context"
	"testing"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
)

// The owner files a coding agent as a kind (工单 #93): its status says so,
// a restart keeps it, clearing it goes back to told from what it is; an
// unknown kind or an agent the machine doesn't know is refused.
func TestAgentCategory(t *testing.T) {
	h := newHarness(t, DefaultLimits())
	n := h.n
	ctx := context.Background()
	set := func(agent, category string) error {
		_, err := n.execAdmin(ctx, &identityv1.AdminCommand{Command: &identityv1.AdminCommand_SetAgentCategory{SetAgentCategory: &identityv1.SetAgentCategory{Agent: agent, Category: category}}})
		return err
	}
	category := func() string {
		t.Helper()
		for _, a := range n.Status().Agents {
			if a.Name == "fake" {
				return a.Category
			}
		}
		t.Fatal("fake not listed")
		return ""
	}
	if c := category(); c != "" {
		t.Fatalf("category before any say = %q, want none", c)
	}
	if err := set("fake", "personal"); err != nil {
		t.Fatal(err)
	}
	if c := category(); c != "personal" {
		t.Fatalf("category = %q, want personal", c)
	}
	n.loadAgentCategories() // a restart reads what was kept
	if c := category(); c != "personal" {
		t.Fatalf("category after a restart = %q, want personal", c)
	}
	if err := set("fake", ""); err != nil {
		t.Fatal(err)
	}
	if c := category(); c != "" {
		t.Fatalf("category once cleared = %q, want none", c)
	}
	if err := set("fake", "robot"); err == nil {
		t.Fatal("an unknown kind was taken")
	}
	if err := set("nobody", "local"); err == nil {
		t.Fatal("an agent the machine doesn't know was filed")
	}
}
