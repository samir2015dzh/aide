package brandenv

import (
	"slices"
	"testing"
)

func TestGet(t *testing.T) {
	t.Setenv("AIDE_X", "")
	t.Setenv("HERMESH_X", "")
	if v := Get("X"); v != "" {
		t.Fatalf("neither set: %q", v)
	}
	t.Setenv("HERMESH_X", "old")
	if v := Get("X"); v != "old" {
		t.Fatalf("the old name only: %q", v)
	}
	t.Setenv("AIDE_X", "new")
	if v := Get("X"); v != "new" {
		t.Fatalf("both: %q, want the new name's", v)
	}
}

func TestBoth(t *testing.T) {
	if got := Both("SESSION", "s1"); !slices.Equal(got, []string{"AIDE_SESSION=s1", "HERMESH_SESSION=s1"}) {
		t.Fatal(got)
	}
}
