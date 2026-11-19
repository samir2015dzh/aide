package dshnative

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var openSemaphore = windows.NewLazySystemDLL("kernel32.dll").NewProc("OpenSemaphoreW")

// lockProbe tells whether a DSH process holds a session's write lock. On
// Windows that lock is a named semaphore, Local\dsh-session-lock-<sha256
// of the lock path in lower case>, that exists only while a process holds
// a handle to it: the probe asks whether it exists (OpenSemaphoreW) and
// closes what it opened at once, without waiting on it, so its count never
// changes. Local\ names are per logon session: hermeshd sees the
// semaphores of DSH processes in its own.
func lockProbe() func(dir string) (held, known bool) {
	return func(dir string) (bool, bool) {
		path, err := filepath.Abs(lockPath(dir))
		if err != nil {
			return false, false
		}
		sum := sha256.Sum256([]byte(strings.ToLower(path)))
		name, err := windows.UTF16PtrFromString(`Local\dsh-session-lock-` + hex.EncodeToString(sum[:]))
		if err != nil {
			return false, false
		}
		h, _, err := openSemaphore.Call(windows.SYNCHRONIZE, 0, uintptr(unsafe.Pointer(name)))
		switch {
		case h != 0:
			windows.CloseHandle(windows.Handle(h))
			return true, true
		case errors.Is(err, windows.ERROR_FILE_NOT_FOUND):
			return false, true
		case errors.Is(err, windows.ERROR_ACCESS_DENIED): // it exists
			return true, true
		}
		return false, false
	}
}
