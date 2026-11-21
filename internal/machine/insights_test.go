package machine

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
)

// The review of a session's events, to the millisecond: turns, failures and
// retries, the question's wait, tool calls, changes and tokens, what it
// finds and the prompt it suggests.
func TestReviewSession(t *testing.T) {
	t0 := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	var evs []*sessionv1.SessionEvent
	add := func(ms int, e *sessionv1.SessionEvent) {
		e.Seq, e.Ts = uint64(len(evs)+1), timestamppb.New(t0.Add(time.Duration(ms)*time.Millisecond))
		evs = append(evs, e)
	}
	started := func(turn uint32) *sessionv1.SessionEvent {
		return &sessionv1.SessionEvent{Kind: &sessionv1.SessionEvent_TurnStarted{TurnStarted: &sessionv1.TurnStarted{Turn: turn}}}
	}
	user := func(text string) *sessionv1.SessionEvent {
		return &sessionv1.SessionEvent{Kind: &sessionv1.SessionEvent_UserMessage{UserMessage: &sessionv1.UserMessage{Text: text}}}
	}
	status := func(st sessionv1.SessionStatus) *sessionv1.SessionEvent {
		return &sessionv1.SessionEvent{Kind: &sessionv1.SessionEvent_Status{Status: &sessionv1.StatusChanged{Status: st}}}
	}
	call := func(id, kind, summary string) *sessionv1.SessionEvent {
		return &sessionv1.SessionEvent{Kind: &sessionv1.SessionEvent_ToolCall{ToolCall: &sessionv1.ToolCall{CallId: id, Kind: kind, Summary: summary}}}
	}
	result := func(id string, isErr bool) *sessionv1.SessionEvent {
		return &sessionv1.SessionEvent{Kind: &sessionv1.SessionEvent_ToolResult{ToolResult: &sessionv1.ToolResult{CallId: id, IsError: isErr}}}
	}
	done := func(turn uint32, kind sessionv1.FailureKind, in, out uint64, files uint32) *sessionv1.SessionEvent {
		tc := &sessionv1.TurnCompleted{Turn: turn, Usage: &sessionv1.Usage{InputTokens: in, OutputTokens: out}, DiffStat: &sessionv1.DiffStat{FilesChanged: files, Insertions: 3 * files}}
		if kind != 0 {
			tc.IsError, tc.Failure = true, &sessionv1.Failure{Kind: kind}
		}
		return &sessionv1.SessionEvent{Kind: &sessionv1.SessionEvent_TurnCompleted{TurnCompleted: tc}}
	}
	const (
		rateLimit = sessionv1.FailureKind_FAILURE_KIND_RATE_LIMIT
		stalled   = sessionv1.FailureKind_FAILURE_KIND_STALLED
	)
	// Turn 1: two edits and a failed shell; the machine retries once after
	// a rate limit; 2.5 s.
	add(0, started(1))
	add(0, user("fix the login page"))
	add(100, call("a", "edit", "login.go"))
	add(200, result("a", false))
	add(300, call("b", "shell", "go test"))
	add(400, result("b", true))
	add(450, &sessionv1.SessionEvent{Kind: &sessionv1.SessionEvent_Error{Error: &sessionv1.ErrorEvent{Message: "429 (retrying)", Failure: &sessionv1.Failure{Kind: rateLimit}}}})
	add(500, status(sessionv1.SessionStatus_SESSION_STATUS_QUEUED))
	add(600, status(sessionv1.SessionStatus_SESSION_STATUS_RUNNING))
	add(700, call("c", "edit", "login_test.go"))
	add(2500, done(1, 0, 100, 10, 2))
	// Turn 2: asks a question answered after 4 s, then stalls; 6 s.
	add(3000, started(2))
	add(3000, user("also keep the old URL working"))
	add(3500, &sessionv1.SessionEvent{Kind: &sessionv1.SessionEvent_QuestionAsked{QuestionAsked: &sessionv1.QuestionAsked{Question: &sessionv1.Question{
		QuestionId: "q1", Turn: 2, AskedAt: timestamppb.New(t0.Add(3500 * time.Millisecond)), Items: []*sessionv1.QuestionItem{{Id: "i", Text: "Redirect or alias?"}}}}}})
	add(7500, &sessionv1.SessionEvent{Kind: &sessionv1.SessionEvent_QuestionResolved{QuestionResolved: &sessionv1.QuestionResolved{
		QuestionId: "q1", Resolution: sessionv1.QuestionResolution_QUESTION_RESOLUTION_ANSWERED, Answers: []*sessionv1.Answer{{ItemId: "i", Selected: []string{"Redirect"}}}}}})
	add(9000, done(2, stalled, 50, 5, 0))
	// The agent can't start again: a failure with no turn.
	add(9500, &sessionv1.SessionEvent{Kind: &sessionv1.SessionEvent_Error{Error: &sessionv1.ErrorEvent{Message: "429", Failure: &sessionv1.Failure{Kind: rateLimit}}}})
	// Turn 3 asks again and fails on the rate limit; turn 4 runs on.
	add(10000, started(3))
	add(10000, user("try again"))
	add(11000, done(3, rateLimit, 1, 1, 0))
	add(12000, started(4))
	add(12000, user("try again"))
	add(12300, call("d", "read", "a.go"))

	in := reviewSession("s_1", "Login", &sessionv1.DiffStat{FilesChanged: 2, Insertions: 6, Deletions: 1}, evs, t0.Add(time.Hour))
	if in.LastEventSeq != uint64(len(evs)) || len(in.Turns) != 4 || in.FailedTurns != 2 || in.AutoRetries != 1 || in.RetriedTurns != 2 ||
		in.Stalls != 1 || in.Interrupts != 0 || in.BusyMs != 2500+6000+1000+300 || in.QuestionWaitMs != 4000 {
		t.Fatalf("totals: %v", in)
	}
	t1, t2, t4 := in.Turns[0], in.Turns[1], in.Turns[3]
	if t1.DurationMs != 2500 || t1.ToolCalls != 3 || t1.ToolErrors != 1 || t1.AutoRetries != 1 || t1.Failure != 0 || t1.DiffStat.FilesChanged != 2 || t1.Usage.InputTokens != 100 ||
		t2.DurationMs != 6000 || t2.QuestionWaitMs != 4000 || t2.Failure != stalled ||
		t4.EndedAt != nil || t4.DurationMs != 300 || t4.ToolCalls != 1 {
		t.Fatalf("turns: %v", in.Turns)
	}
	wantFailures := []*sessionv1.FailureCount{{Kind: rateLimit, Count: 3}, {Kind: stalled, Count: 1}}
	if len(in.Failures) != 2 || !proto.Equal(in.Failures[0], wantFailures[0]) || !proto.Equal(in.Failures[1], wantFailures[1]) {
		t.Fatalf("failures: %v", in.Failures)
	}
	q := in.Questions
	if len(q) != 1 || q[0].WaitMs != 4000 || q[0].Turn != 2 || q[0].Resolution != sessionv1.QuestionResolution_QUESTION_RESOLUTION_ANSWERED || q[0].Texts[0] != "Redirect or alias?" {
		t.Fatalf("questions: %v", q)
	}
	if len(in.Tools) != 3 || in.Tools[0].Kind != "edit" || in.Tools[0].Calls != 2 || in.Tools[1].Kind != "read" || in.Tools[2].Kind != "shell" || in.Tools[2].Errors != 1 {
		t.Fatalf("tools: %v", in.Tools)
	}
	if in.Usage.InputTokens != 151 || in.Usage.OutputTokens != 16 || in.DiffStat.FilesChanged != 2 || in.DiffStat.Deletions != 1 {
		t.Fatalf("usage %v, changes %v", in.Usage, in.DiffStat)
	}
	var codes []string
	for _, f := range in.Findings {
		codes = append(codes, f.Code)
	}
	if strings.Join(codes, ",") != "clarifying_questions,follow_ups,repeated_failures,stalled" {
		t.Fatalf("findings: %v", in.Findings)
	}
	p := in.SuggestedPrompt
	for _, want := range []string{"fix the login page\n", "- also keep the old URL working", "- Redirect or alias? → Redirect", "## Done when", "small steps", "same failure comes back"} {
		if !strings.Contains(p, want) {
			t.Fatalf("suggested prompt lacks %q:\n%s", want, p)
		}
	}
	if strings.Count(p, "try again") != 1 {
		t.Fatalf("a message said twice is said once:\n%s", p)
	}
	for _, want := range []string{"## Session insights: Login (s_1)", "| Turns | 4 (2 failed) |", "| Retries | 1 by the machine, 2 turn(s) asked again |",
		"| Stalls / interrupts | 1 / 0 |", "| Questions | 1, waited 4.0s |", "| Tool calls | edit 2, read 1, shell 1 (1 failed) |", "| Changes | 2 file(s), +6 −1 |",
		"| 2 | 2026-11-20 09:00:03Z | 6.0s | 4.0s | 0 | 0 | stalled |", "| 4 | ", "running", "Redirect or alias? (answered after 4.0s)", "```text\nfix the login page"} {
		if !strings.Contains(in.Markdown, want) {
			t.Fatalf("markdown lacks %q:\n%s", want, in.Markdown)
		}
	}
}

