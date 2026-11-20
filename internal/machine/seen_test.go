package machine

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/sessionstate"
)

// What the owner has seen of a session is kept on its machine
// (docs/session-states.md §2, §7): a finished turn is done_unseen until a
// device of the owner opens it; a question, told of by a desktop, then
// with the owner there. Only the owner marks; marking nothing new writes
// nothing; it survives a restart of the store.
func TestMarkSessionSeen(t *testing.T) {
	h := newHarness(t, DefaultLimits())
	ctx := context.Background()
	owner := &Principal{Kind: PrincipalOwner}
	id := h.start("say one")
	s := h.wait(id, idle)
	if st := sessionstate.StateOf(s); st != sessionv1.SessionState_SESSION_STATE_DONE_UNSEEN {
		t.Fatalf("a finished turn nobody opened: %v (seen %v)", st, s.Seen)
	}
	if _, err := h.s.MarkSeen(ctx, h.p, &sessionv1.MarkSessionSeenRequest{SessionId: id, Opened: true}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("an agent marks: %v", err)
	}
	s, err := h.s.MarkSeen(ctx, owner, &sessionv1.MarkSessionSeenRequest{SessionId: id, Opened: true})
	if err != nil || s.Seen.GetTurns() != 1 || sessionstate.StateOf(s) != sessionv1.SessionState_SESSION_STATE_DONE {
		t.Fatalf("opened: %v %v", s, err)
	}
	if r, _ := h.s.st.Session(id); r.Seen.GetTurns() != 1 {
		t.Fatalf("stored seen %v", r.Seen)
	}

	// A question: told of by a desktop, the owner away; then there.
	q := h.start("ask Color|Which color?|Red,Blue\nsay done")
	w := h.wait(q, waiting)
	qid := w.Progress.GetPendingQuestion().GetQuestionId()
	if pending, seen := h.s.questionSeen(q); pending != qid || seen.GetQuestionId() != "" {
		t.Fatalf("before: %q %v", pending, seen)
	}
	h.s.MarkSeen(ctx, owner, &sessionv1.MarkSessionSeenRequest{SessionId: q, QuestionId: "other"})
	if _, seen := h.s.questionSeen(q); seen.GetQuestionId() != "" {
		t.Fatalf("a question not pending was marked: %v", seen)
	}
	h.s.MarkSeen(ctx, owner, &sessionv1.MarkSessionSeenRequest{SessionId: q, QuestionId: qid})
	if _, seen := h.s.questionSeen(q); seen.GetQuestionId() != qid || seen.GetQuestionPresent() {
		t.Fatalf("told of, owner away: %v", seen)
	}
	h.s.MarkSeen(ctx, owner, &sessionv1.MarkSessionSeenRequest{SessionId: q, QuestionId: qid, Present: true})
	before, _ := h.s.st.Session(q)
	if !before.Seen.GetQuestionPresent() {
		t.Fatalf("owner there: %v", before.Seen)
	}
	// Told again with the owner away: the owner was there all the same.
	if s, _ := h.s.MarkSeen(ctx, owner, &sessionv1.MarkSessionSeenRequest{SessionId: q, QuestionId: qid}); !s.Seen.GetQuestionPresent() {
		t.Fatalf("presence forgotten: %v", s.Seen)
	}
	h.sendNow(q, "Blue")
	if s := h.wait(q, idle); sessionstate.StateOf(s) != sessionv1.SessionState_SESSION_STATE_DONE_UNSEEN || s.Seen.GetQuestionId() != qid {
		t.Fatalf("answered and finished: %v %v", sessionstate.StateOf(s), s.Seen)
	}
}
