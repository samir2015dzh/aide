package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/peer"
	"github.com/samir2015dzh/aide/internal/provider"
	"github.com/samir2015dzh/aide/internal/sessionstate"
)

func (a *app) sessionCmd() *cobra.Command {
	cmd := group(&cobra.Command{Use: "session", Short: "Start and manage coding-agent sessions"})
	cmd.AddCommand(a.startCmd(), a.getCmd(), a.waitCmd(), a.listCmd(), a.eventsCmd(), a.sendCmd(), a.answerCmd(), a.interruptCmd(), a.archiveCmd(), a.unarchiveCmd(), a.diffCmd(), a.pushCmd())
	return cmd
}

// withMachine runs fn on the machine a session belongs to, to act on it:
// not on a DSH session, which agents only read.
func (a *app) withMachine(ctx context.Context, sessionID string, fn func(c *peer.Client, n *peer.Machine, ref machineRef) error) error {
	if isNative(sessionID) {
		return readOnly(sessionID)
	}
	return a.sessionMachine(ctx, sessionID, fn)
}

// sessionMachine runs fn on the machine a session (or a DSH session)
// belongs to.
func (a *app) sessionMachine(ctx context.Context, sessionID string, fn func(c *peer.Client, n *peer.Machine, ref machineRef) error) error {
	c, machines, err := a.connect(ctx, func(seen []machineRef) []machineRef {
		m, _ := resolveMachine(seen, "", sessionID)
		return []machineRef{m}
	})
	if err != nil {
		return err
	}
	defer c.Close()
	ref, err := resolveMachine(machines, "", sessionID)
	if err != nil {
		return err
	}
	return a.onMachine(ctx, c, ref, fn)
}

// onMachine connects to the machine and runs fn there.
func (a *app) onMachine(ctx context.Context, c *peer.Client, ref machineRef, fn func(c *peer.Client, n *peer.Machine, ref machineRef) error) error {
	const hint = "try again once it is back; `aide project list` shows the machines offline now"
	var seen time.Time
	if ls := ref.Entry.GetLastSeen(); ls != nil {
		seen = ls.AsTime()
	}
	if !ref.Online {
		m := offlineMachine{Machine: ref.Name, LastSeen: tsJSON(ref.Entry.GetLastSeen())}
		return &exitError{code: exitUnavailable, kind: "unreachable", err: fmt.Errorf("machine %s is offline (%s)", ref.Name, offlineText(m)), hint: hint,
			machine: ref.Name, why: "offline", heard: seen}
	}
	n, err := c.Machine(ctx, ref.ID)
	if err != nil {
		return &exitError{code: exitUnavailable, kind: "unreachable", err: fmt.Errorf("machine %s cannot be reached: %v", ref.Name, err), hint: hint,
			machine: ref.Name, why: err.Error(), heard: seen}
	}
	if err := checkName(ref, n); err != nil {
		return err
	}
	return fn(c, n, ref)
}

// Attachment limits, as the machine enforces them.
const (
	maxFiles     = 20
	maxFileBytes = 32 << 20
)

// readFiles reads --file arguments as attachments.
func readFiles(paths []string) ([]*sessionv1.Attachment, error) {
	if len(paths) > maxFiles {
		return nil, fail(exitUsage, "%d files; at most %d per message", len(paths), maxFiles)
	}
	var out []*sessionv1.Attachment
	total := 0
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil {
			return nil, fail(exitUsage, "%v", err)
		}
		if fi.IsDir() {
			return nil, fail(exitUsage, "%s is a directory; attach files (or an archive)", p)
		}
		if total += int(fi.Size()); total > maxFileBytes {
			return nil, fail(exitUsage, "files are larger than %d MiB in total", maxFileBytes>>20)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, fail(exitUsage, "%v", err)
		}
		out = append(out, &sessionv1.Attachment{Name: filepath.Base(p), Data: b})
	}
	return out, nil
}

// readPrompt reads the message text from --<flag> or --<flag>-file; it may
// be empty only with files. Stdin is read only when asked for (-): an agent's
// stdin is often a pipe nobody writes to or closes, and reading it unasked
// would hang.
func readPrompt(flag, text, file string, files int) (string, error) {
	switch {
	case text != "":
		return text, nil
	case file == "-":
		b, err := io.ReadAll(os.Stdin)
		return string(b), err
	case file != "":
		b, err := os.ReadFile(file)
		return string(b), err
	case files > 0:
		return "", nil
	}
	return "", fail(exitUsage, "give the %s with --%s, or --%s-file (- reads stdin), or attach --file", flag, flag, flag)
}

