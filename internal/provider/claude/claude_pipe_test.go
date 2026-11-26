package claude

import (
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
)

func TestIsStdinClosedPortable(t *testing.T) {
	for _, err := range []error{
		io.ErrClosedPipe,
		fmt.Errorf("send: %w", &os.PathError{Op: "write", Path: "|1", Err: os.ErrClosed}),
	} {
		if !isStdinClosed(err) {
			t.Errorf("isStdinClosed(%v) = false, want true", err)
		}
	}
	for _, err := range []error{
		errors.New("permission denied"),
		&os.PathError{Op: "write", Path: "|1", Err: os.ErrPermission},
	} {
		if isStdinClosed(err) {
			t.Errorf("isStdinClosed(%v) = true, want false", err)
		}
	}
}
