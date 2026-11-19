package provider

import (
	"slices"
	"testing"
)

// An environment passed to posix_spawn has one entry a key, its last, as
// os/exec makes it: a value added after another of its key overrides it.
func TestDedupEnv(t *testing.T) {
	got := dedupEnv([]string{"A=1", "PATH=/bin", "B=", "A=2", "C", "B=x", "C=3"})
	if want := []string{"PATH=/bin", "A=2", "B=x", "C=3"}; !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := dedupEnv(nil); len(got) != 0 {
		t.Fatalf("nil: %q", got)
	}
}
