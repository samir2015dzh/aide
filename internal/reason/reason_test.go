package reason

import (
	"errors"
	"fmt"
	"testing"

	"connectrpc.com/connect"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
)

// TestReason (工单 #60): the error keeps its English message for logs and older apps, and carries its key and args
// for the apps to say in the owner's language; wrapped, the key is still found.
func TestReason(t *testing.T) {
	e := New(connect.CodeUnavailable, "no_host_online", "no machine of yours is online to keep the conversation: start Aide on one", "machine", "work-pc")
	if e.Message() != "no machine of yours is online to keep the conversation: start Aide on one" || e.Code() != connect.CodeUnavailable {
		t.Fatalf("message or code: %v", e)
	}
	if got := Of(fmt.Errorf("wrapped: %w", e)); got != "no_host_online" {
		t.Fatalf("key: %q", got)
	}
	m, err := e.Details()[0].Value()
	if r, ok := m.(*localv1.ErrorReason); err != nil || !ok || r.Args["machine"] != "work-pc" {
		t.Fatalf("args: %v %v", m, err)
	}
	if Of(errors.New("plain")) != "" || Of(connect.NewError(connect.CodeInternal, errors.New("x"))) != "" {
		t.Fatal("a key on an error without one")
	}
}
