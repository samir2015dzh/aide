package resident

import (
	"io"
	"log/slog"
	"runtime/debug"
	"testing"
)

func TestMemoryLimit(t *testing.T) {
	before := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(before) })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Setenv("GOMEMLIMIT", "")
	if got := SetMemoryLimit(log); got != DefaultMemoryLimit || debug.SetMemoryLimit(-1) != DefaultMemoryLimit {
		t.Fatalf("default: %d", got)
	}
	// Set, it is the runtime's: what it reads is kept (here, what the test set).
	debug.SetMemoryLimit(96 << 20)
	t.Setenv("GOMEMLIMIT", "96MiB")
	if got := SetMemoryLimit(log); got != 96<<20 {
		t.Fatalf("GOMEMLIMIT=96MiB: %d", got)
	}
	debug.SetMemoryLimit(maxInt64)
	t.Setenv("GOMEMLIMIT", "off")
	if got := SetMemoryLimit(log); got != 0 {
		t.Fatalf("GOMEMLIMIT=off: %d", got)
	}
}
