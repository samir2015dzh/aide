package machine

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
)

// Session insights (docs/session-insights.md): a review of a session worked
// out from its stored events by rules, never by a model, made when the
// session is archived or when someone asks, and kept in kv until a newer
// event comes.

func insightsKey(id string) string { return "insights/" + id }

// Insights is the session's review: the one kept, while no event came
// since, or a new one, then kept.
func (s *Sessions) Insights(ctx context.Context, p *Principal, req *sessionv1.GetSessionInsightsRequest) (*sessionv1.SessionInsights, error) {
	_, r, err := s.find(p, req.SessionId, false)
	if err != nil {
		return nil, err
	}
	return s.insights(r)
}

func (s *Sessions) insights(r *sessionRow) (*sessionv1.SessionInsights, error) {
	// Events are stored in batches: wait for those already emitted.
	if err := s.w.sync(writeOp{}); err != nil {
		return nil, err
	}
	last, err := s.st.MaxSeq(r.ID)
	if err != nil {
		return nil, err
	}
	kept := &sessionv1.SessionInsights{}
	if ok, err := s.st.GetProto(insightsKey(r.ID), kept); err == nil && ok && kept.LastEventSeq == last {
		return kept, nil
	}
	var evs []*sessionv1.SessionEvent
	for after := uint64(0); ; {
		page, err := s.st.Events(r.ID, after, 1000)
		if err != nil {
			return nil, err
		}
		evs = append(evs, page...)
		if len(page) < 1000 {
			break
		}
		after = page[len(page)-1].Seq
	}
	in := reviewSession(r.ID, r.Title, r.Progress.GetDiffStat(), evs, time.Now())
	if still, _ := s.st.Session(r.ID); still == nil { // deleted meanwhile: nothing to keep it for
		return in, nil
	}
	if err := s.st.SetProto(insightsKey(r.ID), in); err != nil {
		s.log.Warn("insights not kept", "session", r.ID, "err", err)
	}
	return in, nil
}

