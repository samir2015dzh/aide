package machine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	"github.com/samir2015dzh/aide/internal/provider"
	"github.com/samir2015dzh/aide/internal/provider/claude"
	"github.com/samir2015dzh/aide/internal/reason"
)

// Signing a coding agent in from Aide (工单 #51, the owner's choice A; 工单
// #118): on the owner's word, aided starts the agent's own sign-in here,
// `claude auth login`. A browser it opens on this machine may never show (no
// desktop to show it on, or none the owner is at), so Aide does not count on
// one: the address the sign-in prints is sent to the owner's devices, where
// they open it on any device; the code the page then shows comes back the
// same way and is given to the sign-in, which finishes. Aide never reads,
// keeps or sends the account, the password or the token; the address is
// held only while the sign-in runs, the code only written to it. What else
// the command prints goes nowhere but its last line, kept as the reason when
// it fails, with anything token-like masked. When it ends the agent is
// checked again (`claude auth status`), and the sign-in succeeded only if
// the agent then says it is signed in. The Claude desktop app and its
// sessions are not touched.

// LoginTimeout is how long a sign-in may wait for the owner (工单 #118: 15
// minutes, the time its address and code stay good for).
var LoginTimeout = 15 * time.Minute

// LoginURLWait is how long a sign-in may take to print the address to open:
// one that prints none is stopped and failed, said why, not left waiting.
var LoginURLWait = 30 * time.Second

// loginNoURL is why a sign-in failed that printed no address to open.
const loginNoURL = "Claude Code's sign-in printed no address to open"

type agentLogin struct {
	st     *machinev1.AgentLogin
	cancel context.CancelFunc
	stdin  io.WriteCloser
}

func (n *Machine) loginOfLocked(name string) *machinev1.AgentLogin {
	if l := n.logins[name]; l != nil {
		return l.st
	}
	return nil
}

// StartAgentLogin starts the sign-in of agent here; with code, gives the one
// under way the code its address's page showed; or (cancel) stops the one
// under way. It answers with the sign-in's state.
func (n *Machine) StartAgentLogin(name string, cancel bool, code ...string) (*machinev1.AgentLogin, error) {
	if name != "claude" {
		return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("signing %s in from Aide is not supported: only Claude Code", name))
	}
	given := strings.TrimSpace(strings.Join(code, ""))
	n.mu.Lock()
	cur := n.logins[name]
	if cur != nil && cur.st.State == machinev1.AgentLoginState_AGENT_LOGIN_STATE_WAITING {
		defer n.mu.Unlock()
		switch {
		case cancel:
			cur.cancel()
		case given != "":
			if cur.st.Url == "" {
				return nil, reason.New(connect.CodeFailedPrecondition, "login_no_url", "Claude Code's sign-in has not printed its address yet")
			}
			if strings.ContainsAny(given, "\r\n") || len(given) > 4096 {
				return nil, reason.New(connect.CodeInvalidArgument, "login_bad_code", "not a sign-in code")
			}
			if _, err := io.WriteString(cur.stdin, given+"\n"); err != nil {
				return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("could not give the code to Claude Code's sign-in: %w", err))
			}
			st := cloneLogin(cur.st)
			st.CodeSent = true
			cur.st = st
			go n.kickStatus()
		}
		return cur.st, nil
	}
	if cancel || given != "" {
		n.mu.Unlock()
		if given != "" {
			return nil, reason.New(connect.CodeFailedPrecondition, "login_not_waiting", "no sign-in of Claude Code is under way here")
		}
		return cur.GetSt(), nil
	}
	p, _ := n.provider(name).(*claude.Provider)
	n.mu.Unlock()
	if p == nil {
		return nil, reason.New(connect.CodeNotFound, "claude_not_set_up", "Claude Code is not set up on this machine")
	}
	ctx, stop := context.WithTimeout(context.Background(), LoginTimeout)
	cmd := p.LoginCommand(ctx)
	if cmd == nil {
		stop()
		return nil, reason.New(connect.CodeNotFound, "claude_cli_missing", "Claude Code's command line is not installed on this machine")
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		stop()
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("could not start Claude Code's sign-in: %w", err))
	}
	l := &agentLogin{st: &machinev1.AgentLogin{State: machinev1.AgentLoginState_AGENT_LOGIN_STATE_WAITING, StartedAt: timestamppb.Now()}, cancel: stop, stdin: stdin}
	var tail lastLine
	found := make(chan struct{})
	var once sync.Once
	url := &loginURL{found: func(u string) {
		once.Do(func() {
			n.mu.Lock()
			if l.st.State == machinev1.AgentLoginState_AGENT_LOGIN_STATE_WAITING {
				st := cloneLogin(l.st)
				st.Url = u
				l.st = st
			}
			n.mu.Unlock()
			close(found)
			n.kickStatus()
		})
	}}
	// The address may come on either; the reason it failed is its last line.
	cmd.Stdout, cmd.Stderr = url, io.MultiWriter(url, &tail)
	// Cancelled or out of time, it ends with what it started, and its state
	// says so at once (no child left holding its output open).
	wait, err := provider.StartInGroup(cmd)
	if err != nil {
		stop()
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("could not start Claude Code's sign-in: %w", err))
	}
	n.mu.Lock()
	n.logins[name] = l
	n.mu.Unlock()
	n.audit.Log(AuditRecord{Kind: "agent.login", Subject: "owner", Decision: "allow", Detail: name})
	n.kickStatus()
	noURL := false
	go func() {
		select {
		case <-found:
		case <-ctx.Done():
		case <-time.After(LoginURLWait):
			n.mu.Lock()
			noURL = true
			n.mu.Unlock()
			stop()
		}
	}()
	go func() {
		err := wait()
		stdin.Close()
		n.mu.Lock()
		none := noURL
		n.mu.Unlock()
		cancelled := !none && errors.Is(ctx.Err(), context.Canceled)
		timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
		stop()
		// What the agent says now decides, not how the command ended; its
		// models are the account's, asked again on the owner's sign-in (工单 #112).
		<-n.CheckAgentsOnPurpose()
		n.mu.Lock()
		in := n.infos[name].LoggedIn
		st := &machinev1.AgentLogin{StartedAt: l.st.StartedAt, EndedAt: timestamppb.Now()}
		switch {
		case in:
			st.State = machinev1.AgentLoginState_AGENT_LOGIN_STATE_SUCCEEDED
		case none:
			st.State, st.Detail = machinev1.AgentLoginState_AGENT_LOGIN_STATE_FAILED, loginNoURL
		case cancelled:
			st.State = machinev1.AgentLoginState_AGENT_LOGIN_STATE_CANCELLED
		case timedOut:
			st.State = machinev1.AgentLoginState_AGENT_LOGIN_STATE_TIMED_OUT
		default:
			st.State = machinev1.AgentLoginState_AGENT_LOGIN_STATE_FAILED
			st.Detail = tail.String()
			if st.Detail == "" && err != nil {
				st.Detail = err.Error()
			}
		}
		l.st = st
		n.mu.Unlock()
		n.kickStatus()
	}()
	return l.st, nil
}

