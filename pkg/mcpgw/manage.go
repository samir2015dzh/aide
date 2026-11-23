package mcpgw

import (
	"context"
	"errors"
	"net/url"
	"time"
)

// What an account sees of its MCP connections, and what it can do with
// them: list them and disconnect them.

// ConnectionInfo is one MCP connection as its account sees it.
type ConnectionInfo struct {
	ID string
	// Name is the app's name ("Claude"), ClientHost where its metadata
	// document lives ("claude.ai"; "" for a registered client).
	Name       string
	ClientHost string
	// ReturnsTo is where the app returns to after connecting, as it
	// registered it ("127.0.0.1", "claude.ai"): a detail, never its name
	// (工单 #85).
	ReturnsTo string
	Verified  bool
	State     string
	// GrantID: the owner's list of agents has it too.
	GrantID   string
	CreatedAt time.Time
	LastUsed  time.Time
}

// ErrNoConnection: no such live connection for this account.
var ErrNoConnection = errors.New("no such connection")

// Connections lists an account's live MCP connections, newest first.
func (g *Gateway) Connections(ctx context.Context, accountID string) ([]ConnectionInfo, error) {
	conns, err := g.db.ListConnections(ctx, ConnectionFilter{AccountID: accountID, States: []string{StateActive}})
	if err != nil {
		return nil, err
	}
	out := make([]ConnectionInfo, 0, len(conns))
	for i := len(conns) - 1; i >= 0; i-- {
		c := conns[i]
		info := ConnectionInfo{ID: c.ID, Name: c.ClientName, State: c.State, GrantID: c.GrantID, CreatedAt: c.CreatedAt, LastUsed: c.LastUsed}
		if cl, err := g.db.GetClient(ctx, c.ClientID); err == nil && cl != nil {
			info.Name, info.Verified = cl.Name, cl.Verified
			if len(cl.RedirectURIs) > 0 {
				if u, err := url.Parse(cl.RedirectURIs[0]); err == nil {
					info.ReturnsTo = u.Hostname()
				}
			}
			if u, err := url.Parse(cl.ID); err == nil && cl.Source == "cimd" {
				info.ClientHost = u.Hostname()
			}
		}
		out = append(out, info)
	}
	return out, nil
}

// owned returns an account's live connection.
func (g *Gateway) owned(ctx context.Context, accountID, id string) (*Connection, error) {
	c, err := g.db.GetConnection(ctx, id)
	if err != nil {
		return nil, err
	}
	if c == nil || c.AccountID != accountID || !c.Live() {
		return nil, ErrNoConnection
	}
	return c, nil
}

// Disconnect ends an account's connection: its tokens stop working, its
// key is destroyed and its grant revoked.
func (g *Gateway) Disconnect(ctx context.Context, accountID, id string) error {
	if _, err := g.owned(ctx, accountID, id); err != nil {
		return err
	}
	return g.endConnection(ctx, id, StateRevoked, "disconnected in the console")
}
