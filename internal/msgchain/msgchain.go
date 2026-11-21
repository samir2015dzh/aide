// Package msgchain keeps chains of messages between agents from running
// away (docs/agent-messages.md §15): each message carries its chain's
// trace_id and its hop in it, and a recipient holds, for the owner, one
// that has gone too far.
package msgchain

import (
	"crypto/rand"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

const (
	// MaxHops is the longest chain a recipient takes: a message at a hop
	// beyond it is held.
	MaxHops = 4
	// MaxPasses is how often a chain may reach one endpoint within Window;
	// beyond it, it goes round, and is held.
	MaxPasses = 3
	Window    = time.Hour
)

// New starts a chain: a message that answers nothing, or the owner's.
func New() (traceID string, hop uint32) {
	return "tr_" + strings.ToLower(ulid.MustNew(ulid.Now(), rand.Reader).String()), 1
}

// Next is the trace and hop of a message written because of one in the
// chain traceID at hop: the same chain, one hop more. A parent with no
// trace (from before chains) starts one.
func Next(traceID string, hop uint32) (string, uint32) {
	if traceID == "" {
		return New()
	}
	return traceID, hop + 1
}

// Guard is a recipient's record of the chains that reached each of its
// endpoints. It is the recipient's own, in memory: a restart forgets it,
// and the relay's per-pair limits stay the floor (docs/agent-messages.md
// §5).
type Guard struct {
	mu   sync.Mutex
	seen map[string][]time.Time // endpoint + "\x00" + trace → when it came
}

// Check records a message of chain traceID at hop reaching endpoint at now,
// and says why to hold it, or "" to take it.
func (g *Guard) Check(endpoint, traceID string, hop uint32, now time.Time) string {
	if hop > MaxHops {
		return "the chain of messages it belongs to has gone more than 4 hops without the owner"
	}
	if traceID == "" {
		return ""
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.seen == nil {
		g.seen = map[string][]time.Time{}
	}
	key := endpoint + "\x00" + traceID
	var recent []time.Time
	for _, at := range g.seen[key] {
		if now.Sub(at) < Window {
			recent = append(recent, at)
		}
	}
	recent = append(recent, now)
	g.seen[key] = recent
	if len(g.seen) > 10000 { // forget the old chains
		for k, ats := range g.seen {
			if now.Sub(ats[len(ats)-1]) >= Window {
				delete(g.seen, k)
			}
		}
	}
	if len(recent) > MaxPasses {
		return "the chain of messages it belongs to came round to this endpoint more than 3 times in an hour"
	}
	return ""
}
