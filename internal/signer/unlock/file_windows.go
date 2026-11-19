package unlock

import (
	"errors"
	"io"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// readFile reads path and lets it be replaced or removed meanwhile.
// os.ReadFile opens without FILE_SHARE_DELETE, and while such a handle is
// open Windows refuses to replace or delete the file: the signer's window,
// reading the wrong passphrases' count as it draws, made the check that
// counts one more fail ("Access is denied") and closed the window.
func readFile(path string) ([]byte, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	f := os.NewFile(uintptr(h), path)
	defer f.Close()
	return io.ReadAll(f)
}

// retry does f again for a moment while another program has the file open
// without sharing it (an antivirus scanning it, say), or it is still being
// deleted.
func retry(f func() error) error {
	var err error
	for range 40 {
		if err = f(); err == nil || !errors.Is(err, windows.ERROR_ACCESS_DENIED) && !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			return err
		}
		time.Sleep(25 * time.Millisecond)
	}
	return err
}
