package resident

import (
	"fmt"
	"strconv"
	"sync"

	"github.com/samir2015dzh/aide/internal/brandenv"
)

// Memory pressure (docs/resident.md §3): while the system is short of
// memory, new sessions stay queued instead of starting an agent (30–60 MB
// each, and what it runs) into a computer that is already paging; they
// start in order once it has memory again.

// Memory is the system's physical memory now.
type Memory struct {
	Total, Available uint64 // bytes
}

// Pressure says whether memory is low, with a margin so that it does not
// flip around the threshold.
type Pressure struct {
	read func() (Memory, bool) // the system's (tests replace it)

	mu  sync.Mutex
	low bool
}

// Thresholds: low below the reserve (AIDE_MEMORY_RESERVE_MB, default 1024
// MB) or under 8% of the total free; it stays low until 25% more than that
// is free again. AIDE_MEMORY_RESERVE_MB=0 turns it off.
const (
	defaultReserve = 1 << 30
	lowShare       = 0.08
	recoverFactor  = 1.25
)

func reserve() uint64 {
	if v, err := strconv.ParseUint(brandenv.Get("MEMORY_RESERVE_MB"), 10, 64); err == nil {
		return v << 20
	}
	return defaultReserve
}

var pressure = &Pressure{read: readMemory}

// MemoryLow is the system's memory pressure, with a few words on it when
// low (for the log, and the session's queued reason).
func MemoryLow() (bool, string) { return pressure.Low() }

// Low reads the memory now and says whether it is low, and how low.
func (p *Pressure) Low() (bool, string) {
	if brandenv.Get("MEMORY_RESERVE_MB") == "0" {
		return false, ""
	}
	m, ok := p.read()
	if !ok || m.Total == 0 {
		return false, "" // unknown: never held back for it
	}
	line := max(reserve(), uint64(float64(m.Total)*lowShare))
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.low {
		p.low = m.Available < uint64(float64(line)*recoverFactor)
	} else {
		p.low = m.Available < line
	}
	if !p.low {
		return false, ""
	}
	return true, fmt.Sprintf("%d MB of %d MB free (needs %d MB)", m.Available>>20, m.Total>>20, uint64(float64(line)*recoverFactor)>>20)
}
