// Package reason gives an error a key the apps say in the owner's language
// (工单 #60): the message stays English, for logs and for apps that don't
// know the key; the apps look the key up (localv1.ErrorReason on the error
// as a detail) and say it in their own words, with its args.
package reason

import (
	"errors"

	"connectrpc.com/connect"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
)

// New is a connect error with message msg and the reason key, args in
// pairs (name, value, …).
func New(code connect.Code, key, msg string, args ...string) *connect.Error {
	e := connect.NewError(code, errors.New(msg))
	r := &localv1.ErrorReason{Reason: key}
	if len(args) > 1 {
		r.Args = map[string]string{}
		for i := 0; i+1 < len(args); i += 2 {
			r.Args[args[i]] = args[i+1]
		}
	}
	if d, err := connect.NewErrorDetail(r); err == nil {
		e.AddDetail(d)
	}
	return e
}

// Of is the reason key an error carries, "" when none.
func Of(err error) string {
	var ce *connect.Error
	if !errors.As(err, &ce) {
		return ""
	}
	for _, d := range ce.Details() {
		if m, err := d.Value(); err == nil {
			if r, ok := m.(*localv1.ErrorReason); ok {
				return r.Reason
			}
		}
	}
	return ""
}