func (a *app) startCmd() *cobra.Command {
	var project, agent, base, model, effort, mode, prompt, promptFile, title, idem string
	var repo, branch, machine string
	var questionTimeout time.Duration
	var files []string
	var wait bool
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a coding-agent session (Claude Code, Codex, Gemini, Cursor, ...) in a project, or in a GitHub repository",
		RunE: func(cmd *cobra.Command, _ []string) error {
			switch {
			case project == "" && repo == "":
				return fail(exitUsage, "give --project to work in a project, or --repo to work in a GitHub repository")
			case project != "" && repo != "":
				return fail(exitUsage, "give --project or --repo, not both")
			case repo == "" && (branch != "" || machine != ""):
				return fail(exitUsage, "--branch and --machine go with --repo (in a project, --base names the branch)")
			case repo != "" && base != "":
				return fail(exitUsage, "with --repo, name the branch with --branch")
			}
			text, err := readPrompt("prompt", prompt, promptFile, len(files))
			if err != nil {
				return err
			}
			atts, err := readFiles(files)
			if err != nil {
				return err
			}
			var qt *uint32
			if cmd.Flags().Changed("question-timeout") {
				v := uint32(questionTimeout / time.Second)
				qt = &v
			}
			if idem == "" {
				idem = newIdempotencyKey()
			}
			ctx := cmd.Context()
			if timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, timeout)
				defer cancel()
			}
			warm := warmProject(project)
			if repo != "" {
				warm = warmTargets(machine)
			}
			c, machines, err := a.connect(ctx, warm)
			if err != nil {
				return err
			}
			defer c.Close()
			var ref machineRef
			var projectID string
			if repo != "" {
				ref, err = repoMachine(machines, machine)
			} else {
				ref, projectID, err = a.resolveProject(ctx, c, machines, project)
			}
			if err != nil {
				return err
			}
			return a.onMachine(ctx, c, ref, func(c *peer.Client, n *peer.Machine, ref machineRef) error {
				// A repository's branch is its base ref (one of them at most is set).
				res, err := n.Session.StartSession(ctx, &sessionv1.StartSessionRequest{IdempotencyKey: idem, ProjectId: projectID,
					Agent: agent, Prompt: text, BaseRef: base + branch, Model: model, Effort: effort, Mode: mode, Attachments: atts, QuestionTimeoutSeconds: qt, Title: title,
					Conversation: a.as, Repo: repo})
				if connect.CodeOf(err) == connect.CodeNotFound && repo == "" {
					return &exitError{code: exitUsage, kind: "not_found", err: err, hint: "see `aide project list`"}
				}
				if err != nil {
					return err
				}
				s := res.Session
				// The id first, on stderr, whatever comes next: a caller killed
				// while it waits still knows its session.
				if s.Repo != "" {
					fmt.Fprintf(cmd.ErrOrStderr(), "session %s in %s/%s (GitHub %s, branch %s)\n", s.SessionId, ref.Name, s.ProjectId, s.Repo, s.RepoBranch)
				} else {
					fmt.Fprintf(cmd.ErrOrStderr(), "session %s in %s/%s\n", s.SessionId, ref.Name, projectID)
				}
				if !wait {
					return a.started(cmd, s, ref.Name)
				}
				return a.waitTurn(ctx, cmd, c, ref, s.SessionId, 0)
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&project, "project", "", "where to work: machine/project as aide project list shows it, or the project alone when only one machine has it")
	f.StringVar(&agent, "agent", "claude", "coding agent to run: claude, codex, or an ACP agent (gemini, cursor, copilot, ...); aide project list shows those available")
	f.StringVar(&base, "base", "", "base git ref of the session's worktree (worktree projects only; default: the project's); fetched from the project's remote when the machine lacks it")
	f.StringVar(&repo, "repo", "", "instead of --project: a GitHub repository to work in (owner/repo or its URL), which the machine clones, or fetches again, with its own git login")
	f.StringVar(&branch, "branch", "", "with --repo: the branch the session's worktree starts from (default: the repository's default branch)")
	f.StringVar(&machine, "machine", "", "with --repo: the machine to work on (name or id; default: the only one online)")
	f.StringVar(&model, "model", "", "model to run, one aide agent list shows for the agent (default: the one the owner set for the agent on that machine, else the agent's own)")
	f.StringVar(&effort, "effort", "", "reasoning effort, one aide agent list shows for the model (default: the one the owner set for the agent on that machine, else the agent's own)")
	f.StringVar(&mode, "mode", "", "session mode (an ACP agent's permission preset), one aide agent list shows for the agent (default: the one the owner set for the agent on that machine, else the agent's own)")
	f.StringVar(&prompt, "prompt", "", "prompt text")
	f.StringVar(&promptFile, "prompt-file", "", "read the prompt from a file (- for stdin)")
	f.StringVar(&title, "title", "", "a short title for session lists (default: the first line of the prompt)")
	f.StringArrayVarP(&files, "file", "f", nil, "attach a file (repeatable; images are shown to the agent, other files are saved on the machine and listed by path)")
	f.DurationVar(&questionTimeout, "question-timeout", 30*time.Minute, "how long the agent's questions wait for an answer before it goes on alone (0: nobody will answer)")
	f.StringVar(&idem, "idempotency-key", "", "retrying with the same key returns the same session")
	f.BoolVar(&wait, "wait", false, waitHelp)
	f.DurationVar(&timeout, "timeout", 0, timeoutHelp)
	return cmd
}

// repoMachine is the machine a session in a GitHub repository runs on: the
// one named, else the only one online.
func repoMachine(machines []machineRef, flag string) (machineRef, error) {
	if flag != "" {
		return resolveMachine(machines, flag, "")
	}
	var online []machineRef
	var names []string
	for _, m := range machines {
		if m.Online {
			online = append(online, m)
		}
		names = append(names, m.Name)
	}
	switch {
	case len(online) == 1:
		return online[0], nil
	case len(machines) == 1:
		return machines[0], nil
	case len(machines) == 0:
		return machineRef{}, &exitError{code: exitUsage, kind: "not_found", err: fmt.Errorf("no machine to clone the repository on"), hint: "see `aide project list`"}
	}
	return machineRef{}, fail(exitUsage, "several machines can clone the repository: pass --machine with one of %s", strings.Join(names, ", "))
}

// waitTurn follows events after afterSeq until the session is idle or
// ended, then prints the outcome. It reconnects on transient failures.
func (a *app) waitTurn(ctx context.Context, cmd *cobra.Command, c *peer.Client, ref machineRef, id string, afterSeq uint64) error {
	w := cmd.OutOrStdout()
	last := afterSeq
	var result *sessionv1.TurnCompleted
	var final sessionv1.SessionStatus
	for attempt := 0; ; attempt++ {
		n, err := c.Machine(ctx, ref.ID)
		if err == nil {
			var stream *connect.ServerStreamForClient[sessionv1.SessionEvent]
			stream, err = n.Session.StreamEvents(ctx, &sessionv1.StreamEventsRequest{SessionId: id, AfterSeq: last, Follow: true})
			if err == nil {
				for stream.Receive() {
					e := stream.Msg()
					last = e.Seq
					if tc := e.GetTurnCompleted(); tc != nil {
						result = tc
					}
					if st := e.GetStatus(); st != nil {
						switch st.Status {
						case sessionv1.SessionStatus_SESSION_STATUS_IDLE, sessionv1.SessionStatus_SESSION_STATUS_ARCHIVED,
							sessionv1.SessionStatus_SESSION_STATUS_CRASHED, sessionv1.SessionStatus_SESSION_STATUS_WAITING_INPUT:
							final = st.Status
						}
					}
					if final != 0 {
						break
					}
				}
				err = stream.Err()
				stream.Close()
			}
		}
		if final != 0 {
			break
		}
		if ctx.Err() != nil {
			var s *sessionv1.Session
			if a.output == "json" {
				// The caller may not know the session yet (start --wait).
				fetch, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				if s, _ = a.getSession(fetch, c, ref, id); s == nil {
					// The cancelled stream can take the machine's connection
					// with it (502 through the link): read it on a fresh one.
					c.ForgetMachine(n)
					s, _ = a.getSession(fetch, c, ref, id)
				}
				cancel()
			}
			return a.outcome(w, ref.Name, s, &exitError{code: exitTimeout, kind: "wait_timeout", err: fmt.Errorf("stopped waiting for session %s; its turn goes on", id),
				hint: fmt.Sprintf("check it with `aide session get %s`", id)})
		}
		c.ForgetMachine(n)
		if attempt > 30 {
			return a.outcome(w, ref.Name, nil, &exitError{code: exitUnavailable, kind: "unreachable", err: fmt.Errorf("lost connection to %s: %v", ref.Name, err),
				hint: fmt.Sprintf("check it later with `aide session get %s`", id)})
		}
		time.Sleep(2 * time.Second)
	}
	s, err := a.getSession(ctx, c, ref, id)
	if err != nil {
		return a.outcome(w, ref.Name, nil, err)
	}
	if final == sessionv1.SessionStatus_SESSION_STATUS_WAITING_INPUT {
		return a.outcome(w, ref.Name, s, &exitError{code: exitNeedsInput, kind: "needs_input", err: fmt.Errorf("session %s is waiting for an answer", id),
			hint: fmt.Sprintf("`aide session answer %s --choose <option>` or `--text <answer>` (or reply with `aide session send`)", id)})
	}
	return a.outcome(w, ref.Name, s, turnError(s, result, final))
}

