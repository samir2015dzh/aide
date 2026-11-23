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

// Who gets on the roster (工单 #64, #79), as in the apps: personal agents,
// coding agents a name tells or the owner filed; never Aide's own command
// line on a machine, nor a name of no known kind on one.
func TestOnRoster(t *testing.T) {
	machines := []string{"work-pc", "BigBox"}
	for _, c := range []struct {
		name string
		set  bool
		want bool
	}{
		{"Muse", false, true},
		{"Claude (claude.ai)", false, true},
		{"DSH on work-pc", false, true},
		{"Codex", false, true},
		{"client@BigBox", false, false},
		{"Client@elsewhere", false, false},
		{"client@BigBox", true, false}, // whatever it was filed as
		{"Her on work-pc", false, false},
		{"Her on work-pc", true, true}, // the owner filed it as a coding agent
	} {
		cat, _ := agentCategory(c.name, machines)
		if got := onRoster(c.name, cat, c.set); got != c.want {
			t.Errorf("%q (set %v): %v, want %v", c.name, c.set, got, c.want)
		}
	}
}
