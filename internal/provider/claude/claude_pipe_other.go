//go:build !windows

package claude

import (
	"errors"
	"syscall"
)

// isPipeClosedErrno reports a write to a pipe whose reader is gone (EPIPE).
func isPipeClosedErrno(err error) bool { return errors.Is(err, syscall.EPIPE) }
