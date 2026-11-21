package machine

import (
	"context"
	"testing"
	"time"

	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
)

// A message for information only (docs/agent-messages.md §15) starts no
// turn: it waits, and the next turn, whoever's, takes it, as that turn's
// asker's.
func TestFYIWaitsForTheNextTurn(t *testing.T) {
	h := newHarness(t, DefaultLimits())
	id := h.start("say hi")
	h.wait(id, sessionv1.SessionStatus_SESSION_STATUS_IDLE)
	turns := len(h.events(id))
	ctx := context.Background()
	deliver := func(text string, fyi bool) string {
		t.Helper()
		_, delivered, err := h.s.Deliver(ctx, h.p, id, "", text, &sessionv1.MessageReceived{MessageId: "msg_" + text, Text: text}, nil, fyi)
		if err != nil {
			t.Fatal(err)
		}
		return delivered
	}
	if d := deliver("note the build moved to ci-2", true); d != "queued" {
		t.Fatalf("fyi delivered %q, want queued", d)
	}
	time.Sleep(200 * time.Millisecond)
	if st := h.lastStatus(id); st != sessionv1.SessionStatus_SESSION_STATUS_IDLE {
		t.Fatalf("an fyi started a turn: %v", st)
	}
	h.s.mu.Lock()
	l := h.s.live[id]
	h.s.mu.Unlock()
	l.mu.Lock()
	waiting := len(l.fyi)
	l.mu.Unlock()
	if waiting != 1 {
		t.Fatalf("%d fyi waiting, want 1 (events since: %d)", waiting, len(h.events(id))-turns)
	}
	if d := deliver("say done", false); d != "turn" {
		t.Fatalf("a message delivered %q, want turn", d)
	}
	h.wait(id, sessionv1.SessionStatus_SESSION_STATUS_IDLE)
	l.mu.Lock()
	waiting, asker := len(l.fyi), l.asker
	l.mu.Unlock()
	if waiting != 0 || asker != h.p.ID() {
		t.Fatalf("after the next turn: %d fyi waiting, asker %q", waiting, asker)
	}
}
