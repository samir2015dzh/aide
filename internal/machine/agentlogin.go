package machine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	"github.com/samir2015dzh/aide/internal/provider/claude"
)

// Signing a coding agent in from Aide (工单 #51, the owner's choice A): on
// the owner's word, aided starts the agent's own sign-in here, `claude auth
// login`, which opens the browser on this machine; the owner signs in there.
// Aide never reads, keeps or sends the account, the password or the token:
// what the command prints goes nowhere but its last line, kept as the reason
// when it fails, with anything token-like masked. When it ends the agent is
// checked again, and the sign-in succeeded only if the agent then says it is
// signed in. The Claude desktop app and its sessions are not touched.

// LoginTimeout is how long a sign-in may wait for the owner in the browser.
var LoginTimeout = 10 * time.Minute

type agentLogin struct {
	st     *machinev1.AgentLogin
	cancel context.CancelFunc
}

func (n *Machine) loginOfLocked(name string) *machinev1.AgentLogin {
	if l := n.logins[name]; l != nil {
		return l.st
	}
	return nil
}

// StartAgentLogin starts the sign-in of agent here, or (cancel) stops the
// one under way; either way it answers with the sign-in's state.
func (n *Machine) StartAgentLogin(name string, cancel bool) (*machinev1.AgentLogin, error) {
	if name != "claude" {
		return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("signing %s in from Aide is not supported: only Claude Code", name))
	}
	n.mu.Lock()
	cur := n.logins[name]
	if cur != nil && cur.st.State == machinev1.AgentLoginState_AGENT_LOGIN_STATE_WAITING {
		if cancel {
			cur.cancel()
		}
		st := cur.st
		n.mu.Unlock()
		return st, nil
	}
	if cancel {
		n.mu.Unlock()
		return cur.GetSt(), nil
	}
	p, _ := n.provider(name).(*claude.Provider)
	n.mu.Unlock()
	if p == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("Claude Code is not set up on this machine"))
	}
	ctx, stop := context.WithTimeout(context.Background(), LoginTimeout)
	cmd := p.LoginCommand(ctx)
	if cmd == nil {
		stop()
		return nil, connect.NewError(connect.CodeNotFound, errors.New("Claude Code's command line is not installed on this machine"))
	}
	var tail lastLine
	cmd.Stdout, cmd.Stderr = io.Discard, &tail
	if err := cmd.Start(); err != nil {
		stop()
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("could not start Claude Code's sign-in: %w", err))
	}
	l := &agentLogin{st: &machinev1.AgentLogin{State: machinev1.AgentLoginState_AGENT_LOGIN_STATE_WAITING, StartedAt: timestamppb.Now()}, cancel: stop}
	n.mu.Lock()
	n.logins[name] = l
	n.mu.Unlock()
	n.audit.Log(AuditRecord{Kind: "agent.login", Subject: "owner", Decision: "allow", Detail: name})
	n.kickStatus()
	go func() {
		err := cmd.Wait()
		cancelled := errors.Is(ctx.Err(), context.Canceled)
		timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
		stop()
		// What the agent says now decides, not how the command ended.
		<-n.CheckAgents(true, 0)
		n.mu.Lock()
		in := n.infos[name].LoggedIn
		st := &machinev1.AgentLogin{StartedAt: l.st.StartedAt, EndedAt: timestamppb.Now()}
		switch {
		case in:
			st.State = machinev1.AgentLoginState_AGENT_LOGIN_STATE_SUCCEEDED
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

// GetSt is the state of a sign-in, nil for none.
func (l *agentLogin) GetSt() *machinev1.AgentLogin {
	if l == nil {
		return nil
	}
	return l.st
}

// lastLine keeps the last non-empty line written to it, at most 200
// characters, with anything that looks like a token masked: the reason a
// sign-in failed, never a secret.
type lastLine struct{ line string }

var tokenLike = regexp.MustCompile(`[A-Za-z0-9_\-.~+/=]{24,}`)

func (w *lastLine) Write(b []byte) (int, error) {
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			w.line = l
		}
	}
	return len(b), nil
}

func (w *lastLine) String() string {
	s := tokenLike.ReplaceAllString(w.line, "…")
	if r := []rune(s); len(r) > 200 {
		s = string(r[:200]) + "…"
	}
	return s
}
