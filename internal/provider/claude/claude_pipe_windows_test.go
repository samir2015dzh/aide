package claude

import (
	"fmt"
	"os"
	"syscall"
	"testing"
)

// localizedErrno is an Errno worded as a Chinese-language Windows words it:
// the English fallback cannot match, only the number can (工单 #126).
type localizedErrno struct{ errno syscall.Errno }

func (e localizedErrno) Error() string { return "管道正在被关闭。" }
func (e localizedErrno) Unwrap() error { return e.errno }

func TestIsStdinClosedWindowsErrno(t *testing.T) {
	for _, n := range []syscall.Errno{232, 109} { // ERROR_NO_DATA, ERROR_BROKEN_PIPE
		for _, err := range []error{
			&os.PathError{Op: "write", Path: "|1", Err: n},
			fmt.Errorf("send: %w", &os.PathError{Op: "write", Path: "|1", Err: localizedErrno{n}}),
		} {
			if !isStdinClosed(err) {
				t.Errorf("isStdinClosed(%v) = false, want true (errno %d)", err, n)
			}
		}
	}
	if err := (&os.PathError{Op: "write", Path: "|1", Err: localizedErrno{5}}); isStdinClosed(err) { // ERROR_ACCESS_DENIED
		t.Fatal("ERROR_ACCESS_DENIED taken for a closed pipe")
	}
}
