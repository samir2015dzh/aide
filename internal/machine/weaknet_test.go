package machine

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
)

func TestSendWithIDDeduplicatesWeakNetworkResend(t *testing.T) {
	h := newHarness(t, Limits{})
	id := h.start("say first")
	h.wait(id, sessionv1.SessionStatus_SESSION_STATUS_IDLE)

	// First delivery with client_message_id should trigger a new turn.
	if _, err := h.s.SendWithID(context.Background(), h.p, id, "say second", nil, "msg-001", 1); err != nil {
		t.Fatalf("first SendWithID: %v", err)
	}
	h.wait(id, sessionv1.SessionStatus_SESSION_STATUS_IDLE)

	evsBefore := h.events(id)
	userCountBefore := 0
	for _, e := range evsBefore {
		if u := e.GetUserMessage(); u != nil && u.Text == "say second" {
			userCountBefore++
		}
	}
	if userCountBefore != 1 {
		t.Fatalf("expected 1 user message for 'say second', got %d", userCountBefore)
	}

	// Duplicate resend (e.g. client timed out before receiving ack) must be a no-op.
	sess, err := h.s.SendWithID(context.Background(), h.p, id, "say second", nil, "msg-001", 1)
	if err != nil {
		t.Fatalf("duplicate SendWithID: %v", err)
	}
	if sess.SessionId != id {
		t.Fatalf("expected session %s, got %s", id, sess.SessionId)
	}

	// Wait briefly to confirm no second turn was started.
	time.Sleep(100 * time.Millisecond)
	evsAfter := h.events(id)
	userCountAfter := 0
	for _, e := range evsAfter {
		if u := e.GetUserMessage(); u != nil && u.Text == "say second" {
			userCountAfter++
		}
	}
	if userCountAfter != 1 {
		t.Fatalf("duplicate resend created extra user message: got %d", userCountAfter)
	}

	// A new client_message_id on the same session must still be delivered in order.
	if _, err := h.s.SendWithID(context.Background(), h.p, id, "say third", nil, "msg-002", 2); err != nil {
		t.Fatalf("second distinct SendWithID: %v", err)
	}
	h.wait(id, sessionv1.SessionStatus_SESSION_STATUS_IDLE)
	evsFinal := h.events(id)
	foundThird := false
	for _, e := range evsFinal {
		if u := e.GetUserMessage(); u != nil && u.Text == "say third" {
			foundThird = true
		}
	}
	if !foundThird {
		t.Fatalf("expected 'say third' user message after distinct client_message_id")
	}
}

func TestReserveSentMessagePersistsAcrossStoreReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "machine.db")
	st, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	dup, err := st.ReserveSentMessage("owner", "sess-1", "cmid-1", 1)
	if err != nil || dup {
		t.Fatalf("first ReserveSentMessage: dup=%v err=%v", dup, err)
	}
	dup, err = st.ReserveSentMessage("owner", "sess-1", "cmid-1", 1)
	if err != nil || !dup {
		t.Fatalf("second ReserveSentMessage: dup=%v err=%v", dup, err)
	}
	st.Close()

	// Reopen DB (simulates daemon restart) and verify dedup survives.
	st2, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("reopen Store: %v", err)
	}
	defer st2.Close()
	dup, err = st2.ReserveSentMessage("owner", "sess-1", "cmid-1", 1)
	if err != nil || !dup {
		t.Fatalf("after reopen ReserveSentMessage: dup=%v err=%v", dup, err)
	}

	// ForgetSentMessage releases the reservation so retry works after a pre-accept failure.
	st2.ForgetSentMessage("owner", "sess-1", "cmid-1")
	dup, err = st2.ReserveSentMessage("owner", "sess-1", "cmid-1", 1)
	if err != nil || dup {
		t.Fatalf("after ForgetSentMessage ReserveSentMessage: dup=%v err=%v", dup, err)
	}
}

func TestStreamReconnectCatchUpNoGapNoDuplicate(t *testing.T) {
	h := newHarness(t, Limits{})
	id := h.start("say alpha")
	h.wait(id, sessionv1.SessionStatus_SESSION_STATUS_IDLE)

	// Read first wave of events (simulating connection up to turn 1).
	var got []*sessionv1.SessionEvent
	err := h.s.Stream(context.Background(), h.p, &sessionv1.StreamEventsRequest{
		SessionId: id,
		AfterSeq:  0,
		Follow:    false,
	}, func(e *sessionv1.SessionEvent) error {
		got = append(got, e)
		return nil
	})
	if err != nil {
		t.Fatalf("initial Stream: %v", err)
	}
	if len(got) == 0 {
		t.Fatalf("expected events from turn 1")
	}
	disconnectSeq := got[len(got)-1].Seq

	// Client is offline while turns 2 and 3 run on the machine.
	if _, err := h.s.SendWithID(context.Background(), h.p, id, "say beta", nil, "cmid-2", 2); err != nil {
		t.Fatalf("send beta: %v", err)
	}
	h.wait(id, sessionv1.SessionStatus_SESSION_STATUS_IDLE)
	if _, err := h.s.SendWithID(context.Background(), h.p, id, "say gamma", nil, "cmid-3", 3); err != nil {
		t.Fatalf("send gamma: %v", err)
	}
	h.wait(id, sessionv1.SessionStatus_SESSION_STATUS_IDLE)

	// Reconnect reporting last received seq; must receive exact missed tail.
	var caughtUp []*sessionv1.SessionEvent
	err = h.s.Stream(context.Background(), h.p, &sessionv1.StreamEventsRequest{
		SessionId: id,
		AfterSeq:  disconnectSeq,
		Follow:    false,
	}, func(e *sessionv1.SessionEvent) error {
		caughtUp = append(caughtUp, e)
		return nil
	})
	if err != nil {
		t.Fatalf("catch-up Stream: %v", err)
	}
	if len(caughtUp) == 0 {
		t.Fatalf("expected missed events after seq %d", disconnectSeq)
	}

	combined := append(got, caughtUp...)
	full := h.events(id)
	if len(combined) != len(full) {
		t.Fatalf("combined event count %d != full event count %d", len(combined), len(full))
	}
	for i := range full {
		if combined[i].Seq != full[i].Seq {
			t.Fatalf("event[%d] seq mismatch: combined=%d full=%d", i, combined[i].Seq, full[i].Seq)
		}
	}
}
