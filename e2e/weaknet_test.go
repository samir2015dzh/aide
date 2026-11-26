package e2e

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
)

// The app's outbox (工单 #115), through the relay to another machine: a
// message sent with a client_message_id and a file, then sent again with
// the same id (its answer was lost on a weak network), is taken once, and
// what the session records of it names the id although its text has the
// file listed after it; a message sent while the agent waits answers its
// question, and the answer names its id too. That is what the outbox takes
// a message for delivered by.
func TestOutboxResendThroughRelay(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	env := []string{"AIDE_FAKE_PROVIDERS=1"}
	w := newOwnerMac(t, env, &localv1.GrantSpec{})
	ctx := context.Background()
	app := w.app.client

	home, err := app.GetStatus(ctx, &localv1.GetStatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	other, _ := w.otherMac("work-mac", env)
	far := w.logInMachineOnly(other, home, "work-mac").LocalMachineId
	if _, err := other.AddProject(ctx, &localv1.AddProjectRequest{MachineId: far, ProjectId: "billing", Path: gitRepo(t, map[string]string{"README.md": "# billing\n"})}); err != nil {
		t.Fatal(err)
	}
	var s *sessionv1.Session
	eventually(t, time.Minute, "a session on work-mac", func() bool {
		res, err := app.StartSession(ctx, &localv1.StartSessionRequest{MachineId: far, Session: &sessionv1.StartSessionRequest{ProjectId: "billing", Agent: "claude", Prompt: "say first"}})
		s = res.GetSession()
		return err == nil
	})
	events := func() []*sessionv1.SessionEvent {
		res, err := app.ListSessionEvents(ctx, &localv1.ListSessionEventsRequest{SessionId: s.SessionId, Limit: 1000})
		if err != nil {
			return nil
		}
		return res.Events
	}
	idle := func(turns uint32) {
		t.Helper()
		eventually(t, time.Minute, "turn done", func() bool {
			res, err := app.ListSessions(ctx, &localv1.ListSessionsRequest{})
			if err != nil {
				return false
			}
			for _, x := range res.Sessions {
				if x.SessionId == s.SessionId {
					return x.Status == sessionv1.SessionStatus_SESSION_STATUS_IDLE && x.Progress.GetTurnCount() >= turns
				}
			}
			return false
		})
	}
	idle(1)

	send := &localv1.SendMessageRequest{SessionId: s.SessionId, Text: "say second", ClientMessageId: "cmid-e2e-1", ClientSeq: 1,
		Attachments: []*sessionv1.Attachment{{Name: "notes.txt", Data: []byte("some notes\n")}}}
	for i := 0; i < 2; i++ {
		if _, err := app.SendMessage(ctx, send); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}
	idle(2)
	var echoed []*sessionv1.UserMessage
	for _, e := range events() {
		if u := e.GetUserMessage(); u != nil && slices.Contains(u.ClientMessageIds, "cmid-e2e-1") {
			echoed = append(echoed, u)
		}
	}
	if len(echoed) != 1 || !strings.HasPrefix(echoed[0].Text, "say second\n\n[Aide] Attached files") {
		t.Fatalf("user messages echoing the id: %v", echoed)
	}

	if _, err := app.SendMessage(ctx, &localv1.SendMessageRequest{SessionId: s.SessionId, Text: "ask Pick|Which one?|red,blue"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, 30*time.Second, "the question", func() bool {
		res, err := app.ListSessions(ctx, &localv1.ListSessionsRequest{})
		if err != nil {
			return false
		}
		for _, x := range res.Sessions {
			if x.SessionId == s.SessionId {
				return x.GetProgress().GetPendingQuestion() != nil
			}
		}
		return false
	})
	if _, err := app.SendMessage(ctx, &localv1.SendMessageRequest{SessionId: s.SessionId, Text: "blue", ClientMessageId: "cmid-e2e-2", ClientSeq: 2}); err != nil {
		t.Fatal(err)
	}
	idle(3)
	var answered bool
	for _, e := range events() {
		if r := e.GetQuestionResolved(); r != nil && slices.Equal(r.ClientMessageIds, []string{"cmid-e2e-2"}) {
			answered = true
		}
	}
	if !answered {
		t.Fatalf("no QuestionResolved names the answer's id")
	}
}
