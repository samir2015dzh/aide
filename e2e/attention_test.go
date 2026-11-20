//go:build unix

package e2e

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"
	"time"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/sessionstate"
	"github.com/samir2015dzh/aide/pkg/identity"
)

// TestAttentionTriage (docs/session-states.md §2, §7), as the owner lives
// it, with the owner's Mac (its aided, as the desktop app talks to it) and
// a phone (a mobile core, as the phone apps run it) both following the
// owner's world:
//
//   - an agent's finished turn is done_unseen everywhere, the agent's CLI
//     included, until a device of the owner opens it; opened on the phone,
//     the Mac's screen has it done, and the other way round: the machine
//     keeps what the owner saw, and every device reads it from there;
//   - a question goes to the desktop first: phones are told only once no
//     desktop said it told the owner within the grace; when one did, the
//     owner away, once it is still unanswered at the escalation; never
//     once it is answered meanwhile, or the owner was at the desktop.
//
// The grace and the escalation are 5 s and 15 s here (10 s and 3 min as
// shipped).
func TestAttentionTriage(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	const grace, escalate = 5 * time.Second, 15 * time.Second
	fake := filepath.Join(binaries(t), "fakeacp")
	w := newOwnerMac(t, []string{"AIDE_GEMINI_BIN=" + fake, "FAKE_ACP_STATE=" + filepath.Join(t.TempDir(), "fakeacp"),
		"AIDE_QUESTION_DESKTOP_GRACE=" + grace.String(), "AIDE_QUESTION_ESCALATE_AFTER=" + escalate.String()},
		&localv1.GrantSpec{Agents: []string{"gemini"}})
	spool := filepath.Join(t.TempDir(), "push")
	w.relayArgs = append(w.relayArgs, "--push-spool", spool)
	w.restartRelay()
	eventually(t, 30*time.Second, "the machine is back on the relay", func() bool {
		r := w.cli(30*time.Second, "project", "list")
		return r.code == 0 && strings.Contains(r.stdout, "owner-mac/demo")
	})
	ctx := context.Background()
	desk := w.app.client
	st, err := desk.GetStatus(ctx, &localv1.GetStatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	phone := joinMobile(t, w, st.RelayUrl, t.TempDir())
	deskScreen, phoneScreen := watchScreenOf(ctx, t, desk), watchScreenOf(ctx, t, phone.client)
	// A phone registered for questions: what the machine hands the relay for it is spooled.
	key, _ := ecdh.X25519().GenerateKey(rand.Reader)
	eventually(t, 30*time.Second, "the phone registered for notices", func() bool {
		_, err := phone.client.RegisterPush(ctx, &localv1.RegisterPushRequest{Token: "abc123", Environment: "development",
			NoticePublicKey: key.PublicKey().Bytes(), Kinds: []string{"question"}})
		return err == nil
	})
	time.Sleep(time.Second) // the machine hears of the registration over its update stream

	stateOn := func(scr *screen, id string) func() sessionv1.SessionState {
		return func() sessionv1.SessionState {
			scr.mu.Lock()
			defer scr.mu.Unlock()
			if s := scr.sessions[id]; s != nil {
				return sessionstate.StateOf(s)
			}
			return sessionv1.SessionState_SESSION_STATE_UNSPECIFIED
		}
	}
	reads := func(id string, want sessionv1.SessionState, where ...*screen) {
		t.Helper()
		for _, scr := range where {
			eventually(t, 30*time.Second, id+" reads "+sessionstate.StateName(want), func() bool { return stateOn(scr, id)() == want })
		}
	}
	unseen, done := sessionv1.SessionState_SESSION_STATE_DONE_UNSEEN, sessionv1.SessionState_SESSION_STATE_DONE

	// An agent's turn ends: a new result, on both screens and to the CLI.
	s, r := w.startSession(time.Minute, "--agent", "gemini", "--title", "写 hello", "--prompt", "say 好了", "--wait")
	if s.SessionID == "" {
		t.Fatalf("start: %+v", r)
	}
	reads(s.SessionID, unseen, deskScreen, phoneScreen)
	if got := w.get(s.SessionID); got.State != "done_unseen" {
		t.Fatalf("the CLI: %+v", got)
	}
	// Opened on the phone: done on the Mac too.
	if _, err := phone.client.MarkSessionSeen(ctx, &sessionv1.MarkSessionSeenRequest{SessionId: s.SessionID, Opened: true}); err != nil {
		t.Fatalf("opened on the phone: %v", err)
	}
	reads(s.SessionID, done, deskScreen, phoneScreen)
	// Another turn: new again; opened on the Mac, done on the phone.
	if _, r := w.send(time.Minute, s.SessionID, "say 又好了"); r.code != 0 {
		t.Fatalf("send: %+v", r)
	}
	reads(s.SessionID, unseen, deskScreen, phoneScreen)
	opened, err := desk.MarkSessionSeen(ctx, &sessionv1.MarkSessionSeenRequest{SessionId: s.SessionID, Opened: true})
	if err != nil || opened.Session.GetSeen().GetTurns() != 2 {
		t.Fatalf("opened on the Mac: %v %v", opened, err)
	}
	reads(s.SessionID, done, deskScreen, phoneScreen)
	// Opened again: nothing new, nothing written (its place in lists kept; the store keeps whole seconds).
	again, err := desk.MarkSessionSeen(ctx, &sessionv1.MarkSessionSeenRequest{SessionId: s.SessionID, Opened: true})
	if err != nil || again.Session.UpdatedAt.GetSeconds() != opened.Session.UpdatedAt.GetSeconds() {
		t.Fatalf("opened again: %v %v", again, err)
	}

	// Questions. What the phone was told, by session, and when.
	pushed := map[string]time.Time{}
	seenFiles := map[string]bool{}
	readPushes := func() {
		for _, x := range readSpool(t, spool, seenFiles) {
			sealed, _ := base64.StdEncoding.DecodeString(x.Payload.Aide.N)
			n := &sessionv1.Notice{}
			if err := identity.OpenNotice(key.Bytes(), sealed, n); err != nil {
				t.Fatalf("open notice: %v", err)
			}
			if n.Kind == "question" {
				if _, ok := pushed[n.SessionId]; !ok {
					pushed[n.SessionId] = time.Now()
				}
			}
		}
	}
	ask := func(title string) (string, *sessionv1.Question, time.Time) {
		t.Helper()
		at := time.Now()
		s, r := w.startSession(time.Minute, "--agent", "gemini", "--title", title, "--prompt", "ask 选哪个|红,蓝\nsay 好", "--wait")
		if s.SessionID == "" {
			t.Fatalf("start: %+v", r)
		}
		res, err := desk.ListSessions(ctx, &localv1.ListSessionsRequest{})
		if err != nil {
			t.Fatal(err)
		}
		for _, x := range res.Sessions {
			if x.SessionId == s.SessionID && x.Progress.GetPendingQuestion() != nil {
				return s.SessionID, x.Progress.PendingQuestion, at
			}
		}
		t.Fatalf("no question on %s", s.SessionID)
		return "", nil, at
	}
	// The desktop tells the machine it told the owner of it (the app's main process: questionAcks).
	told := func(id string, q *sessionv1.Question, present bool) {
		t.Helper()
		if _, err := desk.MarkSessionSeen(ctx, &sessionv1.MarkSessionSeenRequest{SessionId: id, QuestionId: q.QuestionId, Present: present}); err != nil {
			t.Fatalf("told: %v", err)
		}
	}
	// What the phone app decides on (LiveNotices.questionDue): what the machine keeps, as its Watch brings it.
	phoneHas := func(id, qid string, present bool) {
		t.Helper()
		eventually(t, 30*time.Second, "the phone's view of "+id, phoneScreen.has(func(s *screen) bool {
			x := s.sessions[id]
			return x != nil && x.GetSeen().GetQuestionId() == qid && x.GetSeen().GetQuestionPresent() == present
		}))
	}

	// No desktop told of it: the phone, once the grace is over (not before).
	alone, _, aloneAt := ask("没人告诉")
	eventually(t, 30*time.Second, "the phone told of a question no desktop took", func() bool { readPushes(); _, ok := pushed[alone]; return ok })
	if d := pushed[alone].Sub(aloneAt); d < grace {
		t.Fatalf("the phone told after %v, before the desktop's grace", d)
	}

	// Told by the desktop, the owner away; told, then answered; told with the owner there.
	away, awayQ, awayAt := ask("人不在")
	told(away, awayQ, false)
	answered, answeredQ, _ := ask("答了")
	told(answered, answeredQ, false)
	there, thereQ, _ := ask("人在")
	told(there, thereQ, true)
	phoneHas(away, awayQ.QuestionId, false)
	phoneHas(there, thereQ.QuestionId, true)
	if _, err := desk.AnswerQuestion(ctx, &localv1.AnswerQuestionRequest{SessionId: answered, QuestionId: answeredQ.QuestionId,
		Answers: []*sessionv1.Answer{{ItemId: answeredQ.Items[0].Id, Selected: []string{"红"}}}}); err != nil {
		t.Fatalf("answer: %v", err)
	}
	// Past the grace: none of them yet.
	time.Sleep(time.Until(awayAt.Add(grace + 2*time.Second)))
	readPushes()
	for _, id := range []string{away, answered, there} {
		if _, ok := pushed[id]; ok {
			t.Fatalf("%s told to the phone within the grace though the desktop had it", id)
		}
	}
	// The one still waiting, the owner away: at the escalation.
	eventually(t, 30*time.Second, "the unanswered question escalated to the phone", func() bool { readPushes(); _, ok := pushed[away]; return ok })
	if d := pushed[away].Sub(awayAt); d < escalate {
		t.Fatalf("escalated after %v, before %v", d, escalate)
	}
	// The answered one, and the one the owner was there for: never.
	time.Sleep(3 * time.Second)
	readPushes()
	for _, id := range []string{answered, there} {
		if _, ok := pushed[id]; ok {
			t.Fatalf("%s told to the phone", id)
		}
	}
}
