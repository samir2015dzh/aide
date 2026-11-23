package mcpgw

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"errors"
	"fmt"
	"github.com/samir2015dzh/aide/internal/legacy"
	"regexp"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"google.golang.org/protobuf/proto"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	"github.com/samir2015dzh/aide/internal/cli"
	"github.com/samir2015dzh/aide/pkg/identity"
)

// Connections: made on the consent page with the owner's account and
// password, which the server signs a grant for at once; they then act with
// it. See docs/mcp-gateway.md §5.

type cachedKey struct {
	key *identity.SoftwareKey
	exp time.Time
}

// errTooMany: the owner (or account) has as many connections as allowed.
var errTooMany = errors.New("too many connected apps: disconnect one in Aide first")

// createConnection makes the connection an authorization's consent asked
// for, with a new client key, for the owner whose account and password
// were typed: the server signs its grant then (relay.IssueGrant).
func (g *Gateway) createConnection(ctx context.Context, a *Authorization, acct Account, c *Client, ua, account, password string) (*Connection, error) {
	live, err := g.db.ListConnections(ctx, ConnectionFilter{AccountID: acct.ID, States: []string{StateActive}})
	if err != nil {
		return nil, err
	}
	if len(live) >= maxConnections {
		return nil, errTooMany
	}
	key, err := identity.GenerateKey()
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key.Key)
	if err != nil {
		return nil, err
	}
	now := g.cfg.Now()
	name := label(c.Name)
	signed, chain, err := g.cfg.Relay.IssueGrant(account, password, identity.MarshalPublicKey(key.Public()), name, g.issuer.String())
	if err != nil {
		return nil, err
	}
	grant := &identityv1.Grant{}
	if err := identity.Unverified(signed, grant); err != nil {
		return nil, err
	}
	conn := &Connection{ID: "mc_" + strings.ToLower(ulid.Make().String()), AccountID: acct.ID, AccountHint: acct.Hint, OwnerID: grant.OwnerId, ClientID: c.ID,
		ClientName: name, State: StateActive, ConsentUA: clip(ua, 200), CreatedAt: now, ApprovedAt: now, GrantID: grant.GrantId}
	conn.Grant, _ = proto.Marshal(signed)
	conn.RootChain, _ = proto.Marshal(chain)
	if conn.SealedKey, err = g.cfg.Vault.Seal(der, keyAAD(conn.ID)); err != nil {
		return nil, err
	}
	if err := g.db.PutConnection(ctx, conn); err != nil {
		return nil, err
	}
	g.mu.Lock()
	g.keys[conn.ID] = cachedKey{key: key, exp: now.Add(keyCacheTTL)}
	g.mu.Unlock()
	c.LastUsed = now
	g.db.PutClient(ctx, c)
	g.replaceOlder(ctx, conn)
	g.log.Info("mcp: connected", "connection", conn.ID, "client", c.ID, "grant", grant.GrantId)
	return conn, nil
}

