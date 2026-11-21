package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// status asks the machine that runs, as doctor does: aided login's own, or
// the app's in its owner profile, the current one before the others.
func TestMachineDirs(t *testing.T) {
	data := t.TempDir()
	own := filepath.Join(data, "machine")
	if got := machineDirs(data); !slices.Equal(got, []string{own}) {
		t.Fatalf("no profiles: %v", got)
	}
	for _, p := range []string{"01aaa", "01bbb", "01ccc"} {
		if err := os.MkdirAll(filepath.Join(data, "owner", "profiles", p, "machine"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	prof := func(p string) string { return filepath.Join(data, "owner", "profiles", p, "machine") }
	if err := os.WriteFile(filepath.Join(data, "owner", "current"), []byte("01bbb\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, want := machineDirs(data), []string{own, prof("01bbb"), prof("01aaa"), prof("01ccc")}; !slices.Equal(got, want) {
		t.Fatalf("current profile second: %v, want %v", got, want)
	}
	// A current that names a path, not a profile: left out; the profiles there follow, in order.
	os.WriteFile(filepath.Join(data, "owner", "current"), []byte("../../etc"), 0o600)
	if got, want := machineDirs(data), []string{own, prof("01aaa"), prof("01bbb"), prof("01ccc")}; !slices.Equal(got, want) {
		t.Fatalf("current a path: %v, want %v", got, want)
	}
}
