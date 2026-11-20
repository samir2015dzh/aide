package owner

import (
	"errors"
	"testing"

	"connectrpc.com/connect"

	relayv1 "github.com/samir2015dzh/aide/gen/go/aide/relay/v1"
)

// A relay that gives its MCP address, one whose MCP is off, one older than
// the call (2026-11-20: the connect guide put that down to a missing
// domain and HTTPS), and one that did not answer.
func TestMCPAnswer(t *testing.T) {
	for _, c := range []struct {
		name      string
		res       *relayv1.GetMCPResponse
		err       error
		url       string
		older, ok bool
	}{
		{"address", &relayv1.GetMCPResponse{Url: "https://relay.example.com/mcp/k"}, nil, "https://relay.example.com/mcp/k", false, true},
		{"mcp off", &relayv1.GetMCPResponse{}, nil, "", false, true},
		{"older relay", nil, connect.NewError(connect.CodeUnimplemented, errors.New("unknown procedure")), "", true, true},
		{"no answer", nil, connect.NewError(connect.CodeUnavailable, errors.New("gone")), "", false, false},
	} {
		url, older, ok := mcpAnswer(c.res, c.err)
		if url != c.url || older != c.older || ok != c.ok {
			t.Errorf("%s: got %q %v %v", c.name, url, older, ok)
		}
	}
}
