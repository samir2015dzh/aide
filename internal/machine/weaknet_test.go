package machine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

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

// userMessagesWith are the session's UserMessage events that carry the
// client_message_id cmid.
func (h *harness) userMessagesWith(id, cmid string) []*sessionv1.UserMessage {
	var out []*sessionv1.UserMessage
	for _, e := range h.events(id) {
		if u := e.GetUserMessage(); u != nil && slices.Contains(u.ClientMessageIds, cmid) {
			out = append(out, u)
		}
	}
	return out
}

// attachmentDirs lists the directories newMessage made for the session.
func (h *harness) attachmentDirs(id string) []string {
	ents, _ := os.ReadDir(h.s.attachmentsDir(id))
	var out []string
	for _, e := range ents {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

// The UserMessage a send records names its client_message_id, so the
// sender's outbox knows it arrived although the recorded text differs
// (here: the attached files listed after it); a resend of it saves no
// second copy of its files.
func TestResendWithAttachmentsRecordsOneMessageAndOneDir(t *testing.T) {
	h := newHarness(t, Limits{})
	id := h.start("say first")
	h.wait(id, sessionv1.SessionStatus_SESSION_STATUS_IDLE)
	atts := []*sessionv1.Attachment{{Name: "notes.txt", Data: []byte("some notes\n")}}
	for i := 0; i < 2; i++ {
		if _, err := h.s.SendWithID(context.Background(), h.p, id, "say second", atts, "cmid-att", 1); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
		h.wait(id, sessionv1.SessionStatus_SESSION_STATUS_IDLE)
	}
	got := h.userMessagesWith(id, "cmid-att")
	if len(got) != 1 {
		t.Fatalf("user messages with the id: %d\n%s", len(got), h.dump(id))
	}
	if !strings.HasPrefix(got[0].Text, "say second\n\n[Aide] Attached files") {
		t.Fatalf("recorded text: %q", got[0].Text)
	}
	if dirs := h.attachmentDirs(id); len(dirs) != 1 {
		t.Fatalf("attachment dirs after a resend: %v", dirs)
	}
}

// A message sent while the agent waits answers its question: the
// QuestionResolved it records names the send's client_message_id (no
// UserMessage is recorded for it).
func TestAnswerBySendEchoesClientMessageID(t *testing.T) {
	h := newHarness(t, DefaultLimits())
	id := h.start("ask Color|Which color?|Red,Blue")
	h.wait(id, waiting)
	if _, err := h.s.SendWithID(context.Background(), h.p, id, "red", nil, "cmid-answer", 1); err != nil {
		t.Fatal(err)
	}
	h.wait(id, idle)
	r := h.resolved(id)
	if len(r) != 1 || !slices.Equal(r[0].ClientMessageIds, []string{"cmid-answer"}) {
		t.Fatalf("resolved: %v", r)
	}
}

// A resend that comes while the first send of the same id is still being
// delivered waits for it: when the first fails, the resend is not told
// it was delivered, it delivers the message itself.
func TestConcurrentResendWaitsForFirstOutcome(t *testing.T) {
	h := newHarness(t, Limits{})
	id := h.start("say first")
	h.wait(id, sessionv1.SessionStatus_SESSION_STATUS_IDLE)

	// The first send: it holds the id and has reserved it, then fails.
	key := h.p.ID() + "\x00" + id + "\x00" + "cmid-race"
	release, err := h.s.holdSend(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if dup, err := h.s.st.ReserveSentMessage(h.p.ID(), id, "cmid-race", 1); err != nil || dup {
		t.Fatalf("reserve: %v %v", dup, err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := h.s.SendWithID(context.Background(), h.p, id, "say second", nil, "cmid-race", 1)
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("the resend returned (%v) while the first was in flight", err)
	case <-time.After(200 * time.Millisecond):
	}

	h.s.st.ForgetSentMessage(h.p.ID(), id, "cmid-race")
	release()
	if err := <-done; err != nil {
		t.Fatalf("resend after the first failed: %v", err)
	}
	h.wait(id, sessionv1.SessionStatus_SESSION_STATUS_IDLE)
	if got := h.userMessagesWith(id, "cmid-race"); len(got) != 1 {
		t.Fatalf("user messages with the id: %d\n%s", len(got), h.dump(id))
	}

	// Waiting is cut short by the caller with an error a client retries.
	release, err = h.s.holdSend(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = h.s.SendWithID(ctx, h.p, id, "say third", nil, "cmid-race", 1)
	if connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatalf("resend while in flight, cut short: %v", err)
	}
}

func TestClientMessageIDIsBounded(t *testing.T) {
	h := newHarness(t, Limits{})
	id := h.start("say first")
	h.wait(id, sessionv1.SessionStatus_SESSION_STATUS_IDLE)
	_, err := h.s.SendWithID(context.Background(), h.p, id, "say second", nil, strings.Repeat("x", maxClientMessageID+1), 1)
	var ce *connect.Error
	if !errors.As(err, &ce) || ce.Code() != connect.CodeInvalidArgument {
		t.Fatalf("over-long client_message_id: %v", err)
	}
}
