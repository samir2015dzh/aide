//go:build !windows

package resident

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// readMemory reads Linux's /proc/meminfo (MemAvailable); elsewhere memory
// is unknown, and never holds a session back.
func readMemory() (Memory, bool) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return Memory{}, false
	}
	defer f.Close()
	var m Memory
	var total, avail bool
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		kb, err := strconv.ParseUint(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "kB")), 10, 64)
		if err != nil {
			continue
		}
		switch k {
		case "MemTotal":
			m.Total, total = kb<<10, true
		case "MemAvailable":
			m.Available, avail = kb<<10, true
		}
	}
	return m, total && avail
}
