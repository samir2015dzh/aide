package resident

import (
	"strings"
	"testing"
)

func TestMemoryPressure(t *testing.T) {
	t.Setenv("AIDE_MEMORY_RESERVE_MB", "")
	const gb = 1 << 30
	m := Memory{Total: 16 * gb}
	p := &Pressure{read: func() (Memory, bool) { return m, true }}
	// 16 GB: the line is 8% of it, 1.28 GB; back above 1.6 GB.
	for _, c := range []struct {
		avail uint64
		low   bool
	}{
		{8192 << 20, false},
		{1340 << 20, false},
		{1290 << 20, true},
		{1500 << 20, true}, // not yet back
		{1700 << 20, false},
		{1340 << 20, false},
	} {
		m.Available = c.avail
		low, why := p.Low()
		if low != c.low || low != (why != "") {
			t.Fatalf("%d MB free: low %v (%q)", c.avail>>20, low, why)
		}
		if low && !strings.Contains(why, "MB free") {
			t.Fatal(why)
		}
	}
	// A small computer: the reserve, 1 GB, is the line.
	m = Memory{Total: 4 * gb, Available: 900 << 20}
	p = &Pressure{read: func() (Memory, bool) { return m, true }}
	if low, _ := p.Low(); !low {
		t.Fatal("900 MB of 4 GB free is low")
	}
	// The reserve can be set.
	t.Setenv("AIDE_MEMORY_RESERVE_MB", "256")
	p = &Pressure{read: func() (Memory, bool) { return m, true }}
	if low, _ := p.Low(); low {
		t.Fatal("900 MB free over a 320 MB line is not low")
	}
	// Unknown memory never holds back.
	p = &Pressure{read: func() (Memory, bool) { return Memory{}, false }}
	if low, _ := p.Low(); low {
		t.Fatal("unknown is low")
	}
	if m, ok := readMemory(); ok && (m.Total == 0 || m.Available > m.Total) {
		t.Fatalf("this system: %+v", m)
	}
}
