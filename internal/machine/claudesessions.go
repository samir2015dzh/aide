package machine

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/convreport"
	"github.com/samir2015dzh/aide/internal/paths"
	"github.com/samir2015dzh/aide/internal/provider/claude"
	"github.com/samir2015dzh/aide/internal/shellenv"
	"github.com/samir2015dzh/aide/pkg/identity"
)

// The Claude Code sessions the Claude desktop app lists here, beside the
// ones Aide started (docs/claude-sessions.md). Owner only, read only.

func (s *adminService) ListClaudeSessions(ctx context.Context, _ *machinev1.ListClaudeSessionsRequest) (*machinev1.ListClaudeSessionsResponse, error) {
	if principalFrom(ctx).Kind != PrincipalOwner {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("owner only"))
	}
	return s.n.ClaudeSessions()
}

func (s *adminService) ReadClaudeSession(ctx context.Context, req *machinev1.ReadClaudeSessionRequest) (*machinev1.ReadClaudeSessionResponse, error) {
	if principalFrom(ctx).Kind != PrincipalOwner {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("owner only"))
	}
	return s.n.ReadClaudeSession(req)
}

// ListClaudeSessions for an agent: those it may see as it sees DSH's
// (dshVisible), for its roster; the index folders are the owner's.
func (s *machineService) ListClaudeSessions(ctx context.Context, _ *machinev1.ListClaudeSessionsRequest) (*machinev1.ListClaudeSessionsResponse, error) {
	visible, err := s.n.dshVisible(principalFrom(ctx))
	if err != nil || visible == nil {
		return &machinev1.ListClaudeSessionsResponse{}, err
	}
	all, err := s.n.ClaudeSessions()
	if err != nil {
		return nil, err
	}
	out := &machinev1.ListClaudeSessionsResponse{}
	for _, cs := range all.Sessions {
		if visible(cs.Cwd) {
			out.Sessions = append(out.Sessions, cs)
		}
	}
	return out, nil
}

// claudeSkip leaves out the sessions Aide started, which it lists in the
// app too: by Claude Code's id (provider_ref), and by the folder (one of
// Aide's worktrees).
func (n *Machine) claudeSkip() (func(cliID, cwd string) bool, error) {
	refs, err := n.st.ProviderRefs()
	if err != nil {
		return nil, err
	}
	roots := withReal(n.sessions.ws.root)
	return func(cliID, cwd string) bool { return refs[cliID] || within(roots, cwd) }, nil
}

// ClaudeSessions lists the app's Claude Code sessions here, and the index
// folders read.
func (n *Machine) ClaudeSessions() (*machinev1.ListClaudeSessionsResponse, error) {
	skip, err := n.claudeSkip()
	if err != nil {
		return nil, err
	}
	p := claude.PlacesFor(shellenv.Environ())
	ss := claude.List(p, skip)
	claudeAddresses(ss...)
	return &machinev1.ListClaudeSessionsResponse{Sessions: ss, Indexes: p.Indexes}, nil
}

// claudeAddresses gives each session whose conversation this computer's CLI
// agent's hooks reported its address (convreport); the others none.
func claudeAddresses(ss ...*machinev1.ClaudeSession) {
	r := convreport.Load(paths.CLIHome())
	for _, s := range ss {
		if s == nil {
			continue
		}
		s.Address = ""
		if key, err := identity.NormalizeName(s.CliSessionId); err == nil {
			s.Address = r.Address(key)
		}
	}
}

// ReadClaudeSession reads one and pages through its conversation as
// ListEvents does.
func (n *Machine) ReadClaudeSession(req *machinev1.ReadClaudeSessionRequest) (*machinev1.ReadClaudeSessionResponse, error) {
	if req.AfterSeq > 0 && req.BeforeSeq > 0 {
		return nil, cerr(connect.CodeInvalidArgument, "give after_seq or before_seq, not both")
	}
	limit := int(req.Limit)
	switch {
	case limit == 0:
		limit = 50
	case limit > 1000:
		return nil, cerr(connect.CodeInvalidArgument, "at most 1000 events per page")
	}
	skip, err := n.claudeSkip()
	if err != nil {
		return nil, err
	}
	s, evs, err := claude.Read(claude.PlacesFor(shellenv.Environ()), req.SessionId, skip)
	if errors.Is(err, claude.ErrNoSession) {
		return nil, cerr(connect.CodeNotFound, "the Claude desktop app has no session %q here", req.SessionId)
	}
	if err != nil {
		return nil, err
	}
	claudeAddresses(s)
	res := &machinev1.ReadClaudeSessionResponse{Session: s}
	res.Events, res.HasMoreBefore, res.HasMoreAfter = eventPage(evs, req.AfterSeq, req.BeforeSeq, limit)
	if req.View == sessionv1.EventView_EVENT_VIEW_BASIC {
		for i, e := range res.Events {
			res.Events[i] = BasicEvent(e)
		}
	}
	return res, nil
}