// turnError is the error a finished wait exits with, if any.
func turnError(s *sessionv1.Session, result *sessionv1.TurnCompleted, final sessionv1.SessionStatus) error {
	id := s.SessionId
	var f *sessionv1.Failure
	switch {
	case result != nil && result.IsError:
		f = result.Failure
	case final == sessionv1.SessionStatus_SESSION_STATUS_CRASHED:
		// The turn did not finish (or start): the agent crashed or could
		// not start, or aided restarted.
		f = s.GetProgress().GetLastFailure()
		if f == nil {
			f = &sessionv1.Failure{Kind: sessionv1.FailureKind_FAILURE_KIND_AGENT_CRASHED, Retryable: true,
				Message: "the turn did not finish", Hint: "send the message again to resume the session"}
		}
	case final == sessionv1.SessionStatus_SESSION_STATUS_IDLE && result == nil:
		// The turn never ran (interrupted while it waited to start).
		f = s.GetProgress().GetLastFailure()
	case final == sessionv1.SessionStatus_SESSION_STATUS_ARCHIVED:
		return &exitError{code: exitFailed, kind: "archived", err: fmt.Errorf("session %s was archived", id),
			hint: fmt.Sprintf("`aide session unarchive %s` to go on with it", id)}
	}
	if f == nil {
		return nil
	}
	code := exitFailed
	if f.GetRetryable() {
		code = exitRetryable
	}
	kind := firstNonEmpty(failureName(f.GetKind()), "unknown")
	msg := firstNonEmpty(f.GetMessage(), s.Error, resultText(result), "turn failed")
	return &exitError{code: code, kind: kind, err: fmt.Errorf("session %s: %s: %s", id, kind, msg), hint: f.GetHint(), retryable: f.GetRetryable()}
}

// outcome ends a wait with the session as session get shows it (its
// question, or the agent's full result) and, when the wait fails, the
// error: in JSON one document, in text the session on stdout and the
// error on stderr.
func (a *app) outcome(w io.Writer, machine string, s *sessionv1.Session, err error) error {
	if a.output != "json" {
		if s != nil {
			writeSession(w, s, machine, true)
		}
		return err
	}
	if s == nil {
		return a.report(w, err)
	}
	return a.report(w, err, field{"session", sessionView(s, machine)})
}

// started reports a message accepted without waiting: the session, and on
// stderr how to see what becomes of it.
func (a *app) started(cmd *cobra.Command, s *sessionv1.Session, machine string) error {
	fmt.Fprintf(cmd.ErrOrStderr(), "→ it runs on; check it with `aide session get %s` (status, then the full result once idle)\n", s.SessionId)
	return a.showSession(cmd, s, machine, false)
}

// showSession prints a session: in JSON the document, in text a summary
// (and with full, its question or result).
func (a *app) showSession(cmd *cobra.Command, s *sessionv1.Session, machine string, full bool) error {
	a.keepSeen(s.GetMachineId(), time.Now(), s)
	if a.output == "json" {
		return a.report(cmd.OutOrStdout(), nil, field{"session", sessionView(s, machine)})
	}
	writeSession(cmd.OutOrStdout(), s, machine, full)
	return nil
}

const (
	waitHelp    = "block until the turn finishes, then print the session with the agent's full result (exit code: how the turn went); without it the command returns at once and session get shows progress and, once idle, the result"
	timeoutHelp = "with --wait: stop waiting after this long (exit code 6; the turn goes on, see session get). Pass one below the time limit of the tool running this command"
)

func resultText(r *sessionv1.TurnCompleted) string {
	if r == nil {
		return ""
	}
	return r.Result
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func (a *app) getSession(ctx context.Context, c *peer.Client, ref machineRef, id string) (*sessionv1.Session, error) {
	n, err := c.Machine(ctx, ref.ID)
	if err != nil {
		return nil, err
	}
	return n.Session.GetSession(ctx, &sessionv1.GetSessionRequest{SessionId: id})
}

// failureName is the CLI spelling of a failure kind ("rate_limit"). A kind
// newer than this build (from a newer machine) is "unknown"; the failure's
// message, hint and retryable still say what to do.
func failureName(k sessionv1.FailureKind) string {
	if k == sessionv1.FailureKind_FAILURE_KIND_UNSPECIFIED {
		return ""
	}
	if _, ok := sessionv1.FailureKind_name[int32(k)]; !ok {
		return "unknown"
	}
	return strings.ToLower(strings.TrimPrefix(k.String(), "FAILURE_KIND_"))
}

// statusName is the CLI spelling of a status; one newer than this build is
// "unknown".
func statusName(s sessionv1.SessionStatus) string {
	if _, ok := sessionv1.SessionStatus_name[int32(s)]; !ok {
		return "unknown"
	}
	return strings.ToLower(strings.TrimPrefix(s.String(), "SESSION_STATUS_"))
}

// plural is "1 file", "2 files".
func plural(n int, one string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %ss", n, one)
}

// writeSession prints a session for reading: a summary line, what is
// going on now, its last failure; with full, its question or, once the
// turn is over, the agent's result.
func writeSession(w io.Writer, s *sessionv1.Session, machine string, full bool) {
	v := sessionView(s, machine)
	fmt.Fprintf(w, "session %s · %s · %s · %s · %s\n", v.SessionID, v.StateText, v.Project, v.Agent, plural(int(v.Turns), "turn"))
	active := turnInProgress(s.Status)
	switch {
	case v.CurrentTool != "":
		fmt.Fprintf(w, "now: %s\n", oneLine(v.CurrentTool, 120))
	case active && v.LastMessage != "":
		fmt.Fprintf(w, "last: %s\n", oneLine(v.LastMessage, 120))
	}
	if f := v.LastFailure; f != nil {
		fmt.Fprintf(w, "failure: %s: %s%s\n", f.Kind, oneLine(f.Message, 200), suffixParen(f.Hint))
	}
	if d := v.DiffStat; d.FilesChanged > 0 {
		fmt.Fprintf(w, "changes: %s changed, +%d -%d (`aide session diff %s`)\n", plural(int(d.FilesChanged), "file"), d.Insertions, d.Deletions, v.SessionID)
	}
	if !full {
		return
	}
	if q := s.GetProgress().GetPendingQuestion(); q != nil {
		fmt.Fprintln(w)
		writeQuestion(w, q)
	} else if !active && v.Result != "" {
		fmt.Fprintf(w, "\n%s\n", strings.TrimSpace(v.Result))
	}
}

// turnInProgress: the session has a turn queued, starting, running or
// waiting for an answer.
func turnInProgress(st sessionv1.SessionStatus) bool {
	switch st {
	case sessionv1.SessionStatus_SESSION_STATUS_QUEUED, sessionv1.SessionStatus_SESSION_STATUS_PREPARING,
		sessionv1.SessionStatus_SESSION_STATUS_RUNNING, sessionv1.SessionStatus_SESSION_STATUS_WAITING_INPUT:
		return true
	}
	return false
}

func suffixParen(s string) string {
	if s == "" {
		return ""
	}
	return " (" + s + ")"
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) > n {
		return string([]rune(s)[:n]) + "…"
	}
	return s
}

