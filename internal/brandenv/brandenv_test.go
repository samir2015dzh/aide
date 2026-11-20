package brandenv

import (
	"slices"
	"testing"
)

func TestGet(t *testing.T) {
	t.Setenv(Prefix+"X", "")
	t.Setenv(OldPrefix+"X", "")
	if v := Get("X"); v != "" {
		t.Fatalf("neither set: %q", v)
	}
	t.Setenv(OldPrefix+"X", "old")
	if v := Get("X"); v != "old" {
		t.Fatalf("the old name only: %q", v)
	}
	t.Setenv(Prefix+"X", "new")
	if v := Get("X"); v != "new" {
		t.Fatalf("both: %q, want the new name's", v)
	}
}

func TestBoth(t *testing.T) {
	if got := Both("SESSION", "s1"); !slices.Equal(got, []string{Prefix + "SESSION=s1", OldPrefix + "SESSION=s1"}) || Prefix == OldPrefix {
		t.Fatal(got)
	}
}
