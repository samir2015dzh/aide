package relay

import (
	"time"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	"github.com/samir2015dzh/aide/pkg/identity"
)

// An agent whose key a server holds (an MCP connection, Grant.custodian)
// has no connection of its own: the gateway connects for it only while it
// calls. Online for it is what it did lately (工单 #94), as MQTT counts any
// packet within 1.5 keepalives and Matrix keeps a user online for minutes
// after their last action: its last call within ActiveWindow. An agent with
// a connection of its own stays online exactly while connected.
//
// ActiveWindow is twice the five minutes a stateless MCP client is expected
// to call at least that often (an agent checking its inbox; the gateway
// itself notes a call at most once a minute): one missed call keeps it
// online, two say it is gone. Past it the agent is offline with when it
// last called.
const ActiveWindow = 10 * time.Minute

// ClientActive records that the agent clientID of ownerID did something at
// at (a call through the MCP gateway). Owners see it at once when that
// changes what they show (online again, or its last call a minute later),
// and again when the window closes with nothing since.
func (s *Server) ClientActive(ownerID, clientID string, at time.Time) {
	if clientID == "" {
		return
	}
	t, err := s.tenantFor(ownerID)
	if err != nil {
		return
	}
	s.mu.Lock()
	prev, ok := t.lastSeen[clientID]
	if ok && !at.After(prev) {
		s.mu.Unlock()
		return
	}
	t.lastSeen[clientID] = at
	if !ok || at.Sub(prev) >= time.Minute {
		s.wakeOwnerLocked(t)
	}
	s.mu.Unlock()
	time.AfterFunc(time.Until(at.Add(ActiveWindow))+time.Second, func() { s.wakeOwner(t) })
}

// custodied is the owner's agents whose key a server holds, by client id.
func (s *Server) custodied(t *tenant) map[string]bool {
	out := map[string]bool{}
	grants, err := s.store.Grants(t.ownerID)
	if err != nil {
		return out
	}
	for _, sg := range grants {
		g := &identityv1.Grant{}
		if identity.Unverified(sg, g) == nil && g.Custodian != "" {
			out[g.ClientId] = true
		}
	}
	return out
}

// recentlyActive reports whether an agent with no connection now is online
// by its last call (an agent whose key a server holds, its call within
// ActiveWindow). s.mu is held.
func recentlyActive(t *tenant, clientID string, custodied bool, now time.Time) bool {
	seen, ok := t.lastSeen[clientID]
	return custodied && ok && now.Sub(seen) < ActiveWindow
}