func (a *app) printEvent(w io.Writer, e *sessionv1.SessionEvent) {
	switch k := e.Kind.(type) {
	case *sessionv1.SessionEvent_Status:
		fmt.Fprintf(w, "[%s]%s\n", statusName(k.Status.Status), suffix(k.Status.Reason))
	case *sessionv1.SessionEvent_TurnStarted:
		fmt.Fprintf(w, "── turn %d ──\n", k.TurnStarted.Turn)
	case *sessionv1.SessionEvent_MessageReceived:
		m := k.MessageReceived
		fmt.Fprintf(w, "✉ from %s (%s, %s): %s\n", firstNonEmpty(m.FromName, m.From), m.MessageId, deliveredText(m.Delivered), oneLine(m.Text, 200))
	case *sessionv1.SessionEvent_MessageSent:
		m := k.MessageSent
		fmt.Fprintf(w, "✉ to %s (%s): %s\n", firstNonEmpty(m.ToName, m.To), m.MessageId, oneLine(m.Text, 200))
	case *sessionv1.SessionEvent_WorkspaceRequest:
		r := k.WorkspaceRequest
		fmt.Fprintf(w, "⚑ workspace request %s (%s %s): %s, by %s%s\n", r.RequestId, r.Kind, oneLine(r.Item, 80), r.Status,
			firstNonEmpty(r.FromName, r.From), suffix(oneLine(r.Note, 200)))
	case *sessionv1.SessionEvent_UserMessage:
		who := ""
		switch m := k.UserMessage; {
		case m.From == "owner":
			who = "[owner] "
		case m.FromName != "":
			who = "[" + m.FromName + "] "
		}
		fmt.Fprintf(w, "> %s%s\n", who, oneLine(k.UserMessage.Text, 200))
		for _, f := range k.UserMessage.Attachments {
			fmt.Fprintf(w, "  attached: %s (%s, %d bytes)\n", f.Name, f.MediaType, f.Size)
		}
	case *sessionv1.SessionEvent_AgentMessage:
		fmt.Fprintln(w, strings.TrimSpace(k.AgentMessage.Text))
	case *sessionv1.SessionEvent_ToolCall:
		d := ""
		if k.ToolCall.Decision == "deny" {
			d = "  [denied: " + k.ToolCall.Reason + "]"
		}
		fmt.Fprintf(w, "• %s: %s%s\n", k.ToolCall.Name, oneLine(k.ToolCall.Summary, 120), d)
	case *sessionv1.SessionEvent_ToolResult:
		if k.ToolResult.IsError {
			fmt.Fprintf(w, "  ↳ error: %s\n", oneLine(k.ToolResult.Summary, 160))
		}
	case *sessionv1.SessionEvent_TurnCompleted:
		tc := k.TurnCompleted
		st := ""
		if d := tc.DiffStat; d != nil && d.FilesChanged > 0 {
			st = fmt.Sprintf(", %s +%d -%d", plural(int(d.FilesChanged), "file"), d.Insertions, d.Deletions)
		}
		if tc.IsError {
			fmt.Fprintf(w, "✗ turn %d failed%s: %s%s\n", tc.Turn, kindNote(tc.Failure), oneLine(firstNonEmpty(tc.Failure.GetMessage(), tc.Result), 200), st)
			if h := tc.Failure.GetHint(); h != "" {
				fmt.Fprintf(w, "  ↳ %s\n", h)
			}
		} else {
			fmt.Fprintf(w, "✓ turn %d done%s\n", tc.Turn, st)
		}
	case *sessionv1.SessionEvent_Error:
		fmt.Fprintf(w, "!%s %s\n", kindNote(k.Error.Failure), k.Error.Message)
	case *sessionv1.SessionEvent_QuestionAsked:
		writeQuestion(w, k.QuestionAsked.Question)
	case *sessionv1.SessionEvent_QuestionResolved:
		r := k.QuestionResolved
		switch r.Resolution {
		case sessionv1.QuestionResolution_QUESTION_RESOLUTION_ANSWERED:
			var parts []string
			for _, x := range r.Answers {
				parts = append(parts, (&provider.Answer{Selected: x.Selected, Text: x.Text}).Value())
			}
			fmt.Fprintf(w, "  ↳ answered by %s: %s\n", r.By, oneLine(strings.Join(parts, "; "), 200))
		case sessionv1.QuestionResolution_QUESTION_RESOLUTION_DECLINED:
			fmt.Fprintf(w, "  ↳ declined by %s\n", r.By)
		case sessionv1.QuestionResolution_QUESTION_RESOLUTION_TIMED_OUT:
			fmt.Fprintln(w, "  ↳ no answer: the agent continues on its own")
		default:
			fmt.Fprintln(w, "  ↳ no longer asked")
		}
	}
}

// writeQuestion prints a question as an agent (or a person) reads it.
func writeQuestion(w io.Writer, q *sessionv1.Question) {
	if q == nil {
		return
	}
	for _, it := range q.Items {
		h := ""
		if it.Header != "" {
			h = "[" + it.Header + "] "
		}
		fmt.Fprintf(w, "? %s%s\n", h, strings.TrimSpace(it.Text))
		for i, o := range it.Options {
			d := ""
			if o.Description != "" {
				d = " — " + o.Description
			}
			fmt.Fprintf(w, "  %d. %s%s\n", i+1, o.Label, d)
		}
		var notes []string
		if it.MultiSelect {
			notes = append(notes, "choose one or more")
		}
		if it.AllowOther && len(it.Options) > 0 {
			notes = append(notes, "or answer in your own words")
		}
		if it.Secret {
			notes = append(notes, "secret: passed to the agent, not recorded")
		}
		if len(q.Items) > 1 {
			notes = append(notes, "item "+it.Id)
		}
		if len(notes) > 0 {
			fmt.Fprintf(w, "  (%s)\n", strings.Join(notes, "; "))
		}
	}
}

