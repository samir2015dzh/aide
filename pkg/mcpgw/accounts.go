package mcpgw

import (
	"context"
	"net/http"

	"github.com/samir2015dzh/aide/pkg/relay"
)

// SingleOwner is the Accounts of a self-hosted relay: one owner (none until
// the server is set up), whose account and password the consent page asks
// for (relay.IssueGrant): only those connect an app.
type SingleOwner struct{ Relay *relay.Server }

func (s SingleOwner) Browser(*http.Request) (Account, bool) {
	return Account{ID: "owner", OwnerID: s.Relay.SingleOwner()}, true
}

func (s SingleOwner) SignInURL(next string) string { return next }

func (s SingleOwner) Owner(context.Context, string) (string, error) {
	return s.Relay.SingleOwner(), nil
}
