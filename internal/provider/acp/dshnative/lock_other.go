//go:build !linux && !windows

package dshnative

// lockProbe can't tell here whether a DSH process holds a session's write
// lock: macOS has no /proc/locks (whether fcntl F_GETLK sees flock(2)
// locks there is unverified), and trying to take the lock could make DSH
// fail to open the session.
func lockProbe() func(dir string) (held, known bool) {
	return func(string) (bool, bool) { return false, false }
}