func (a *app) answerCmd() *cobra.Command {
	var questionID, text string
	var choose, items []string
	var decline, wait bool
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "answer <session id>",
		Short: "Answer the question the agent asked (it waits for it)",
		Long: `Answer the agent's pending question: pick options with --choose (label or
number, repeatable for multi-select), give free text with --text, or answer
each item of a multi-part question with --item <header or id>=<value>.
--decline tells the agent nobody will answer, so it goes on on its own.
A message sent with ` + "`aide session send`" + ` while the agent waits also answers it.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, timeout)
				defer cancel()
			}
			return a.withMachine(ctx, args[0], func(c *peer.Client, n *peer.Machine, ref machineRef) error {
				before, err := n.Session.GetSession(ctx, &sessionv1.GetSessionRequest{SessionId: args[0]})
				if err != nil {
					return err
				}
				q := before.GetProgress().GetPendingQuestion()
				if q == nil {
					return &exitError{code: exitUsage, kind: "invalid_state", err: fmt.Errorf("session %s is not waiting for an answer (it is %s)", args[0], statusName(before.Status))}
				}
				req := &sessionv1.AnswerQuestionRequest{SessionId: args[0], QuestionId: firstNonEmpty(questionID, q.QuestionId), Decline: decline}
				if !decline {
					answers, err := buildAnswers(q, choose, text, items)
					if err != nil {
						return err
					}
					req.Answers = answers
				}
				res, err := n.Session.AnswerQuestion(ctx, req)
				if err != nil {
					return err
				}
				if !wait {
					return a.started(cmd, res, ref.Name)
				}
				return a.waitTurn(ctx, cmd, c, ref, args[0], before.GetProgress().GetLastEventSeq())
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&questionID, "question", "", "question id (default: the pending one)")
	f.StringArrayVarP(&choose, "choose", "c", nil, "option to pick, by label or number (repeatable)")
	f.StringVar(&text, "text", "", "free-text answer")
	f.StringArrayVar(&items, "item", nil, "answer one item of a multi-part question: <header or id>=<option or text> (repeatable)")
	f.BoolVar(&decline, "decline", false, "no answer: the agent goes on on its own")
	f.BoolVar(&wait, "wait", false, waitHelp)
	f.DurationVar(&timeout, "timeout", 0, timeoutHelp)
	return cmd
}

// buildAnswers turns the answer flags into answers to q's items.
func buildAnswers(q *sessionv1.Question, choose []string, text string, items []string) ([]*sessionv1.Answer, error) {
	pick := func(it *sessionv1.QuestionItem, v string) (string, bool) {
		v = strings.TrimSpace(v)
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= len(it.Options) {
			return it.Options[n-1].Label, true
		}
		for _, o := range it.Options {
			if strings.EqualFold(o.Label, v) {
				return o.Label, true
			}
		}
		return "", false
	}
	if len(items) > 0 {
		if len(choose) > 0 || text != "" {
			return nil, fail(exitUsage, "use either --item or --choose/--text")
		}
		var out []*sessionv1.Answer
		for _, kv := range items {
			k, v, ok := strings.Cut(kv, "=")
			if !ok {
				return nil, fail(exitUsage, "--item takes <header or id>=<value>, got %q", kv)
			}
			var it *sessionv1.QuestionItem
			for _, x := range q.Items {
				if x.Id == k || strings.EqualFold(x.Header, k) {
					it = x
				}
			}
			if it == nil {
				return nil, fail(exitUsage, "the question has no item %q", k)
			}
			a := &sessionv1.Answer{ItemId: it.Id}
			if l, ok := pick(it, v); ok {
				a.Selected = []string{l}
			} else {
				a.Text = v
			}
			out = append(out, a)
		}
		return out, nil
	}
	if len(q.Items) != 1 {
		return nil, fail(exitUsage, "the question has %d items: answer each with --item <header or id>=<value>", len(q.Items))
	}
	if len(choose) == 0 && strings.TrimSpace(text) == "" {
		return nil, fail(exitUsage, "give an answer with --choose or --text (or --decline)")
	}
	it := q.Items[0]
	a := &sessionv1.Answer{ItemId: it.Id, Text: text}
	for _, c := range choose {
		l, ok := pick(it, c)
		if !ok {
			return nil, fail(exitUsage, "no option %q (options: %s)", c, labels(it))
		}
		a.Selected = append(a.Selected, l)
	}
	return []*sessionv1.Answer{a}, nil
}

func labels(it *sessionv1.QuestionItem) string {
	var l []string
	for i, o := range it.Options {
		l = append(l, fmt.Sprintf("%d. %s", i+1, o.Label))
	}
	return strings.Join(l, ", ")
}

func kindNote(f *sessionv1.Failure) string {
	if k := failureName(f.GetKind()); k != "" {
		return " (" + k + ")"
	}
	return ""
}

func suffix(s string) string {
	if s == "" {
		return ""
	}
	return " " + s
}

func (a *app) getCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <session id>",
		Short: "Show a session: its status and progress; its question when it asks one; the agent's full result once a turn is over",
		Long: `Show a session: its status and progress; its question when it asks one; the agent's full result once a turn is over.

A DSH native session (dsh_…, from session list --native) shows as DeepSeek Harness keeps it: its title,
state, folder and last activity, read-only.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if isNative(args[0]) {
				return a.getNative(cmd, args[0])
			}
			err := a.withMachine(cmd.Context(), args[0], func(c *peer.Client, n *peer.Machine, ref machineRef) error {
				s, err := n.Session.GetSession(cmd.Context(), &sessionv1.GetSessionRequest{SessionId: args[0]})
				if err != nil {
					return err
				}
				return a.showSession(cmd, s, ref.Name, true)
			})
			return a.lastKnown(cmd, args[0], err)
		},
	}
}

