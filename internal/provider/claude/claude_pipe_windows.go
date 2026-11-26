package claude

import (
	"errors"

	"golang.org/x/sys/windows"
)

// isPipeClosedErrno reports a write to a pipe whose reader is gone. Windows
// says so with ERROR_NO_DATA (232, "the pipe is being closed") or
// ERROR_BROKEN_PIPE (109), never EPIPE, and words both in the system's
// language, so only the number is a reliable test (工单 #126).
func isPipeClosedErrno(err error) bool {
	return errors.Is(err, windows.ERROR_NO_DATA) || errors.Is(err, windows.ERROR_BROKEN_PIPE)
}