// reviewSession reviews a session from its events, oldest first.
func reviewSession(id, title string, diff *sessionv1.DiffStat, evs []*sessionv1.SessionEvent, now time.Time) *sessionv1.SessionInsights {
	in := &sessionv1.SessionInsights{SessionId: id, GeneratedAt: timestamppb.New(now), Usage: &sessionv1.Usage{},
		DiffStat: &sessionv1.DiffStat{FilesChanged: diff.GetFilesChanged(), Insertions: diff.GetInsertions(), Deletions: diff.GetDeletions()}}
	var (
		cur       *sessionv1.TurnInsight // the turn in progress
		lastAt    time.Time
		prevFail  bool
		lastErr   sessionv1.FailureKind // the turn's last error
		failures  = map[sessionv1.FailureKind]uint32{}
		tools     = map[string]*sessionv1.ToolCount{}
		callKind  = map[string]string{}
		curTurn   = map[string]*sessionv1.TurnInsight{} // call id → its turn
		questions = map[string]*sessionv1.QuestionInsight{}
		askedIn   = map[string]*sessionv1.TurnInsight{} // question id → its turn
		prompts   []string                              // what the dispatcher wrote, in order
		answers   []string                              // "question → answer"
		reads     int
		edits     int
		edited    []string
	)
	for _, e := range evs {
		at := e.Ts.AsTime()
		lastAt = at
		in.LastEventSeq = e.Seq
		switch k := e.Kind.(type) {
		case *sessionv1.SessionEvent_TurnStarted:
			cur = &sessionv1.TurnInsight{Turn: k.TurnStarted.Turn, StartedAt: e.Ts, Usage: &sessionv1.Usage{}, DiffStat: &sessionv1.DiffStat{}}
			lastErr = sessionv1.FailureKind_FAILURE_KIND_UNSPECIFIED
			in.Turns = append(in.Turns, cur)
			if prevFail {
				in.RetriedTurns++
			}
		case *sessionv1.SessionEvent_UserMessage:
			if t := strings.TrimSpace(k.UserMessage.Text); t != "" {
				prompts = append(prompts, t)
			}
		case *sessionv1.SessionEvent_Status:
			// The machine starting the turn again by itself (retryLater,
			// restartFresh): the turn is queued while it is in progress.
			if cur != nil && k.Status.Status == sessionv1.SessionStatus_SESSION_STATUS_QUEUED {
				cur.AutoRetries++
				in.AutoRetries++
				if lastErr != sessionv1.FailureKind_FAILURE_KIND_UNSPECIFIED {
					failures[lastErr]++
				}
				lastErr = sessionv1.FailureKind_FAILURE_KIND_UNSPECIFIED
			}
		case *sessionv1.SessionEvent_ToolCall:
			c := k.ToolCall
			kind := cmp.Or(c.Kind, "other")
			t := tools[kind]
			if t == nil {
				t = &sessionv1.ToolCount{Kind: kind}
				tools[kind] = t
			}
			t.Calls++
			callKind[c.CallId] = kind
			switch kind {
			case "read", "search":
				reads++
			case "edit":
				edits++
				if c.Summary != "" && !slices.Contains(edited, c.Summary) {
					edited = append(edited, c.Summary)
				}
			}
			if cur != nil {
				cur.ToolCalls++
				curTurn[c.CallId] = cur
			}
		case *sessionv1.SessionEvent_ToolResult:
			if !k.ToolResult.IsError {
				break
			}
			if t := tools[callKind[k.ToolResult.CallId]]; t != nil {
				t.Errors++
			}
			if t := curTurn[k.ToolResult.CallId]; t != nil {
				t.ToolErrors++
			}
		case *sessionv1.SessionEvent_QuestionAsked:
			q := k.QuestionAsked.Question
			qi := &sessionv1.QuestionInsight{QuestionId: q.GetQuestionId(), Turn: q.GetTurn(), AskedAt: cmp.Or(q.GetAskedAt(), e.Ts)}
			for _, it := range q.GetItems() {
				qi.Texts = append(qi.Texts, it.Text)
			}
			questions[qi.QuestionId] = qi
			askedIn[qi.QuestionId] = cur
			in.Questions = append(in.Questions, qi)
		case *sessionv1.SessionEvent_QuestionResolved:
			r := k.QuestionResolved
			qi := questions[r.QuestionId]
			if qi == nil {
				break
			}
			qi.Resolution = r.Resolution
			if w := at.Sub(qi.AskedAt.AsTime()); w > 0 {
				qi.WaitMs = uint64(w.Milliseconds())
			}
			in.QuestionWaitMs += qi.WaitMs
			// Its turn, not its number: a rollback numbers turns again.
			if t := askedIn[r.QuestionId]; t != nil {
				t.QuestionWaitMs += qi.WaitMs
			}
			for i, a := range r.Answers {
				text := strings.TrimSpace(strings.Join(append(slices.Clone(a.Selected), a.Text), " "))
				if text != "" && i < len(qi.Texts) {
					answers = append(answers, qi.Texts[i]+" → "+text)
				}
			}
		case *sessionv1.SessionEvent_RolledBack:
			// Turns turn+1 to last_turn, the latest ones of those numbers,
			// are discarded.
			rb := k.RolledBack
			in.Rollbacks++
			for i := len(in.Turns) - 1; i >= 0; i-- {
				if t := in.Turns[i]; t.Turn > rb.Turn && t.Turn <= rb.LastTurn && !t.RolledBack {
					t.RolledBack = true
					in.DiscardedTurns++
				}
			}
		case *sessionv1.SessionEvent_Error:
			// A failure with no turn in progress: the agent could not start.
			// In a turn, it counts if the machine retries the turn for it.
			switch f := k.Error.Failure; {
			case f == nil:
			case cur == nil:
				failures[f.Kind]++
				prevFail = true
			default:
				lastErr = f.Kind
			}
		case *sessionv1.SessionEvent_TurnCompleted:
			tc := k.TurnCompleted
			if cur == nil || cur.Turn != tc.Turn {
				cur = &sessionv1.TurnInsight{Turn: tc.Turn, StartedAt: e.Ts}
				in.Turns = append(in.Turns, cur)
			}
			cur.EndedAt = e.Ts
			cur.DiffStat = &sessionv1.DiffStat{FilesChanged: tc.DiffStat.GetFilesChanged(), Insertions: tc.DiffStat.GetInsertions(), Deletions: tc.DiffStat.GetDeletions()}
			cur.Usage = &sessionv1.Usage{InputTokens: tc.Usage.GetInputTokens(), OutputTokens: tc.Usage.GetOutputTokens(), CostUsd: tc.Usage.GetCostUsd(), CostEstimated: tc.Usage.GetCostEstimated()}
			in.Usage.InputTokens += cur.Usage.InputTokens
			in.Usage.OutputTokens += cur.Usage.OutputTokens
			in.Usage.CostUsd += cur.Usage.CostUsd
			if cur.Usage.CostEstimated {
				in.Usage.CostEstimated = true
			}
			prevFail = false
			if tc.IsError {
				kind := cmp.Or(tc.Failure.GetKind(), sessionv1.FailureKind_FAILURE_KIND_UNKNOWN)
				cur.Failure = kind
				failures[kind]++
				in.FailedTurns++
				// Going on after an interrupt is not asking again.
				prevFail = kind != sessionv1.FailureKind_FAILURE_KIND_INTERRUPTED
			}
			cur = nil
		}
	}
	for _, t := range in.Turns {
		end := lastAt
		if t.EndedAt != nil {
			end = t.EndedAt.AsTime()
		}
		if d := end.Sub(t.StartedAt.AsTime()); d > 0 {
			t.DurationMs = uint64(d.Milliseconds())
		}
		in.BusyMs += t.DurationMs
	}
	for k, n := range failures {
		in.Failures = append(in.Failures, &sessionv1.FailureCount{Kind: k, Count: n})
	}
	slices.SortFunc(in.Failures, func(a, b *sessionv1.FailureCount) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Kind, b.Kind))
	})
	in.Stalls = failures[sessionv1.FailureKind_FAILURE_KIND_STALLED]
	in.Interrupts = failures[sessionv1.FailureKind_FAILURE_KIND_INTERRUPTED]
	for _, t := range tools {
		in.Tools = append(in.Tools, t)
	}
	slices.SortFunc(in.Tools, func(a, b *sessionv1.ToolCount) int {
		return cmp.Or(cmp.Compare(b.Calls, a.Calls), strings.Compare(a.Kind, b.Kind))
	})
	in.Findings = findings(in, len(prompts), reads, edits)
	in.SuggestedPrompt = suggestPrompt(in, prompts, answers, edited)
	in.Markdown = insightsMarkdown(in, title)
	return in
}