func (a *app) waitCmd() *cobra.Command {
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "wait <session id>",
		Short: "Wait for a session's turn to finish, then show it as session get does (exit code: how its last turn went)",
		Long: `Wait for the turn under way to finish, then print the session with the agent's full result, as
start, send and answer do with --wait: for a session started without --wait, or after a wait that
timed out. It sends nothing and changes nothing.

A session that is not working returns at once: idle or crashed, with the exit code of its last turn
(0, or its failure: 5, 7); waiting for an answer, 8 and the question; archived, 5. Questions are never
answered by waiting: the wait ends as the agent asks one. --timeout ends the wait, not the turn (6).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// The connection outlives the wait, so a wait that times out
			// still shows the session as it is then.
			ctx := cmd.Context()
			return a.withMachine(ctx, args[0], func(c *peer.Client, n *peer.Machine, ref machineRef) error {
				s, err := n.Session.GetSession(ctx, &sessionv1.GetSessionRequest{SessionId: args[0]})
				if err != nil {
					return err
				}
				a.keepSeen(s.GetMachineId(), time.Now(), s)
				switch s.Status {
				case sessionv1.SessionStatus_SESSION_STATUS_WAITING_INPUT:
					return a.outcome(cmd.OutOrStdout(), ref.Name, s, &exitError{code: exitNeedsInput, kind: "needs_input", err: fmt.Errorf("session %s is waiting for an answer", args[0]),
						hint: fmt.Sprintf("`aide session answer %s --choose <option>` or `--text <answer>` (or reply with `aide session send`)", args[0])})
				case sessionv1.SessionStatus_SESSION_STATUS_IDLE, sessionv1.SessionStatus_SESSION_STATUS_CRASHED, sessionv1.SessionStatus_SESSION_STATUS_ARCHIVED:
					return a.outcome(cmd.OutOrStdout(), ref.Name, s, turnError(s, nil, s.Status))
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "session %s is %s; waiting for its turn to finish\n", args[0], statusName(s.Status))
				if timeout > 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, timeout)
					defer cancel()
				}
				return a.waitTurn(ctx, cmd, c, ref, args[0], s.GetProgress().GetLastEventSeq())
			})
		},
	}
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "stop waiting after this long (exit code 6; the turn goes on). Pass one below the time limit of the tool running this command")
	return cmd
}

// lastKnown is session get's answer when the session's machine cannot be
// read (err, exit code 4): the error, with the session as this client last
// read it, marked lost or being checked (docs/session-states.md §6).
func (a *app) lastKnown(cmd *cobra.Command, id string, err error) error {
	var ee *exitError
	if !errors.As(err, &ee) || ee.kind != "unreachable" {
		return err
	}
	s, readAt := a.seenSession(id)
	if s == nil {
		return err
	}
	v := heldView(s, ee.machine, ee.why, ee.heard, readAt)
	w := cmd.OutOrStdout()
	if a.output == "json" {
		return a.report(w, err, field{"session", v})
	}
	fmt.Fprintf(w, "session %s · %s · %s · %s · last read %s ago\n", v.SessionID, v.StateText, v.Project, v.Agent, approx(time.Since(readAt)))
	return err
}

func (a *app) listCmd() *cobra.Command {
	var machineFlag, status string
	var since time.Duration
	var archived, native bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List this client's sessions on all reachable machines (not the archived ones unless --archived)",
		Long: `List this client's sessions on all reachable machines (not the archived ones unless --archived),
and those of others when the grant lets it see sessions it did not start.

--native also lists the DeepSeek Harness sessions Aide did not start (the owner's own in DSH
Desktop, another tool's), as DSH keeps them on each machine: read-only, with ids dsh_…, for
session get and session events. They are listed only to a grant that sees sessions it did not
start, in the projects it covers. Their state is running, idle, ended or unknown; --status
running or idle, --archived and --since (by last activity) apply to them too.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var st sessionv1.SessionStatus
			if archived {
				status = "archived"
			}
			if status != "" {
				v, ok := sessionv1.SessionStatus_value["SESSION_STATUS_"+strings.ToUpper(status)]
				if !ok {
					return fail(exitUsage, "unknown status %q", status)
				}
				st = sessionv1.SessionStatus(v)
			}
			var sinceTS *time.Time
			if since > 0 {
				t := time.Now().Add(-since)
				sinceTS = &t
			}
			return a.listSessions(cmd, machineFlag, st, sinceTS, native)
		},
	}
	cmd.Flags().StringVar(&machineFlag, "machine", "", "machine name or id")
	cmd.Flags().BoolVar(&archived, "archived", false, "list the archived sessions instead")
	cmd.Flags().BoolVar(&native, "native", false, "also list the DeepSeek Harness sessions Aide did not start (read-only, ids dsh_…)")
	cmd.Flags().StringVar(&status, "status", "", "filter by status (queued, running, waiting_input, idle, crashed, archived)")
	cmd.Flags().DurationVar(&since, "since", 0, "only sessions updated within this duration")
	return cmd
}