func cloneLogin(l *machinev1.AgentLogin) *machinev1.AgentLogin {
	return &machinev1.AgentLogin{State: l.State, Detail: l.Detail, StartedAt: l.StartedAt, EndedAt: l.EndedAt, Url: l.Url, CodeSent: l.CodeSent}
}

// GetSt is the state of a sign-in, nil for none.
func (l *agentLogin) GetSt() *machinev1.AgentLogin {
	if l == nil {
		return nil
	}
	return l.st
}

// loginURL finds the first web address a sign-in prints ("…visit:
// https://<its sign-in host>/oauth/authorize?…"), on its own line or not.
type loginURL struct {
	mu    sync.Mutex
	buf   string
	found func(string)
}

var webAddress = regexp.MustCompile(`https://[^\s"'<>\x1b]+`)

func (w *loginURL) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf += string(b)
	// A line not ended yet may hold an address not printed whole.
	done := w.buf
	if i := strings.LastIndexAny(done, "\r\n"); i >= 0 {
		done, w.buf = done[:i], done[i+1:]
	} else {
		done = ""
		if len(w.buf) > 8192 {
			w.buf = w.buf[len(w.buf)-8192:]
		}
	}
	if u := webAddress.FindString(done); u != "" {
		w.found(strings.TrimRight(u, ".,);"))
	}
	return len(b), nil
}

// lastLine keeps the last non-empty line written to it, at most 200
// characters, with anything that looks like a token masked: the reason a
// sign-in failed, never a secret.
type lastLine struct {
	mu   sync.Mutex
	line string
}

var tokenLike = regexp.MustCompile(`[A-Za-z0-9_\-.~+/=]{24,}`)

func (w *lastLine) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			w.line = l
		}
	}
	return len(b), nil
}

func (w *lastLine) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := tokenLike.ReplaceAllString(w.line, "…")
	if r := []rune(s); len(r) > 200 {
		s = string(r[:200]) + "…"
	}
	return s
}