// Thresholds of the findings.
const (
	manyQuestions = 1 // questions asked: the prompt left decisions open
	manyFollowUps = 2 // messages after the first: the goal came in pieces
	manyFailures  = 2 // failed turns (not interrupted)
	manyReads     = 10
)

// findings are what the review finds a better prompt would avoid.
func findings(in *sessionv1.SessionInsights, messages, reads, edits int) []*sessionv1.InsightFinding {
	var out []*sessionv1.InsightFinding
	add := func(code, format string, a ...any) {
		out = append(out, &sessionv1.InsightFinding{Code: code, Text: fmt.Sprintf(format, a...)})
	}
	if n := len(in.Questions); n >= manyQuestions {
		add("clarifying_questions", "The agent asked %d question(s) to settle what the prompt left open; the prompt below settles them up front.", n)
	}
	if n := messages - 1; n >= manyFollowUps {
		add("follow_ups", "%d message(s) after the first prompt added to the task; the goal and what counts as done belong in the first.", n)
	}
	failed := in.FailedTurns - in.Interrupts
	if failed >= manyFailures {
		var kinds []string
		for _, f := range in.Failures {
			if f.Kind != sessionv1.FailureKind_FAILURE_KIND_INTERRUPTED {
				kinds = append(kinds, fmt.Sprintf("%s ×%d", failureName(f.Kind), f.Count))
			}
		}
		add("repeated_failures", "%d turns failed (%s); fix the cause before dispatching again, or make the task smaller.", failed, strings.Join(kinds, ", "))
	}
	if in.Stalls > 0 {
		add("stalled", "The agent went quiet for too long %d time(s); ask for small steps that each report progress, and no long silent commands.", in.Stalls)
	}
	if in.Rollbacks > 0 {
		add("rolled_back", "The work was rolled back %d time(s), discarding %d turn(s); say which approach not to take, and to check in before large changes.", in.Rollbacks, in.DiscardedTurns)
	}
	if in.Interrupts > 0 {
		add("interrupted", "Someone stopped the work %d time(s); say where the work stops and what is out of scope.", in.Interrupts)
	}
	if reads >= manyReads && reads > 3*edits {
		add("missing_context", "The agent spent %d tool calls reading and searching for %d edit(s); name the files, entry points and docs to start from.", reads, edits)
	}
	return out
}

