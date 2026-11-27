package provider

import (
	"reflect"
	"testing"
)

// An answer sent as one string reads back as it was given (工单 #179): the
// chosen labels in order, then the typed text, even where a label contains
// ", " or begins another label. The first cases are paseo's
// (question-card-answer.test.ts, Apache License 2.0).
func TestSplitAnswer(t *testing.T) {
	paseo := []string{"Delete both", "Delete both, fix code", "Email the user"}
	prefixes := []string{"A", "A, B", "A, B, C", "B"}
	for _, c := range []struct {
		answer   string
		labels   []string
		selected []string
		text     string
	}{
		{"Email the user, Delete both", paseo, []string{"Email the user", "Delete both"}, ""},
		{"Delete both, fix code, Email the user", paseo, []string{"Delete both, fix code", "Email the user"}, ""},
		{"Delete both, Also check the logs", paseo, []string{"Delete both"}, "Also check the logs"},
		{"Only typed text", paseo, nil, "Only typed text"},
		// Labels that begin one another: the longest that fits is taken.
		{"A, B, C", prefixes, []string{"A, B, C"}, ""},
		{"B, A", prefixes, []string{"B", "A"}, ""},
		{"A, B, C, A", prefixes, []string{"A, B, C", "A"}, ""},
		// Chosen and typed, the typed text with ", " of its own.
		{"Email the user, see a, b and c", paseo, []string{"Email the user"}, "see a, b and c"},
		// A label is taken once; the same words again are typed text.
		{"B, B", prefixes, []string{"B"}, "B"},
		{"", paseo, nil, ""},
	} {
		sel, text := SplitAnswer(c.answer, c.labels)
		if !reflect.DeepEqual(sel, c.selected) || text != c.text {
			t.Errorf("%q: got %q + %q, want %q + %q", c.answer, sel, text, c.selected, c.text)
		}
	}
	// Value writes what SplitAnswer reads.
	for _, a := range []Answer{
		{Selected: []string{"Delete both, fix code", "Email the user"}, Text: "and tell me"},
		{Selected: []string{"Delete both"}},
		{Text: "just this"},
	} {
		sel, text := SplitAnswer(a.Value(), paseo)
		if !reflect.DeepEqual(sel, a.Selected) || text != a.Text {
			t.Errorf("%+v read back as %q + %q", a, sel, text)
		}
	}
}
