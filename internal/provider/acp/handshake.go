package acp

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/samir2015dzh/aide/internal/provider"
)

// Greeting is what an agent said when started once for the handshake.
type Greeting struct {
	// Title and Version are what initialize's agentInfo says it is.
	Title, Version string
	// Auth are the sign-in methods it offers (their ids).
	Auth []string
	// Session: it opened a new session.
	Session bool
	// ProtocolVersion negotiated during initialize (Aide speaks 1).
	ProtocolVersion uint32
	// Capabilities truthfully derived from initialize's agentCapabilities.
	Capabilities provider.Capabilities
	// Models announced by session/new during the handshake, when dir != "".
	Models []provider.Model
}

// Handshake starts the agent once in env and does the ACP handshake:
// initialize and, when dir is set, session/new there (sent no prompt); then
// stops it. The error says why it fell short, with what the agent printed
// when it printed something.
func (p *Provider) Handshake(ctx context.Context, env []string, dir string) (Greeting, error) {
	bin, pre, extra := p.command()
	if !provider.Exists(bin) {
		return Greeting{}, fmt.Errorf("%s not found", p.Command)
	}
	env = append(append(slices.Clip(env), extra...), p.Env...)
	proc, err := provider.StartProc(ctx, bin, append(slices.Clip(pre), p.Args...), cmp.Or(p.Dir, dir), env)
	if err != nil {
		return Greeting{}, fmt.Errorf("could not start %s: %w", bin, err)
	}
	s := &session{name: p.Name(), proc: proc, events: make(chan provider.Event, 256), pending: map[int64]func(rpcResult){},
		gate: func(context.Context, provider.ToolAction) provider.Decision {
			return provider.Decision{Reason: "a handshake runs no tools"}
		}, ctx: ctx, known: map[string]*toolCall{}, calls: map[string]provider.Decision{}, finished: map[string]bool{}}
	go s.read()
	go func() { // nothing comes of a handshake's session
		for range s.events {
		}
	}()
	defer proc.Stop(time.Second)
	why := func(step string, err error) error {
		// A failed write or read may come just before the process is seen
		// to end: give it a moment, unless the agent answered or time is up.
		var answered *rpcError
		wait := time.NewTimer(0)
		if !errors.As(err, &answered) && ctx.Err() == nil {
			wait.Reset(2 * time.Second)
		}
		defer wait.Stop()
		select {
		case <-proc.Done():
			err = errors.New("it ended before answering")
			if e := proc.Err(); e != nil {
				err = fmt.Errorf("it ended before answering (%v)", e)
			}
		case <-wait.C:
			if errors.Is(err, context.DeadlineExceeded) {
				err = errors.New("no answer in time")
			}
		}
		if tail := lastLine(proc.StderrTail()); tail != "" && !strings.Contains(err.Error(), tail) {
			return fmt.Errorf("%s: %v; it printed: %s", step, err, tail)
		}
		return fmt.Errorf("%s: %w", step, err)
	}
	var g Greeting
	init, err := p.initializeSession(ctx, s)
	if err != nil {
		return g, why("initialize", err)
	}
	g.Title, g.Version = cmp.Or(init.AgentInfo.Title, init.AgentInfo.Name), init.AgentInfo.Version
	g.ProtocolVersion = init.ProtocolVersion
	g.Capabilities = init.toCapabilities()
	for _, m := range init.AuthMethods {
		g.Auth = append(g.Auth, m.ID)
	}
	if dir == "" {
		return g, nil
	}
	if m := authMethod(p.Auth, init.AuthMethods); m != "" {
		if _, err := s.call(ctx, "authenticate", map[string]any{"methodId": m}); err != nil {
			return g, why("authenticate", err)
		}
	}
	res, err := s.call(ctx, "session/new", map[string]any{"cwd": dir, "mcpServers": []any{}})
	if err != nil {
		return g, why("session/new", err)
	}
	var r struct {
		SessionID string `json:"sessionId"`
	}
	json.Unmarshal(res, &r)
	if r.SessionID == "" {
		return g, errors.New("session/new: it returned no session id")
	}
	var cfg sessionConfig
	_ = json.Unmarshal(res, &cfg)
	if offers := cfg.offers(); offers != nil {
		g.Models = offers.Models
		g.Capabilities.SelectEffort, g.Capabilities.SelectMode = cfg.option("thought_level") != nil, len(offers.Modes) > 0
	}
	g.Session = true
	return g, nil
}

// discoverByHandshake is Discover for an agent that may have no --version:
// available once it answers initialize, its version the one it gives.
func (p *Provider) discoverByHandshake(ctx context.Context, info provider.Info) provider.Info {
	g, err := p.Handshake(ctx, provider.ProbeEnv(p.PathEnv), "")
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		info.Problem = provider.ProblemTimedOut
	case err != nil:
		info.Problem, info.Detail = provider.ProblemFailed, err.Error()
	default:
		// As for every ACP agent, its sign-in shows only in a session: not
		// confirmed until one does (工单 #112).
		caps := g.Capabilities
		info.Available, info.LoginUnconfirmed, info.Version = true, true, g.Version
		info.Capabilities = &caps
		info.ProtocolVersion = g.ProtocolVersion
	}
	return info
}

// lastLine is the last line of s that is not blank, cut to what a screen
// shows.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	l := strings.TrimSpace(lines[len(lines)-1])
	if r := []rune(l); len(r) > 200 {
		l = string(r[:200]) + "…"
	}
	return l
}
