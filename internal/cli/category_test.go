package cli

import "testing"

func TestAgentCategory(t *testing.T) {
	machines := []string{"work-pc", "BigBox"}
	for _, c := range []struct{ name, cat, machine string }{
		{"DSH on work-pc", categoryLocal, "work-pc"},
		{"client@BigBox", categoryLocal, "BigBox"},
		{"Codex", categoryLocal, ""},
		{"Muse", categoryPersonal, ""},
		{"Grok Bot (cursor.com)", categoryPersonal, ""},
		{"Claude (claude.ai)", categoryPersonal, ""},
		{"Reviewer", categoryPersonal, ""},
	} {
		if cat, m := agentCategory(c.name, machines); cat != c.cat || m != c.machine {
			t.Errorf("%q: %s %q, want %s %q", c.name, cat, m, c.cat, c.machine)
		}
	}
}
