//go:build !windows

package claude

import (
	"fmt"
	"os"
	"syscall"
	"testing"
)

func TestIsStdinClosedEPIPE(t *testing.T) {
	err := fmt.Errorf("send: %w", &os.PathError{Op: "write", Path: "|1", Err: syscall.EPIPE})
	if !isStdinClosed(err) {
		t.Fatalf("isStdinClosed(%v) = false, want true", err)
	}
	if isStdinClosed(&os.PathError{Op: "write", Path: "|1", Err: syscall.EACCES}) {
		t.Fatal("EACCES taken for a closed pipe")
	}
}