// The machine reviews a real session: the review is kept until a new event
// comes, made when the session is archived, read only by who may read the
// session, and goes with it.
func TestSessionInsights(t *testing.T) {
	h := newHarness(t, retryLimits(20*time.Millisecond))
	ctx := context.Background()
	id := h.start("write a.txt one\nsay one")
	h.wait(id, idle)
	h.send(id, "say working\nfail-times 1 "+capacity+"\nsay two")
	h.wait(id, idle)
	h.send(id, "fail boom")
	h.wait(id, idle)
	h.send(id, "ask Env|Deploy where?|staging,prod\nsay three")
	h.wait(id, waiting)
	time.Sleep(50 * time.Millisecond)
	if _, err := h.s.AnswerQuestion(ctx, h.p, &sessionv1.AnswerQuestionRequest{SessionId: id, Answers: []*sessionv1.Answer{{Selected: []string{"prod"}}}}); err != nil {
		t.Fatal(err)
	}
	h.wait(id, idle)

	in, err := h.s.Insights(ctx, h.p, &sessionv1.GetSessionInsightsRequest{SessionId: id})
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Turns) != 4 || in.FailedTurns != 1 || in.AutoRetries != 1 || in.RetriedTurns != 1 || in.Turns[1].AutoRetries != 1 ||
		in.Turns[2].Failure != sessionv1.FailureKind_FAILURE_KIND_UNKNOWN || len(in.Questions) != 1 || in.Questions[0].WaitMs < 50 ||
		in.Turns[3].QuestionWaitMs != in.Questions[0].WaitMs || in.DiffStat.FilesChanged != 1 || in.Usage.InputTokens != 30 || len(in.Failures) != 2 ||
		!strings.Contains(in.SuggestedPrompt, "Deploy where? → prod") {
		t.Fatalf("insights:\n%v\n%s", in, h.dump(id))
	}
	// Kept: asked again, the same review.
	again, err := h.s.Insights(ctx, h.p, &sessionv1.GetSessionInsightsRequest{SessionId: id})
	if err != nil || !again.GeneratedAt.AsTime().Equal(in.GeneratedAt.AsTime()) {
		t.Fatalf("kept review: %v %v", again, err)
	}
	// Another client that may not read the session can't read its review.
	other := &Principal{Kind: PrincipalClient, ClientID: "c_other", Grant: &identityv1.Grant{GrantId: "g_other", MachineIds: []string{"*"}, ProjectIds: []string{"*"}, Agents: []string{"*"}}}
	if _, err := h.s.Insights(ctx, other, &sessionv1.GetSessionInsightsRequest{SessionId: id}); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("other client: %v", err)
	}
	// Archiving makes a new one (events came) and keeps it.
	if _, err := h.s.Archive(ctx, h.p, id); err != nil {
		t.Fatal(err)
	}
	kept := &sessionv1.SessionInsights{}
	deadline := time.Now().Add(10 * time.Second)
	for {
		last, _ := h.n.st.MaxSeq(id)
		if ok, _ := h.n.st.GetProto(insightsKey(id), kept); ok && kept.LastEventSeq == last {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no review kept at archive: %v", kept)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got, err := h.s.Insights(ctx, h.p, &sessionv1.GetSessionInsightsRequest{SessionId: id}); err != nil || !proto.Equal(got, kept) {
		t.Fatalf("archived review: %v %v", got, err)
	}
	// Deleted with the session.
	owner := &Principal{Kind: PrincipalOwner}
	if err := h.s.Delete(ctx, owner, id); err != nil {
		t.Fatal(err)
	}
	if b, _ := h.n.st.Get(insightsKey(id)); b != nil {
		t.Fatal("review outlived its session")
	}
}

