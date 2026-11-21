package e2e

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
)

// TestSessionInsights reviews a session the way a dispatcher does once the
// work is done: an agent runs a session over the relay whose turns go
// through a rate limit the machine retries, a failure asked again and a
// question answered later, then reads `aide session insights` as JSON (the
// contract, every number right) and as Markdown, and the owner's app reads
// the same review. Another agent may not read it.
func TestSessionInsights(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	w := newOwnerMac(t, []string{"AIDE_FAKE_PROVIDERS=1"}, &localv1.GrantSpec{}, "--retry-delay", "50ms")
	const capacity = "Selected model is at capacity. Please try a different model."

	s, r := w.startSession(time.Minute, "--wait", "--prompt", "write a.txt one\nsay one")
	if r.code != 0 {
		t.Fatalf("start: %+v", r)
	}
	id := s.SessionID
	// Turn 2: the model is at capacity once; the machine retries the turn.
	if _, r := w.send(time.Minute, id, "say working\nfail-times 1 "+capacity+"\nsay two"); r.code != 0 {
		t.Fatalf("retried turn: %+v", r)
	}
	// Turn 3 fails; turn 4 asks again, and asks a question.
	if _, r := w.send(time.Minute, id, "fail boom"); r.code != 5 {
		t.Fatalf("failed turn: %+v", r)
	}
	if _, r := w.send(time.Minute, id, "ask Env|Deploy where?|staging,prod\nsay three"); r.code != 8 {
		t.Fatalf("question: %+v", r)
	}
	time.Sleep(500 * time.Millisecond)
	if r := w.cli(time.Minute, "session", "answer", id, "--choose", "prod", "--wait", "-o", "json"); r.code != 0 {
		t.Fatalf("answer: %+v", r)
	}

	r = w.cli(time.Minute, "session", "insights", id, "-o", "json")
	doc := jsonDoc(t, r)
	if r.code != 0 || doc["ok"] != true {
		t.Fatalf("insights: %+v", r)
	}
	// Every field of the contract, in order.
	var keys struct{ Insights orderedKeys }
	json.Unmarshal([]byte(r.stdout), &keys)
	want := []string{"session_id", "last_event_seq", "generated_at", "turns", "busy_ms", "question_wait_ms", "failed_turns", "failures",
		"auto_retries", "retried_turns", "stalls", "interrupts", "rollbacks", "discarded_turns", "questions", "tools", "diff_stat", "usage", "findings", "suggested_prompt", "markdown"}
	if !slices.Equal(keys.Insights, want) {
		t.Fatalf("insights keys %v, want %v", keys.Insights, want)
	}
	var got struct {
		Insights struct {
			SessionID    string `json:"session_id"`
			LastEventSeq uint64 `json:"last_event_seq"`
			GeneratedAt  string `json:"generated_at"`
			Turns        []struct {
				Turn           uint32  `json:"turn"`
				StartedAt      string  `json:"started_at"`
				EndedAt        *string `json:"ended_at"`
				DurationMs     uint64  `json:"duration_ms"`
				QuestionWaitMs uint64  `json:"question_wait_ms"`
				ToolCalls      uint32  `json:"tool_calls"`
				ToolErrors     *uint32 `json:"tool_errors"`
				AutoRetries    uint32  `json:"auto_retries"`
				Failure        string  `json:"failure"`
				DiffStat       struct {
					FilesChanged uint32 `json:"files_changed"`
				} `json:"diff_stat"`
				Usage struct {
					InputTokens  uint64 `json:"input_tokens"`
					OutputTokens uint64 `json:"output_tokens"`
				} `json:"usage"`
			} `json:"turns"`
			BusyMs         uint64 `json:"busy_ms"`
			QuestionWaitMs uint64 `json:"question_wait_ms"`
			FailedTurns    uint32 `json:"failed_turns"`
			Failures       []struct {
				Kind  string `json:"kind"`
				Count uint32 `json:"count"`
			} `json:"failures"`
			AutoRetries  uint32  `json:"auto_retries"`
			RetriedTurns uint32  `json:"retried_turns"`
			Stalls       *uint32 `json:"stalls"`
			Interrupts   *uint32 `json:"interrupts"`
			Questions    []struct {
				QuestionID string   `json:"question_id"`
				Turn       uint32   `json:"turn"`
				AskedAt    string   `json:"asked_at"`
				Resolution string   `json:"resolution"`
				WaitMs     uint64   `json:"wait_ms"`
				Texts      []string `json:"texts"`
			} `json:"questions"`
			Tools []struct {
				Kind   string `json:"kind"`
				Calls  uint32 `json:"calls"`
				Errors uint32 `json:"errors"`
			} `json:"tools"`
			DiffStat struct {
				FilesChanged uint32 `json:"files_changed"`
			} `json:"diff_stat"`
			Usage struct {
				InputTokens  uint64 `json:"input_tokens"`
				OutputTokens uint64 `json:"output_tokens"`
			} `json:"usage"`
			Findings []struct {
				Code string `json:"code"`
				Text string `json:"text"`
			} `json:"findings"`
			SuggestedPrompt string `json:"suggested_prompt"`
			Markdown        string `json:"markdown"`
		} `json:"insights"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &got); err != nil {
		t.Fatal(err)
	}
	g := got.Insights
	if g.SessionID != id || g.LastEventSeq == 0 || g.GeneratedAt == "" || len(g.Turns) != 4 || g.FailedTurns != 1 ||
		g.AutoRetries != 1 || g.RetriedTurns != 1 || g.Stalls == nil || *g.Stalls != 0 || g.Interrupts == nil || *g.Interrupts != 0 {
		t.Fatalf("totals: %s", r.stdout)
	}
	t1, t2, t3, t4 := g.Turns[0], g.Turns[1], g.Turns[2], g.Turns[3]
	if t1.Turn != 1 || t1.ToolCalls != 1 || t1.ToolErrors == nil || t1.DiffStat.FilesChanged != 1 || t1.Usage.InputTokens != 10 || t1.Failure != "" || t1.EndedAt == nil ||
		t2.AutoRetries != 1 || t2.Failure != "" || t3.Failure != "unknown" || t3.Usage.InputTokens != 0 ||
		t4.QuestionWaitMs < 500 || t4.DurationMs < t4.QuestionWaitMs {
		t.Fatalf("turns: %s", r.stdout)
	}
	if g.BusyMs != t1.DurationMs+t2.DurationMs+t3.DurationMs+t4.DurationMs || g.QuestionWaitMs != t4.QuestionWaitMs {
		t.Fatalf("time: %s", r.stdout)
	}
	failures := map[string]uint32{}
	for _, f := range g.Failures {
		failures[f.Kind] = f.Count
	}
	if len(failures) != 2 || failures["rate_limit"] != 1 || failures["unknown"] != 1 {
		t.Fatalf("failures: %v", g.Failures)
	}
	if q := g.Questions; len(q) != 1 || q[0].Turn != 4 || q[0].Resolution != "answered" || q[0].WaitMs != t4.QuestionWaitMs || q[0].QuestionID == "" ||
		q[0].AskedAt == "" || !slices.Equal(q[0].Texts, []string{"Deploy where?"}) {
		t.Fatalf("questions: %v", q)
	}
	if len(g.Tools) != 1 || g.Tools[0].Kind != "edit" || g.Tools[0].Calls != 1 || g.Tools[0].Errors != 0 {
		t.Fatalf("tools: %v", g.Tools)
	}
	if g.DiffStat.FilesChanged != 1 || g.Usage.InputTokens != 30 || g.Usage.OutputTokens != 15 {
		t.Fatalf("changes %v, usage %v", g.DiffStat, g.Usage)
	}
	var codes []string
	for _, f := range g.Findings {
		codes = append(codes, f.Code)
	}
	if !slices.Equal(codes, []string{"clarifying_questions", "follow_ups"}) {
		t.Fatalf("findings: %v", g.Findings)
	}
	if !strings.HasPrefix(g.SuggestedPrompt, "write a.txt one\nsay one\n") || !strings.Contains(g.SuggestedPrompt, "- Deploy where? → prod") ||
		!strings.Contains(g.SuggestedPrompt, "## Done when") {
		t.Fatalf("suggested prompt:\n%s", g.SuggestedPrompt)
	}

	// The text is the Markdown, to paste into a delivery record.
	r = w.cli(time.Minute, "session", "insights", id)
	if r.code != 0 || r.stdout != g.Markdown || !strings.HasPrefix(r.stdout, "## Session insights: ") ||
		!strings.Contains(r.stdout, "| Turns | 4 (1 failed) |") || !strings.Contains(r.stdout, "| Retries | 1 by the machine, 1 turn(s) asked again |") ||
		!strings.Contains(r.stdout, "| Failures | rate_limit ×1, unknown ×1 |") || !strings.Contains(r.stdout, "### Suggested prompt") {
		t.Fatalf("text: %+v", r)
	}

	// The owner's app reads the same review, kept by the machine.
	app, err := w.app.client.GetSessionInsights(t.Context(), &sessionv1.GetSessionInsightsRequest{SessionId: id})
	if err != nil || app.Markdown != g.Markdown || app.LastEventSeq != g.LastEventSeq {
		t.Fatalf("the app's review: %v %v", app, err)
	}

	// Another agent, which may not read the session, can't read its review.
	other := w.addAgent("other", nil)
	r = run(t, other, time.Minute, filepath.Join(w.bin, "aide"), "session", "insights", id, "-o", "json")
	if d := jsonDoc(t, r); r.code != 2 || d["ok"] != false || d["error"].(map[string]any)["kind"] != "not_found" || d["insights"] != nil {
		t.Fatalf("another agent: %+v", r)
	}
}

// TestSessionInsightsRollback reviews a session that was rolled back: the
// review counts the rollback, marks the turns it discarded, and says so in
// its findings, the Markdown and the suggested prompt.
func TestSessionInsightsRollback(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	w := newOwnerMac(t, []string{"AIDE_FAKE_PROVIDERS=1"}, &localv1.GrantSpec{})
	s, r := w.startSession(time.Minute, "--wait", "--prompt", "write a.txt one\nsay one")
	if r.code != 0 {
		t.Fatalf("start: %+v", r)
	}
	id := s.SessionID
	for _, m := range []string{"write b.txt two\nsay two", "write c.txt three\nsay three"} {
		if _, r := w.send(time.Minute, id, m); r.code != 0 {
			t.Fatalf("send: %+v", r)
		}
	}
	if r := w.cli(time.Minute, "session", "rollback", id, "--turn", "1", "--yes", "-o", "json"); r.code != 0 {
		t.Fatalf("rollback: %+v", r)
	}
	if _, r := w.send(time.Minute, id, "write d.txt four\nsay four"); r.code != 0 {
		t.Fatalf("send after the rollback: %+v", r)
	}

	r = w.cli(time.Minute, "session", "insights", id, "-o", "json")
	var got struct {
		Insights struct {
			Turns []struct {
				Turn       uint32 `json:"turn"`
				RolledBack bool   `json:"rolled_back"`
			} `json:"turns"`
			Rollbacks      uint32 `json:"rollbacks"`
			DiscardedTurns uint32 `json:"discarded_turns"`
			Findings       []struct {
				Code string `json:"code"`
			} `json:"findings"`
			SuggestedPrompt string `json:"suggested_prompt"`
			Markdown        string `json:"markdown"`
		} `json:"insights"`
	}
	if jsonDoc(t, r); r.code != 0 || json.Unmarshal([]byte(r.stdout), &got) != nil {
		t.Fatalf("insights: %+v", r)
	}
	g := got.Insights
	type turn struct {
		n      uint32
		rolled bool
	}
	var turns []turn
	for _, tu := range g.Turns {
		turns = append(turns, turn{tu.Turn, tu.RolledBack})
	}
	if !slices.Equal(turns, []turn{{1, false}, {2, true}, {3, true}, {2, false}}) || g.Rollbacks != 1 || g.DiscardedTurns != 2 {
		t.Fatalf("turns %v, rollbacks %d, discarded %d\n%s", turns, g.Rollbacks, g.DiscardedTurns, r.stdout)
	}
	if !slices.ContainsFunc(g.Findings, func(f struct {
		Code string `json:"code"`
	}) bool {
		return f.Code == "rolled_back"
	}) || !strings.Contains(g.SuggestedPrompt, "not to take again") || !strings.Contains(g.Markdown, "| Rollbacks | 1 (2 turn(s) discarded) |") {
		t.Fatalf("findings %v\n%s", g.Findings, g.Markdown)
	}
}

// orderedKeys decodes a JSON object's keys, in order.
type orderedKeys []string

func (k *orderedKeys) UnmarshalJSON(b []byte) error {
	dec := json.NewDecoder(strings.NewReader(string(b)))
	if _, err := dec.Token(); err != nil {
		return err
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		*k = append(*k, tok.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return err
		}
	}
	return nil
}