func (a *app) listSessions(cmd *cobra.Command, machineFlag string, status sessionv1.SessionStatus, since *time.Time, native bool) error {
	ctx := cmd.Context()
	c, machines, err := a.connect(ctx, warmTargets(machineFlag))
	if err != nil {
		return err
	}
	defer c.Close()
	machines, err = targetMachines(machines, machineFlag)
	if err != nil {
		return err
	}
	names := map[string]string{}
	for _, n := range machines {
		names[n.ID] = n.Name
	}
	got := map[string][]*sessionv1.Session{}
	dsh := map[string][]*machinev1.DSHSession{}
	var mu sync.Mutex
	errs := c.FanOut(ctx, ids(machines), func(ctx context.Context, n *peer.Machine) error {
		req := &sessionv1.ListSessionsRequest{Status: status}
		if since != nil {
			req.Since = timestampOf(*since)
		}
		r, err := n.Session.ListSessions(ctx, req)
		if err == nil {
			mu.Lock()
			got[n.MachineID] = r.Sessions
			names[n.MachineID] = n.Name // as the machine itself says
			mu.Unlock()
		}
		if err != nil || !native {
			return err
		}
		d, err := n.Info.ListDSHSessions(ctx, &machinev1.ListDSHSessionsRequest{})
		if connect.CodeOf(err) == connect.CodeUnimplemented {
			return nil // a machine from before agents read them: it lists none
		}
		if err == nil {
			mu.Lock()
			dsh[n.MachineID] = d.Sessions
			mu.Unlock()
		}
		return err
	})
	out := &sessionv1.ListSessionsResponse{}
	for _, n := range machines {
		for _, s := range got[n.ID] {
			if status == 0 && s.Status == sessionv1.SessionStatus_SESSION_STATUS_ARCHIVED {
				continue
			}
			out.Sessions = append(out.Sessions, s)
		}
	}
	nativeList := []*nativeSessionJSON{}
	statusText := ""
	if status != 0 {
		statusText = statusName(status)
	}
	for _, n := range machines {
		for _, s := range dsh[n.ID] {
			if nativeShown(s, statusText, since) {
				nativeList = append(nativeList, nativeView(s, n.ID, names[n.ID]))
			}
		}
	}
	w := cmd.OutOrStdout()
	offline := offlineOf(machines, errs)
	now := time.Now()
	for id, ss := range got {
		a.keepSeen(id, now, ss...)
	}
	lost := a.lostSessions(offline, status, since)
	if a.output == "json" {
		views := []*sessionJSON{}
		for _, s := range out.Sessions {
			views = append(views, sessionView(s, names[s.MachineId]))
		}
		data := []field{{"sessions", views}}
		if native {
			data = append(data, field{"native_sessions", nativeList})
		}
		return a.report(w, nil, append(data, field{"lost_sessions", lost}, field{"offline", offline})...)
	}
	if len(out.Sessions) == 0 {
		fmt.Fprintln(w, "no sessions")
	} else {
		tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
		fmt.Fprintln(tw, "SESSION\tTITLE\tSTATE\tPROJECT\tAGENT\tTURNS\tNOW / LAST")
		for _, s := range out.Sessions {
			v := sessionView(s, names[s.MachineId])
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d\t%s\n", v.SessionID, oneLine(v.Title, 40), v.StateText, v.Project, v.Agent, v.Turns, oneLine(firstNonEmpty(v.CurrentTool, v.LastMessage, v.Result), 60))
		}
		tw.Flush()
	}
	if len(lost) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "LOST: last known, as this client last read them; their machines cannot be read now")
		tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
		fmt.Fprintln(tw, "SESSION\tTITLE\tSTATE\tPROJECT\tAGENT\tLAST EVENT")
		for _, v := range lost {
			last := "-"
			if v.LastEventAge != nil {
				last = sessionstate.Ago(time.Duration(*v.LastEventAge)*time.Second) + " ago"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", v.SessionID, oneLine(v.Title, 40), v.StateText, v.Project, v.Agent, last)
		}
		tw.Flush()
	}
	if native {
		fmt.Fprintln(w)
		writeNative(w, nativeList)
	}
	for _, m := range offline {
		fmt.Fprintf(w, "offline: %s (%s); its sessions are not listed\n", m.Machine, offlineText(m))
	}
	return nil
}

func (a *app) eventsCmd() *cobra.Command {
	var after, before uint64
	var limit uint32
	cmd := &cobra.Command{
		Use:   "events <session id>",
		Short: "Show a session's events (messages, tool calls, turns, questions), a page at a time: the latest by default",
		Long: `Show a session's events, oldest first, a page at a time. Each event has a
seq; without a cursor the page is the latest --limit events.

  --after SEQ   the events right after SEQ: what happened since you last
                looked (pass the last seq you saw, or the session's
                last_event_seq from session get)
  --before SEQ  the events right before SEQ: older ones

The JSON page object has first_seq and last_seq (cursors for --before and
--after) and has_more_before / has_more_after. With --after and nothing
new, events is empty and last_seq is still SEQ.

A DSH native session's (dsh_…) are its conversation as DeepSeek Harness saved it.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if after > 0 && before > 0 {
				return fail(exitUsage, "give --after or --before, not both")
			}
			if isNative(args[0]) {
				return a.readNative(cmd.Context(), args[0], &machinev1.ReadDSHSessionRequest{AfterSeq: after, BeforeSeq: before, Limit: limit},
					func(res *machinev1.ReadDSHSessionResponse, _ machineRef) error {
						return a.showEvents(cmd, res.Events, res.HasMoreBefore, res.HasMoreAfter, after, before)
					})
			}
			return a.withMachine(cmd.Context(), args[0], func(c *peer.Client, n *peer.Machine, ref machineRef) error {
				res, err := n.Session.ListEvents(cmd.Context(), &sessionv1.ListEventsRequest{SessionId: args[0], AfterSeq: after, BeforeSeq: before, Limit: limit})
				if err != nil {
					return err
				}
				return a.showEvents(cmd, res.Events, res.HasMoreBefore, res.HasMoreAfter, after, before)
			})
		},
	}
	f := cmd.Flags()
	f.Uint32Var(&limit, "limit", 50, "events per page (at most 1000)")
	f.Uint64Var(&after, "after", 0, "the events right after this seq")
	f.Uint64Var(&before, "before", 0, "the events right before this seq")
	return cmd
}

// showEvents prints a page of events that session events asked for with
// after or before.
func (a *app) showEvents(cmd *cobra.Command, events []*sessionv1.SessionEvent, moreBefore, moreAfter bool, after, before uint64) error {
	first, last := before, after // an empty page keeps the cursor
	if len(events) > 0 {
		first, last = events[0].Seq, events[len(events)-1].Seq
	}
	w := cmd.OutOrStdout()
	if a.output == "json" {
		evs := []object{}
		for _, e := range events {
			evs = append(evs, eventView(e))
		}
		return a.report(w, nil, field{"events", evs}, field{"page", object{{"first_seq", first}, {"last_seq", last},
			{"has_more_before", moreBefore}, {"has_more_after", moreAfter}}})
	}
	for _, e := range events {
		a.printEvent(w, e)
	}
	note := fmt.Sprintf("events %d–%d", first, last)
	if len(events) == 0 {
		note = "no events here"
	}
	if moreBefore {
		note += fmt.Sprintf("; earlier: --before %d", first)
	}
	if moreAfter {
		note += fmt.Sprintf("; later: --after %d", last)
	}
	fmt.Fprintln(cmd.ErrOrStderr(), note)
	return nil
}

func (a *app) sendCmd() *cobra.Command {
	var message, file string
	var files []string
	var wait bool
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:    "send <session id>",
		Hidden: true, // `message send --to <session>` took its place (docs/agent-messages.md §7.5)
		Short:  "Send a message to a session; while it works, the message joins the running turn",
		Long: `Send a message to a session.

Attach files with -f (repeatable). An idle session starts a new turn. While a turn runs, Claude Code and Codex
take the message into that turn (at the next tool boundary); agents that
take input only between turns (ACP) run it as the next turn, right after.
--wait returns once the agent has handled every message sent so far.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.ErrOrStderr(), "note: `aide session send` is now `aide message send --to %s`\n", args[0])
			text, err := readPrompt("message", message, file, len(files))
			if err != nil {
				return err
			}
			atts, err := readFiles(files)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			if timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, timeout)
				defer cancel()
			}
			return a.withMachine(ctx, args[0], func(c *peer.Client, n *peer.Machine, ref machineRef) error {
				before, err := n.Session.GetSession(ctx, &sessionv1.GetSessionRequest{SessionId: args[0]})
				if err != nil {
					return err
				}
				if before.Status == sessionv1.SessionStatus_SESSION_STATUS_ARCHIVED {
					return &exitError{code: exitUsage, kind: "invalid_state", err: fmt.Errorf("session %s is archived (read-only)", args[0]),
						hint: fmt.Sprintf("`aide session unarchive %s` to go on with it", args[0])}
				}
				res, err := n.Session.SendMessage(ctx, &sessionv1.SendMessageRequest{SessionId: args[0], Text: text, Attachments: atts})
				if err != nil {
					return err
				}
				if !wait {
					return a.started(cmd, res.Session, ref.Name)
				}
				return a.waitTurn(ctx, cmd, c, ref, args[0], before.GetProgress().GetLastEventSeq())
			})
		},
	}
	cmd.Flags().StringVar(&message, "message", "", "message text")
	cmd.Flags().StringArrayVarP(&files, "file", "f", nil, "attach a file (repeatable)")
	cmd.Flags().StringVar(&file, "message-file", "", "read the message from a file (- for stdin)")
	cmd.Flags().BoolVar(&wait, "wait", false, waitHelp)
	cmd.Flags().DurationVar(&timeout, "timeout", 0, timeoutHelp)
	return cmd
}

