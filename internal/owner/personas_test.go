package owner

import (
	"errors"
	"strings"
	"testing"

	"connectrpc.com/connect"
)

// TestContactsErr (工单 #42): a relay older than contacts answers 404, which
// the client reads as unimplemented "404 Not Found"; one without their store
// says it keeps none. Both reach the apps as contacts_unavailable, which they
// say in words; any other error passes as it was.
func TestContactsErr(t *testing.T) {
	for _, e := range []error{
		connect.NewError(connect.CodeUnimplemented, errors.New("404 Not Found")),
		connect.NewError(connect.CodeUnimplemented, errors.New("this relay keeps no contacts")),
	} {
		got := contactsErr(e)
		if connect.CodeOf(got) != connect.CodeUnimplemented {
			t.Errorf("%v → %v", e, got)
		}
		var ce *connect.Error
		if !errors.As(got, &ce) || !strings.HasPrefix(ce.Message(), ContactsUnavailable) {
			t.Errorf("%v: message %q does not start with %s", e, ce.Message(), ContactsUnavailable)
		}
	}
	other := connect.NewError(connect.CodeUnavailable, errors.New("offline"))
	if contactsErr(other) != other {
		t.Error("another error was changed")
	}
}
