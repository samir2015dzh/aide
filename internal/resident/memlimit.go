// Package resident keeps what aided costs the computer it stays on small
// while it waits (docs/resident.md): a soft limit on the Go heap, Windows'
// power saving while nothing runs and nobody looks, and new sessions held
// back while the system is short of memory.
package resident

import (
	"log/slog"
	"os"
	"runtime/debug"
	"strings"
)

// DefaultMemoryLimit is the Go runtime's soft memory limit when GOMEMLIMIT
// is not set: the garbage collector works harder as the heap nears it, and
// never stops the daemon for it (it is soft: above it the daemon only
// collects more often). 64 MiB is about twice what an idle daemon holds
// (docs/perf.md), well above what many sessions add (~148 KB each).
const DefaultMemoryLimit = 64 << 20

// SetMemoryLimit applies DefaultMemoryLimit unless GOMEMLIMIT says
// otherwise (a size, or "off" for none), which the runtime has applied
// itself; it returns the limit in force, 0 for none.
func SetMemoryLimit(log *slog.Logger) int64 {
	if v := strings.TrimSpace(os.Getenv("GOMEMLIMIT")); v != "" {
		limit := debug.SetMemoryLimit(-1) // reads it
		if limit == maxInt64 {
			limit = 0
		}
		log.Info("go memory limit", "limit_mib", limit>>20, "from", "GOMEMLIMIT="+v)
		return limit
	}
	debug.SetMemoryLimit(DefaultMemoryLimit)
	log.Info("go memory limit", "limit_mib", DefaultMemoryLimit>>20, "from", "default (GOMEMLIMIT overrides)")
	return DefaultMemoryLimit
}

const maxInt64 = 1<<63 - 1