// label names a connection for the owner: the app's name alone, "Claude",
// at most 64 bytes (the relay's limit). Where it returns to is no part of it
// (工单 #85): two connections of one app are told apart by their id and
// when they were made; the consent page and the console say where it
// returns (ConnectionInfo.ReturnsTo).
func label(name string) string {
	if len(name) <= 64 {
		return name
	}
	r := []rune(name)
	for len(string(r))+len("…") > 64 {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

// hostedLabel is how connections were named before 工单 #85: the app and
// where it returned to, "Muse (127.0.0.1)", cut with "…" to fit.
var hostedLabel = regexp.MustCompile(`^(.+) \([^()\s]+\)$`)

// renameHosted names the live connections made before 工单 #85 by their
// app alone, on their grant too (the server signs it again, as the owner's
// rename would), so the apps, the roster, the contacts and the machines
// all say "Muse". A grant the owner renamed since keeps the owner's name.
func (g *Gateway) renameHosted(ctx context.Context) {
	conns, err := g.db.ListConnections(ctx, ConnectionFilter{States: []string{StateActive}})
	if err != nil {
		g.log.Warn("mcp: renaming connections", "err", err)
		return
	}
	for _, c := range conns {
		m := hostedLabel.FindStringSubmatch(c.ClientName)
		if m == nil {
			continue
		}
		name := m[1]
		if cl, err := g.db.GetClient(ctx, c.ClientID); err == nil && cl != nil {
			name = label(cl.Name)
		}
		l := g.connLock(c.ID)
		l.Lock()
		if cur, err := g.db.GetConnection(ctx, c.ID); err == nil && cur != nil && cur.Live() && cur.ClientName == c.ClientName {
			signed, err := g.cfg.Relay.RenameGrant(cur.GrantID, cur.ClientName, name)
			if err == nil && signed != nil {
				cur.Grant, _ = proto.Marshal(signed)
			}
			if err == nil {
				cur.ClientName = name
				err = g.db.PutConnection(ctx, cur)
			}
			if err != nil {
				g.log.Warn("mcp: renaming a connection", "connection", c.ID, "err", err)
			} else {
				g.log.Info("mcp: connection renamed", "connection", c.ID, "name", name)
			}
		}
		l.Unlock()
		g.dropPeer(c.ID) // its tunnels hold the old grant
	}
}

func keyAAD(connectionID string) []byte { return []byte(legacy.MCPKey + connectionID) }

// key is a connection's client private key.
func (g *Gateway) key(c *Connection) (*identity.SoftwareKey, error) {
	now := g.cfg.Now()
	g.mu.Lock()
	ck, ok := g.keys[c.ID]
	g.mu.Unlock()
	if ok && now.Before(ck.exp) {
		return ck.key, nil
	}
	if c.SealedKey == nil {
		return nil, errors.New("the connection's key is gone")
	}
	der, err := g.cfg.Vault.Open(c.SealedKey, keyAAD(c.ID))
	if err != nil {
		return nil, fmt.Errorf("open the connection's key: %w", err)
	}
	k, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, err
	}
	ek, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("the connection's key is not ECDSA")
	}
	key := &identity.SoftwareKey{Key: ek}
	g.mu.Lock()
	g.keys[c.ID] = cachedKey{key: key, exp: now.Add(keyCacheTTL)}
	g.mu.Unlock()
	return key, nil
}

// replaceOlder ends the account's other connections of the same app once
// a new one is made: the app reconnected.
func (g *Gateway) replaceOlder(ctx context.Context, c *Connection) {
	conns, err := g.db.ListConnections(ctx, ConnectionFilter{AccountID: c.AccountID, States: []string{StateActive}})
	if err != nil {
		return
	}
	for _, old := range conns {
		if old.ID != c.ID && old.ClientID == c.ClientID {
			g.endConnection(ctx, old.ID, StateRevoked, "replaced by a new connection")
		}
	}
}

// endConnection ends a connection: its tokens stop working, its key is
// destroyed and its grant revoked.
func (g *Gateway) endConnection(ctx context.Context, id, state, reason string) error {
	l := g.connLock(id)
	l.Lock()
	defer l.Unlock()
	c, err := g.db.GetConnection(ctx, id)
	if err != nil || c == nil {
		return err
	}
	_, err = g.end(ctx, c, state, reason)
	return err
}