func (a *app) interruptCmd() *cobra.Command {
	return a.sessionVerb("interrupt", "Stop the turn in progress; the session stays, and the next message goes on in the same conversation",
		func(ctx context.Context, n *peer.Machine, id string) (*sessionv1.Session, error) {
			return n.Session.InterruptSession(ctx, &sessionv1.InterruptSessionRequest{SessionId: id})
		})
}

func (a *app) archiveCmd() *cobra.Command {
	return a.sessionVerb("archive", "Archive a session you are done with: it becomes read-only and its worktree is freed (its branch keeps the work)",
		func(ctx context.Context, n *peer.Machine, id string) (*sessionv1.Session, error) {
			return n.Session.ArchiveSession(ctx, &sessionv1.ArchiveSessionRequest{SessionId: id})
		})
}

func (a *app) unarchiveCmd() *cobra.Command {
	return a.sessionVerb("unarchive", "Bring an archived session back: its worktree is restored and it takes messages again",
		func(ctx context.Context, n *peer.Machine, id string) (*sessionv1.Session, error) {
			return n.Session.UnarchiveSession(ctx, &sessionv1.UnarchiveSessionRequest{SessionId: id})
		})
}

// sessionVerb is a command that does one thing to a session and prints it.
func (a *app) sessionVerb(verb, short string, do func(ctx context.Context, n *peer.Machine, id string) (*sessionv1.Session, error)) *cobra.Command {
	return &cobra.Command{
		Use:   verb + " <session id>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.withMachine(cmd.Context(), args[0], func(c *peer.Client, n *peer.Machine, ref machineRef) error {
				s, err := do(cmd.Context(), n, args[0])
				if err != nil {
					return err
				}
				return a.showSession(cmd, s, ref.Name, false)
			})
		},
	}
}

func (a *app) diffCmd() *cobra.Command {
	var turn uint32
	var stat bool
	cmd := &cobra.Command{
		Use:   "diff <session id>",
		Short: "Show the session's changes (cumulative, or one --turn; worktree projects only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.withMachine(cmd.Context(), args[0], func(c *peer.Client, n *peer.Machine, ref machineRef) error {
				w := cmd.OutOrStdout()
				var offset uint64
				var patch strings.Builder
				for {
					d, err := n.Session.GetDiff(cmd.Context(), &sessionv1.GetDiffRequest{SessionId: args[0], Turn: turn, StatOnly: stat, Offset: offset})
					if err != nil {
						return err
					}
					if a.output != "json" && stat {
						s := d.GetStat()
						fmt.Fprintf(w, "%s changed, +%d -%d (%s..%s)\n", plural(int(s.GetFilesChanged()), "file"), s.GetInsertions(), s.GetDeletions(), short(d.BaseCommit), short(d.HeadCommit))
						return nil
					}
					if a.output != "json" {
						fmt.Fprint(w, d.Diff)
					} else {
						patch.WriteString(d.Diff)
					}
					if !d.Truncated {
						if a.output != "json" {
							return nil
						}
						return a.report(w, nil, field{"diff", object{{"base_commit", d.BaseCommit}, {"head_commit", d.HeadCommit},
							{"stat", diffStatView(d.Stat)}, {"patch", patch.String()}}})
					}
					offset = d.NextOffset
				}
			})
		},
	}
	cmd.Flags().Uint32Var(&turn, "turn", 0, "only this turn's changes")
	cmd.Flags().BoolVar(&stat, "stat", false, "summary only")
	return cmd
}

func (a *app) pushCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "push <session id>",
		Short: "Push a session's branch to its repository's remote, with the machine's own git login (worktree sessions only; Aide never pushes unasked)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.withMachine(cmd.Context(), args[0], func(c *peer.Client, n *peer.Machine, ref machineRef) error {
				res, err := n.Session.PushSession(cmd.Context(), &sessionv1.PushSessionRequest{SessionId: args[0]})
				if err != nil {
					return err
				}
				if a.output == "json" {
					return a.report(cmd.OutOrStdout(), nil, field{"push", object{{"session_id", args[0]}, {"branch", res.Branch}, {"pull_request_url", res.PullRequestUrl}}})
				}
				fmt.Fprintf(cmd.OutOrStdout(), "pushed %s to the remote origin\n", res.Branch)
				if res.PullRequestUrl != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "open a pull request: %s\n", res.PullRequestUrl)
				}
				return nil
			})
		},
	}
}

func short(c string) string {
	if len(c) > 10 {
		return c[:10]
	}
	return c
}

// lostSessions is what this client last read of the sessions of the
// machines that cannot be read now (as session list filters them),
// marked lost or being checked, newest first.
func (a *app) lostSessions(offline []offlineMachine, status sessionv1.SessionStatus, since *time.Time) []*sessionJSON {
	out := []*sessionJSON{}
	for _, m := range offline {
		var heard time.Time
		if m.LastSeen != nil {
			heard, _ = time.Parse(time.RFC3339, *m.LastSeen)
		}
		ss, readAt := a.seenSessions(m.MachineID)
		for _, s := range ss {
			switch {
			case status == 0 && s.Status == sessionv1.SessionStatus_SESSION_STATUS_ARCHIVED:
			case status != 0 && s.Status != status:
			case since != nil && s.GetUpdatedAt().AsTime().Before(*since):
			default:
				out = append(out, heldView(s, m.Machine, cmp.Or(m.Error, "offline"), heard, readAt))
			}
		}
	}
	return out
}
