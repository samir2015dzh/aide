package cli

import (
	"strings"
	"testing"
	"time"
)

// A session's name in addresses: its title as names are written, cut to
// the longest a name may be, its id when the title has nothing to keep.
func TestSessionName(t *testing.T) {
	for _, c := range []struct{ title, want string }{
		{"Fix CI", "fix-ci"},
		{"  修复 构建  ", "修复-构建"},
		{"", "s_1"},
		{"!!!", "s_1"},
		{strings.Repeat("ab ", 30), strings.Repeat("ab-", 13) + "a"}, // 40 runes, as a name may be
	} {
		if got := sessionName(c.title, "s_1"); got != c.want {
			t.Errorf("sessionName(%q) = %q, want %q", c.title, got, c.want)
		}
	}
}

// Two sessions of one name on a machine: the older keeps it, the later
// take -2, -3, whatever order they are listed in.
func TestSessionNames(t *testing.T) {
	t0 := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	got := sessionNames([]namedSession{
		{"s_c", "Fix CI", t0.Add(2 * time.Minute)},
		{"s_a", "Fix CI", t0},
		{"s_x", "Deploy", t0.Add(time.Minute)},
		{"s_b", "fix ci", t0.Add(time.Minute)},
	})
	want := map[string]string{"s_a": "fix-ci", "s_b": "fix-ci-2", "s_c": "fix-ci-3", "s_x": "deploy"}
	for id, n := range want {
		if got[id] != n {
			t.Errorf("%s: %q, want %q (all: %v)", id, got[id], n, got)
		}
	}
}

// A readable name finds its session: with its machine, or alone when only
// one session has it; two of one name are refused, naming them.
func TestSessionByName(t *testing.T) {
	eps := []endpointJSON{
		{Address: "c_1", Name: "Muse", Kind: "mcp"},
		{Address: "m_a/s_1", Name: "work-mac/fix-ci", Kind: "aide_session"},
		{Address: "m_b/s_2", Name: "home-pc/fix-ci", Kind: "aide_session"},
		{Address: "dsh_m_a_x", Name: "work-mac/explain-repo", Kind: "dsh_session"},
	}
	if e, ok, err := sessionByName("work-mac/fix-ci", eps); !ok || err != nil || e.Address != "m_a/s_1" {
		t.Fatalf("with its machine: %+v %v %v", e, ok, err)
	}
	if e, ok, err := sessionByName("explain-repo", eps); !ok || err != nil || e.Address != "dsh_m_a_x" {
		t.Fatalf("alone, one has it: %+v %v %v", e, ok, err)
	}
	if _, ok, err := sessionByName("fix-ci", eps); ok || err == nil || !strings.Contains(err.Error(), "work-mac/fix-ci, home-pc/fix-ci") {
		t.Fatalf("two of one name: %v %v", ok, err)
	}
	if _, ok, err := sessionByName("Muse", eps); ok || err != nil {
		t.Fatalf("an agent's name is not a session's: %v %v", ok, err)
	}
	if _, ok, err := sessionByName("nobody/fix-ci", eps); ok || err != nil {
		t.Fatalf("no such machine: %v %v", ok, err)
	}
}

// Writing to many at once is the owner's alone.
func TestIsBroadcast(t *testing.T) {
	for to, want := range map[string]bool{"*": true, "work-mac/*": true, "repo:acme/site": true, "Muse": false, "work-mac/fix-ci": false, "m_a/s_1": false} {
		if isBroadcast(to) != want {
			t.Errorf("isBroadcast(%q) = %v", to, !want)
		}
	}
}