// A rollback discards the latest turns of its numbers, which come again;
// each question's wait stays with the turn it was asked in.
func TestReviewRollback(t *testing.T) {
	t0 := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	var evs []*sessionv1.SessionEvent
	add := func(ms int, e *sessionv1.SessionEvent) {
		e.Seq, e.Ts = uint64(len(evs)+1), timestamppb.New(t0.Add(time.Duration(ms)*time.Millisecond))
		evs = append(evs, e)
	}
	turn := func(ms int, n uint32, q string, waitMs int) {
		add(ms, &sessionv1.SessionEvent{Kind: &sessionv1.SessionEvent_TurnStarted{TurnStarted: &sessionv1.TurnStarted{Turn: n}}})
		if q != "" {
			add(ms+100, &sessionv1.SessionEvent{Kind: &sessionv1.SessionEvent_QuestionAsked{QuestionAsked: &sessionv1.QuestionAsked{Question: &sessionv1.Question{
				QuestionId: q, Turn: n, AskedAt: timestamppb.New(t0.Add(time.Duration(ms+100) * time.Millisecond)), Items: []*sessionv1.QuestionItem{{Id: "i", Text: q}}}}}})
			add(ms+100+waitMs, &sessionv1.SessionEvent{Kind: &sessionv1.SessionEvent_QuestionResolved{QuestionResolved: &sessionv1.QuestionResolved{
				QuestionId: q, Resolution: sessionv1.QuestionResolution_QUESTION_RESOLUTION_ANSWERED}}})
		}
		add(ms+500+waitMs, &sessionv1.SessionEvent{Kind: &sessionv1.SessionEvent_TurnCompleted{TurnCompleted: &sessionv1.TurnCompleted{Turn: n}}})
	}
	turn(0, 1, "", 0)
	turn(1000, 2, "q1", 1000)
	turn(3000, 3, "", 0)
	add(4000, &sessionv1.SessionEvent{Kind: &sessionv1.SessionEvent_RolledBack{RolledBack: &sessionv1.RolledBack{Turn: 1, LastTurn: 3}}})
	turn(5000, 2, "q2", 2000)

	in := reviewSession("s_1", "", nil, evs, t0.Add(time.Hour))
	if len(in.Turns) != 4 || in.Rollbacks != 1 || in.DiscardedTurns != 2 || in.QuestionWaitMs != 3000 {
		t.Fatalf("totals: %v", in)
	}
	rolled := []bool{false, true, true, false}
	for i, tu := range in.Turns {
		if tu.RolledBack != rolled[i] {
			t.Fatalf("turn %d (%d) rolled back %v, want %v", i, tu.Turn, tu.RolledBack, rolled[i])
		}
	}
	if in.Turns[1].QuestionWaitMs != 1000 || in.Turns[3].QuestionWaitMs != 2000 {
		t.Fatalf("question waits by turn: %v", in.Turns)
	}
	if !slices.ContainsFunc(in.Findings, func(f *sessionv1.InsightFinding) bool { return f.Code == "rolled_back" }) ||
		!strings.Contains(in.SuggestedPrompt, "rolled back, not to take again") {
		t.Fatalf("findings %v, prompt:\n%s", in.Findings, in.SuggestedPrompt)
	}
	for _, want := range []string{"| Rollbacks | 1 (2 turn(s) discarded) |", "| ok, rolled back |"} {
		if !strings.Contains(in.Markdown, want) {
			t.Fatalf("markdown lacks %q:\n%s", want, in.Markdown)
		}
	}
}
