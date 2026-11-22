package boundary

import (
	"testing"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
)

// A rule holds for the kinds of recipient it names, or every one when it
// names none; a recipient of a kind the sender can't tell meets only the
// rules for every one. The category is what it says, never what matched.
// (Placeholder rules: the owner's own are kept on their side.)
func TestCheck(t *testing.T) {
	c := Compile(&identityv1.BoundaryPolicy{Rules: []*identityv1.BoundaryRule{
		{Category: "example-a", Patterns: []string{`(?i)\bplaceholder-alpha\b`}, RecipientKinds: []string{"Claude"}},
		{Category: "example-b", Patterns: []string{`placeholder-[0-9]{4}`}},
		{Category: "broken", Patterns: []string{`(`}},
	}})
	for _, tc := range []struct{ text, kind, want string }{
		{"see Placeholder-Alpha here", "claude", "example-a"},
		{"see placeholder-alpha here", "codex", ""},
		{"see placeholder-alpha here", "", ""},
		{"code placeholder-1234", "", "example-b"},
		{"code placeholder-1234", "codex", "example-b"},
		{"nothing here", "claude", ""},
	} {
		if got := c.Check(tc.text, tc.kind); got != tc.want {
			t.Errorf("Check(%q, %q) = %q, want %q", tc.text, tc.kind, got, tc.want)
		}
	}
	var none *Checker
	if none.Check("placeholder-1234", "") != "" || !none.Empty() || c.Empty() {
		t.Fatal("a nil checker holds nothing")
	}
}