// suggestPrompt is a better prompt to dispatch such work with.
func suggestPrompt(in *sessionv1.SessionInsights, prompts, answers, edited []string) string {
	has := func(code string) bool {
		return slices.ContainsFunc(in.Findings, func(f *sessionv1.InsightFinding) bool { return f.Code == code })
	}
	var b strings.Builder
	if len(prompts) == 0 {
		b.WriteString("<the task>\n")
	} else {
		b.WriteString(prompts[0] + "\n")
	}
	section := func(title string, lines []string) {
		if len(lines) == 0 {
			return
		}
		b.WriteString("\n## " + title + "\n")
		for _, l := range lines {
			b.WriteString("- " + strings.ReplaceAll(l, "\n", "\n  ") + "\n")
		}
	}
	var later []string
	for _, p := range prompts[min(1, len(prompts)):] {
		if !slices.Contains(later, p) && p != prompts[0] {
			later = append(later, p)
		}
	}
	section("Also (said in later messages)", later)
	section("Decisions already made", answers)
	var done []string
	if has("follow_ups") || has("clarifying_questions") {
		done = append(done, "<what done looks like: the behaviour, the tests that pass>")
	}
	section("Done when", done)
	var context []string
	if has("missing_context") {
		if len(edited) > 10 {
			edited = edited[:10]
		}
		for _, e := range edited {
			context = append(context, "start from "+e)
		}
		context = append(context, "<the files, entry points and docs to read first>")
	}
	section("Context", context)
	var rules []string
	if has("stalled") {
		rules = append(rules, "work in small steps and report after each; no command that runs for minutes without output")
	}
	if has("rolled_back") {
		rules = append(rules, "<the approach that was rolled back, not to take again>; check in before large changes")
	}
	if has("interrupted") {
		rules = append(rules, "<where to stop, and what is out of scope>")
	}
	if has("repeated_failures") {
		rules = append(rules, "if the same failure comes back, stop and say why instead of trying again")
	}
	section("Constraints", rules)
	return strings.TrimRight(b.String(), "\n")
}

