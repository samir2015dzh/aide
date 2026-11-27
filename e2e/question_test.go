package e2e

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/provider"
)

// TestOptionalQuestionItem: an agent's form question with a field it does
// not require (ACP elicitation) can be answered without that field, from
// the owner's app as from the desktop app's question panel.
func TestOptionalQuestionItem(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	fake := filepath.Join(binaries(t), "fakeacp")
	w := newOwnerMac(t, []string{"AIDE_GEMINI_BIN=" + fake, "FAKE_ACP_STATE=" + filepath.Join(t.TempDir(), "fakeacp")},
		&localv1.GrantSpec{Agents: []string{"gemini"}})
	s, r := w.startSession(2*time.Minute, "--agent", "gemini", "--wait", "--prompt", "ask 选哪个颜色|红,蓝")
	if s.SessionID == "" {
		t.Fatalf("start: %+v", r)
	}
	ctx := context.Background()
	var q *sessionv1.Question
	eventually(t, time.Minute, "the question", func() bool {
		res, err := w.app.client.ListSessions(ctx, &localv1.ListSessionsRequest{})
		if err != nil {
			return false
		}
		for _, x := range res.Sessions {
			if x.SessionId == s.SessionID && x.Progress.GetPendingQuestion() != nil {
				q = x.Progress.PendingQuestion
			}
		}
		return q != nil
	})
	if len(q.Items) != 2 || q.Items[0].Optional || !q.Items[1].Optional {
		t.Fatalf("items: %+v", q.Items)
	}
	if _, err := w.app.client.AnswerQuestion(ctx, &localv1.AnswerQuestionRequest{SessionId: s.SessionID, QuestionId: q.QuestionId,
		Answers: []*sessionv1.Answer{{ItemId: q.Items[0].Id, Selected: []string{"红"}}}}); err != nil {
		t.Fatalf("answer without the optional note: %v", err)
	}
	eventually(t, time.Minute, "the turn finishes with the answer", func() bool {
		g := w.get(s.SessionID)
		return g.Status == "idle" && strings.Contains(g.Result, "answer: v-红")
	})
}

// TestMultiSelectAnswer (工单 #179): a multiple-choice question with typed
// text besides is answered the same whichever way the owner answers it —
// from the app (the desktop's and the phone's question panels send what was
// chosen and typed), as a reply typed in the conversation, or with the CLI's
// --item — and the agent is told exactly what was chosen, in order, and
// what was typed: with labels that begin one another, a label that holds
// ", ", and choices and typed text together.
func TestMultiSelectAnswer(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	w := newOwnerMac(t, []string{"AIDE_FAKE_PROVIDERS=1"}, &localv1.GrantSpec{})
	ctx := context.Background()
	pending := func(id string) *sessionv1.Question {
		t.Helper()
		var q *sessionv1.Question
		eventually(t, time.Minute, "the question", func() bool {
			res, err := w.app.client.ListSessions(ctx, &localv1.ListSessionsRequest{})
			if err != nil {
				return false
			}
			for _, x := range res.Sessions {
				if x.SessionId == id && x.Progress.GetPendingQuestion() != nil {
					q = x.Progress.PendingQuestion
				}
			}
			return q != nil
		})
		return q
	}
	for _, c := range []struct {
		name, labels string
		selected     []string
		text         string
	}{
		{"labels begin one another", "A;A, B;A, B, C;B", []string{"A, B", "B"}, ""},
		{"a label holds a comma", "Delete both;Delete both, fix code;Email the user", []string{"Delete both, fix code", "Email the user"}, ""},
		{"chosen and typed", "Delete both;Delete both, fix code;Email the user", []string{"Email the user", "Delete both"}, "also check the logs, then report"},
		{"typed only", "Delete both;Email the user", nil, "only this"},
	} {
		want, _ := json.Marshal(map[string]any{"selected": c.selected, "text": c.text})
		value := (&provider.Answer{Selected: c.selected, Text: c.text}).Value()
		for _, way := range []string{"app", "reply", "cli"} {
			t.Run(c.name+"/"+way, func(t *testing.T) {
				s, r := w.startSession(time.Minute, "--wait", "--prompt", "ask-multi Tidy|What to do|"+c.labels)
				if s.SessionID == "" {
					t.Fatalf("start: %+v", r)
				}
				q := pending(s.SessionID)
				if len(q.Items) != 1 || !q.Items[0].MultiSelect {
					t.Fatalf("question: %+v", q)
				}
				switch way {
				case "app":
					if _, err := w.app.client.AnswerQuestion(ctx, &localv1.AnswerQuestionRequest{SessionId: s.SessionID, QuestionId: q.QuestionId,
						Answers: []*sessionv1.Answer{{ItemId: q.Items[0].Id, Selected: c.selected, Text: c.text}}}); err != nil {
						t.Fatal(err)
					}
				case "reply":
					if _, r := w.send(time.Minute, s.SessionID, value); r.code != 0 {
						t.Fatalf("reply: %+v", r)
					}
				case "cli":
					if r := w.cli(time.Minute, "session", "answer", s.SessionID, "--item", "Tidy="+value, "-o", "json"); r.code != 0 {
						t.Fatalf("answer: %+v", r)
					}
				}
				eventually(t, time.Minute, "the agent told", func() bool {
					g := w.get(s.SessionID)
					return g.Status == "idle" && strings.Contains(g.Result, "picked: ")
				})
				if got := w.get(s.SessionID).Result; got != "picked: "+string(want) {
					t.Fatalf("the agent was told %s, want %s", got, want)
				}
			})
		}
	}
}
