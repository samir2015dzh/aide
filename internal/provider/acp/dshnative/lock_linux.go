package dshnative

import (
	"errors"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// lockProbe tells whether a DSH process holds a session's write lock. On
// Linux that lock is a flock(2) on the session's session.lock: the probe
// looks for it among the locks /proc/locks lists, by the file's inode
// (only: on some filesystems /proc/locks names another device than stat
// does), and never opens the file.
func lockProbe() func(dir string) (held, known bool) {
	b, err := os.ReadFile("/proc/locks")
	if err != nil {
		return func(string) (bool, bool) { return false, false }
	}
	inodes := map[uint64]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		// "1: FLOCK  ADVISORY  WRITE 4242 fd:01:1234567 0 EOF"; a waiter's
		// line has "->" after the number, and holds nothing.
		f := strings.Fields(line)
		if len(f) < 6 || f[1] != "FLOCK" {
			continue
		}
		if i := strings.LastIndexByte(f[5], ':'); i >= 0 {
			if ino, err := strconv.ParseUint(f[5][i+1:], 10, 64); err == nil {
				inodes[ino] = true
			}
		}
	}
	return func(dir string) (bool, bool) {
		fi, err := os.Stat(lockPath(dir))
		if errors.Is(err, fs.ErrNotExist) {
			return false, true // never opened for writing
		}
		if err != nil {
			return false, false
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		return ok && inodes[st.Ino], ok
	}
}