// insightsMarkdown is the review as Markdown for a delivery record.
func insightsMarkdown(in *sessionv1.SessionInsights, title string) string {
	var b strings.Builder
	p := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	name := in.SessionId
	if title != "" {
		name = title + " (" + in.SessionId + ")"
	}
	p("## Session insights: %s\n\n", name)
	p("| | |\n|---|---|\n")
	p("| Turns | %d (%d failed) |\n", len(in.Turns), in.FailedTurns)
	p("| Time | %s (waiting for answers %s) |\n", ms(in.BusyMs), ms(in.QuestionWaitMs))
	var fails []string
	for _, f := range in.Failures {
		fails = append(fails, fmt.Sprintf("%s ×%d", failureName(f.Kind), f.Count))
	}
	p("| Failures | %s |\n", cmp.Or(strings.Join(fails, ", "), "none"))
	p("| Retries | %d by the machine, %d turn(s) asked again |\n", in.AutoRetries, in.RetriedTurns)
	p("| Stalls / interrupts | %d / %d |\n", in.Stalls, in.Interrupts)
	p("| Rollbacks | %d (%d turn(s) discarded) |\n", in.Rollbacks, in.DiscardedTurns)
	p("| Questions | %d, waited %s |\n", len(in.Questions), ms(in.QuestionWaitMs))
	var tools []string
	for _, t := range in.Tools {
		s := fmt.Sprintf("%s %d", t.Kind, t.Calls)
		if t.Errors > 0 {
			s += fmt.Sprintf(" (%d failed)", t.Errors)
		}
		tools = append(tools, s)
	}
	p("| Tool calls | %s |\n", cmp.Or(strings.Join(tools, ", "), "none"))
	p("| Changes | %s |\n", diffText(in.DiffStat))
	p("| Tokens | %d in, %d out%s |\n", in.Usage.GetInputTokens(), in.Usage.GetOutputTokens(), cost(in.Usage))
	if len(in.Turns) > 0 {
		p("\n### Turns\n\n| Turn | Started | Took | Waited | Tools | Retries | Result | Changes | Tokens in/out |\n|---|---|---|---|---|---|---|---|---|\n")
		for _, t := range in.Turns {
			result := "ok"
			switch {
			case t.EndedAt == nil:
				result = "running"
			case t.Failure != sessionv1.FailureKind_FAILURE_KIND_UNSPECIFIED:
				result = failureName(t.Failure)
			}
			if t.RolledBack {
				result += ", rolled back"
			}
			tools := fmt.Sprint(t.ToolCalls)
			if t.ToolErrors > 0 {
				tools += fmt.Sprintf(" (%d failed)", t.ToolErrors)
			}
			p("| %d | %s | %s | %s | %s | %d | %s | %s | %d/%d |\n", t.Turn, t.StartedAt.AsTime().UTC().Format("2006-01-02 15:04:05Z"),
				ms(t.DurationMs), ms(t.QuestionWaitMs), tools, t.AutoRetries, result, diffText(t.DiffStat), t.Usage.GetInputTokens(), t.Usage.GetOutputTokens())
		}
	}
	if len(in.Questions) > 0 {
		p("\n### Questions\n\n")
		for _, q := range in.Questions {
			res := "waiting"
			if q.Resolution != sessionv1.QuestionResolution_QUESTION_RESOLUTION_UNSPECIFIED {
				res = strings.ToLower(strings.TrimPrefix(q.Resolution.String(), "QUESTION_RESOLUTION_")) + " after " + ms(q.WaitMs)
			}
			p("- Turn %d: %s (%s)\n", q.Turn, strings.Join(q.Texts, " / "), res)
		}
	}
	if len(in.Findings) > 0 {
		p("\n### Findings\n\n")
		for _, f := range in.Findings {
			p("- `%s`: %s\n", f.Code, f.Text)
		}
	}
	fence := "```"
	for strings.Contains(in.SuggestedPrompt, fence) {
		fence += "`"
	}
	p("\n### Suggested prompt\n\n%stext\n%s\n%s\n", fence, in.SuggestedPrompt, fence)
	return b.String()
}

// ms is a duration in milliseconds as people read it: 850ms, 12.3s, 4m05s.
func ms(n uint64) string {
	d := time.Duration(n) * time.Millisecond
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", n)
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

func diffText(d *sessionv1.DiffStat) string {
	if d.GetFilesChanged() == 0 {
		return "none"
	}
	return fmt.Sprintf("%d file(s), +%d −%d", d.GetFilesChanged(), d.GetInsertions(), d.GetDeletions())
}

func cost(u *sessionv1.Usage) string {
	if u.GetCostUsd() == 0 {
		return ""
	}
	if u.GetCostEstimated() {
		return fmt.Sprintf(" (~$%.2f est.)", u.GetCostUsd())
	}
	return fmt.Sprintf(" ($%.2f)", u.GetCostUsd())
}

// failureName is the contract's name of a failure kind: "rate_limit".
func failureName(k sessionv1.FailureKind) string {
	return strings.ToLower(strings.TrimPrefix(k.String(), "FAILURE_KIND_"))
}
