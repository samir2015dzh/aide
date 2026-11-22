package machine

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	inboxv1 "github.com/samir2015dzh/aide/gen/go/aide/inbox/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/boundary"
)

// The owner's ruling (丙) holds a message to a session from an agent let in
// less than 7 days ago, or at hop 3 or more of a chain; the information
// boundary holds one that matches a rule for the session's coding agent.
// Held, it waits on the machine until the owner releases it (it reaches the
// session as if never held) or discards it; a day later it expires.
func TestHeldMessages(t *testing.T) {
	h := newHarness(t, Limits{})
	n := h.n
	ctx := context.Background()
	sid := h.start("hello")
	h.wait(sid, sessionv1.SessionStatus_SESSION_STATUS_IDLE)
	now := time.Now()
	grant := func(id string, first time.Time) *identityv1.Grant {
		g := &identityv1.Grant{GrantId: "g_" + id, ClientId: id, ClientLabel: id, MachineIds: []string{"*"}, ProjectIds: []string{"*"}, Agents: []string{"*"},
			TaskVisibility: identityv1.TaskVisibility_TASK_VISIBILITY_ALL}
		if !first.IsZero() {
			g.FirstIssuedAt = timestamppb.New(first)
		}
		return g
	}
	fresh, settled, old := grant("c_fresh", now.Add(-24*time.Hour)), grant("c_settled", now.Add(-30*24*time.Hour)), grant("c_old", time.Time{})
	n.mu.Lock()
	n.grants = append(n.grants, fresh, settled, old)
	n.mu.Unlock()
	letter := func(id, from, text string, hop uint32) *inboxv1.Message {
		return &inboxv1.Message{MessageId: id, From: &inboxv1.Address{Inbox: from}, To: &inboxv1.Address{Inbox: n.machineID, Session: sid}, Text: text, TraceId: "tr_1", Hop: hop}
	}
	// The boundary, as the post office handed it (placeholder rules).
	n.contacts.mu.Lock()
	n.contacts.at = now
	n.contacts.checker = boundary.Compile(&identityv1.BoundaryPolicy{Rules: []*identityv1.BoundaryRule{
		{Category: "example", Patterns: []string{`placeholder-secret`}, RecipientKinds: []string{"fake"}},
		{Category: "elsewhere", Patterns: []string{`placeholder-other`}, RecipientKinds: []string{"codex"}},
	}})
	n.contacts.mu.Unlock()

	for _, c := range []struct {
		g    *identityv1.Grant
		m    *inboxv1.Message
		want string
	}{
		{fresh, letter("msg_01a", "c_fresh", "say hi", 1), "new_agent"},
		{settled, letter("msg_01b", "c_settled", "say hi", 1), ""},
		{old, letter("msg_01c", "c_old", "say hi", 2), ""},
		{old, letter("msg_01d", "c_old", "say hi", 3), "long_chain"},
		{settled, letter("msg_01e", "c_settled", "the placeholder-secret is", 1), "boundary"},
		{settled, letter("msg_01f", "c_settled", "the placeholder-other is", 1), ""},
	} {
		if got, _ := n.holdWhy(c.g, c.m, now); got != c.want {
			t.Errorf("%s: held %q, want %q", c.m.MessageId, got, c.want)
		}
	}
	if _, detail := n.holdWhy(settled, letter("msg_01e", "c_settled", "the placeholder-secret is", 1), now); detail != "it matches the owner's information boundary: example" {
		t.Errorf("a boundary hold says %q (the category, never what matched)", detail)
	}

	// Held, listed, released: it reaches the session.
	if ok, err := n.hold(letter("msg_01a", "c_fresh", "say released", 1), "c_fresh", "Fresh", "new_agent", "new", true); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if ok, _ := n.hold(letter("msg_01a", "c_fresh", "say released", 1), "c_fresh", "Fresh", "new_agent", "new", true); ok {
		t.Fatal("held twice")
	}
	l := n.HeldMessages()
	if len(l.Messages) != 1 || l.Messages[0].FromName != "Fresh" || l.Messages[0].Reason != "new_agent" || l.Messages[0].SessionId != sid ||
		l.Messages[0].ExpiresAt.AsTime().Sub(l.Messages[0].HeldAt.AsTime()) != heldFor {
		t.Fatalf("held: %v", l.Messages)
	}
	if err := n.ReleaseHeldMessage(ctx, "msg_01a", true); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for h.count(sid, func(e *sessionv1.SessionEvent) bool { return e.GetMessageReceived().GetMessageId() == "msg_01a" }) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the released message did not reach the session")
		}
		time.Sleep(20 * time.Millisecond)
	}
	h.wait(sid, sessionv1.SessionStatus_SESSION_STATUS_IDLE)
	if len(n.HeldMessages().Messages) != 0 {
		t.Fatal("still held once released")
	}
	if err := n.ReleaseHeldMessage(ctx, "msg_01a", true); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("released twice: %v", err)
	}

	// Discarded: gone, and it never reaches the session.
	n.hold(letter("msg_01g", "c_old", "say discarded", 3), "c_old", "Old", "long_chain", "hop 3", false)
	if err := n.ReleaseHeldMessage(ctx, "msg_01g", false); err != nil {
		t.Fatal(err)
	}
	if len(n.HeldMessages().Messages) != 0 || h.count(sid, func(e *sessionv1.SessionEvent) bool { return e.GetMessageReceived().GetMessageId() == "msg_01g" }) != 0 {
		t.Fatal("a discarded message")
	}

	// A day later, unreleased: expired.
	n.hold(letter("msg_01h", "c_old", "say late", 3), "c_old", "Old", "long_chain", "hop 3", false)
	n.expireHeld(time.Now().Add(heldFor - time.Minute))
	if len(n.HeldMessages().Messages) != 1 {
		t.Fatal("expired early")
	}
	n.expireHeld(time.Now().Add(heldFor + time.Minute))
	n.heldMu.Lock()
	left := len(n.heldList())
	n.heldMu.Unlock()
	if left != 0 {
		t.Fatal("not expired a day later")
	}

	// A sender's message that matches the boundary for any recipient is
	// refused before it goes.
	n.contacts.mu.Lock()
	n.contacts.checker = boundary.Compile(&identityv1.BoundaryPolicy{Rules: []*identityv1.BoundaryRule{{Category: "everyone", Patterns: []string{`placeholder-all`}}}})
	n.contacts.mu.Unlock()
	err := n.sendMessage(&inboxv1.Message{MessageId: "msg_01i", From: &inboxv1.Address{Inbox: n.machineID, Session: sid}, To: &inboxv1.Address{Inbox: "c_old"}, Text: "placeholder-all here"})
	var be *boundaryError
	if err == nil || !errors.As(err, &be) || be.category != "everyone" {
		t.Fatalf("sent past the boundary: %v", err)
	}
}
