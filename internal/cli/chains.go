package cli

import (
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/samir2015dzh/aide/internal/msgchain"
)

// What this agent keeps of the chains of messages (docs/agent-messages.md
// §15), in its home: the chain each message it took belongs to, so that a
// reply goes on with it; when a reply came to a message it sent (replied,
// which only the sender sees); and when each chain reached it, to hold one
// that goes round.

type chainRec struct {
	Trace string    `json:"trace"`
	Hop   uint32    `json:"hop"`
	At    time.Time `json:"at"`
}

type chainBook struct {
	Traces  map[string]chainRec    `json:"traces"`
	Replied map[string]time.Time   `json:"replied"`
	Passes  map[string][]time.Time `json:"passes"`
}

// chainsPath is where the book is kept; "" for an agent with no home of
// its own (the MCP gateway's), whose book lasts one command.
func (a *app) chainsPath() string {
	if a.home == "" {
		return ""
	}
	return filepath.Join(a.home, "chains.json")
}

func (a *app) chains() *chainBook {
	b := &chainBook{}
	if p := a.chainsPath(); p != "" {
		if data, err := os.ReadFile(p); err == nil {
			json.Unmarshal(data, b)
		}
	}
	if b.Traces == nil {
		b.Traces = map[string]chainRec{}
	}
	if b.Replied == nil {
		b.Replied = map[string]time.Time{}
	}
	if b.Passes == nil {
		b.Passes = map[string][]time.Time{}
	}
	return b
}

// save keeps the book, forgetting what the post office no longer keeps
// either (31 days) and passes older than the window.
func (a *app) saveChains(b *chainBook, now time.Time) {
	for id, r := range b.Traces {
		if now.Sub(r.At) > 31*24*time.Hour {
			delete(b.Traces, id)
		}
	}
	for id, at := range b.Replied {
		if now.Sub(at) > 31*24*time.Hour {
			delete(b.Replied, id)
		}
	}
	for k, ats := range b.Passes {
		var keep []time.Time
		for _, at := range ats {
			if now.Sub(at) < msgchain.Window {
				keep = append(keep, at)
			}
		}
		if len(keep) == 0 {
			delete(b.Passes, k)
		} else {
			b.Passes[k] = keep
		}
	}
	if p := a.chainsPath(); p != "" {
		if data, err := json.Marshal(b); err == nil {
			os.WriteFile(p, data, 0o600)
		}
	}
}

// took records a message this agent took the first time it sees it: its
// chain, the reply it is, and its pass through endpoint. It says why to
// hold it for the owner, "" to take it.
func (b *chainBook) took(endpoint, id, trace string, hop uint32, inReplyTo string, now time.Time) string {
	if _, seen := b.Traces[id]; seen {
		return "" // listed again: counted once
	}
	b.Traces[id] = chainRec{trace, hop, now}
	if inReplyTo != "" {
		b.Replied[inReplyTo] = now
	}
	var g msgchain.Guard
	key := endpoint + "\x00" + trace
	for _, at := range b.Passes[key] {
		g.Check(endpoint, trace, 1, at)
	}
	why := g.Check(endpoint, trace, hop, now)
	if trace != "" {
		b.Passes[key] = append(b.Passes[key], now)
	}
	return why
}

// next is the chain a message goes on with: the one replyTo was in, one hop
// more, or a new one.
func (b *chainBook) next(replyTo string) (string, uint32) {
	if r, ok := b.Traces[replyTo]; ok && replyTo != "" {
		return msgchain.Next(r.Trace, r.Hop)
	}
	return msgchain.New()
}

// messageIDFor is the message id a send with idempotency key key gets: the
// same for this sender each time, so that a retry is the same message.
func messageIDFor(sender, key string) string {
	if key == "" {
		return "msg_" + newIdempotencyKey()
	}
	sum := sha256.Sum256([]byte(sender + "\x00" + key))
	var id ulid.ULID
	copy(id[:], sum[:16])
	return "msg_" + strings.ToLower(id.String())
}