// end is endConnection with the connection's lock held.
func (g *Gateway) end(ctx context.Context, c *Connection, state, reason string) (*Connection, error) {
	if !c.Live() {
		return c, nil
	}
	now := g.cfg.Now()
	if err := g.db.RevokeTokens(ctx, c.ID, "", now); err != nil {
		return nil, err
	}
	if c.GrantID != "" && c.OwnerID != "" {
		if err := g.cfg.Relay.ApplyRevocations(c.OwnerID, &identityv1.RevocationList{RevokedGrantIds: []string{c.GrantID}}); err != nil {
			g.log.Warn("mcp: revoke the grant", "connection", c.ID, "err", err)
		}
	}
	c.State, c.EndedAt, c.EndReason = state, now, reason
	c.SealedKey = nil
	if err := g.db.PutConnection(ctx, c); err != nil {
		return nil, err
	}
	g.mu.Lock()
	delete(g.keys, c.ID)
	g.mu.Unlock()
	limits.Delete(c.ID) // its rate window (work order #40)
	g.dropPeer(c.ID)
	if err := g.inboxes.DeleteInbox(ctx, c.ID); err != nil {
		g.log.Warn("mcp: delete the inbox", "connection", c.ID, "err", err)
	}
	if inbox := aideClientID(c); inbox != "" {
		g.Forget(ctx, c.OwnerID, inbox)
	}
	g.log.Info("mcp: connection ended", "connection", c.ID, "state", state, "reason", reason)
	return c, nil
}

// grantExpiry is when a connection's grant expires.
func (g *Gateway) grantExpiry(c *Connection) (time.Time, bool) {
	s := &identityv1.Signed{}
	gr := &identityv1.Grant{}
	if proto.Unmarshal(c.Grant, s) != nil || identity.Unverified(s, gr) != nil || gr.ExpiresAt == nil {
		return time.Time{}, false
	}
	return gr.ExpiresAt.AsTime(), true
}

// identity is the client identity CLI commands run with.
func (g *Gateway) identity(c *Connection) (cli.Identity, error) {
	key, err := g.key(c)
	if err != nil {
		return cli.Identity{}, err
	}
	rc := &identityv1.RootChain{}
	grant := &identityv1.Signed{}
	if err := proto.Unmarshal(c.RootChain, rc); err != nil {
		return cli.Identity{}, err
	}
	if err := proto.Unmarshal(c.Grant, grant); err != nil {
		return cli.Identity{}, err
	}
	id := cli.Identity{RelayURL: relayURL, OwnerID: c.OwnerID, Key: key, RootChain: rc, Grant: grant}
	if in, priv, err := g.inbox(context.Background(), c); err == nil {
		id.InboxKey, id.TakesMessages, id.InboxRegistered = priv, in.takes(), in.Registered
	} else {
		g.log.Warn("mcp: the connection's inbox", "connection", c.ID, "err", err)
	}
	return id, nil
}

// adopted stores the grant (or root chain) a command picked up.
func (g *Gateway) adopted(id string) func(*identityv1.RootChain, *identityv1.Signed) {
	return func(rc *identityv1.RootChain, grant *identityv1.Signed) {
		ctx := context.Background()
		l := g.connLock(id)
		l.Lock()
		defer l.Unlock()
		c, err := g.db.GetConnection(ctx, id)
		if err != nil || c == nil || c.State != StateActive {
			return
		}
		c.RootChain, _ = proto.Marshal(rc)
		c.Grant, _ = proto.Marshal(grant)
		g.db.PutConnection(ctx, c)
	}
}

// touch records that a connection was used (at most once a minute).
func (g *Gateway) touch(ctx context.Context, c *Connection) {
	now := g.cfg.Now()
	if now.Sub(c.LastUsed) < time.Minute {
		return
	}
	l := g.connLock(c.ID)
	l.Lock()
	defer l.Unlock()
	cur, err := g.db.GetConnection(ctx, c.ID)
	if err != nil || cur == nil {
		return
	}
	cur.LastUsed = now
	g.db.PutConnection(ctx, cur)
}

// ---------------------------------------------------------------------------
// What a connection waits for, in words.

// Status is a connection's state and what to do next.
type Status struct {
	State string `json:"state"`
	// Next is what the person should do, "" when nothing.
	Next string `json:"next"`
}

func (g *Gateway) status(c *Connection) Status {
	s := Status{State: c.State}
	switch c.State {
	case StateRevoked:
		s.Next = "This connection was disconnected (" + c.EndReason + "). Connect the app to Aide again, logging in with the account."
	case StateExpired:
		s.Next = "This connection ended (" + c.EndReason + "). Connect the app to Aide again."
	}
	return s
}
