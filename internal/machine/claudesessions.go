package machine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"connectrpc.com/connect"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/convreport"
	"github.com/samir2015dzh/aide/internal/paths"
	"github.com/samir2015dzh/aide/internal/provider/claude"
	"github.com/samir2015dzh/aide/internal/reason"
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
// Aide's worktrees). One the owner went on with in Aide stays (工单 #181):
// its earlier conversation is only here.
func (n *Machine) claudeSkip() (func(cliID, cwd string) bool, error) {
	refs, err := n.st.ProviderRefs()
	if err != nil {
		return nil, err
	}
	roots := withReal(n.sessions.ws.root)
	return func(cliID, cwd string) bool {
		return refs[cliID] && n.claudeContinuedIn(cliID) == "" || within(roots, cwd)
	}, nil
}

// claudeContinuedKey keeps the session of Aide's a Claude Code session
// goes on in (工单 #181), by Claude Code's id.
func claudeContinuedKey(cliID string) string { return "claude-continued/" + cliID }

func (n *Machine) claudeContinuedIn(cliID string) string {
	b, _ := n.st.Get(claudeContinuedKey(cliID))
	return string(b)
}

// claudeContinued says on each listed session the session of Aide's it
// goes on in, if any.
func (n *Machine) claudeContinued(ss ...*machinev1.ClaudeSession) {
	for _, s := range ss {
		if s != nil && s.CliSessionId != "" {
			s.ContinuedIn = n.claudeContinuedIn(s.CliSessionId)
		}
	}
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
	n.claudeContinued(ss...)
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
	n.claudeContinued(s)
	res := &machinev1.ReadClaudeSessionResponse{Session: s}
	res.Events, res.HasMoreBefore, res.HasMoreAfter = eventPage(evs, req.AfterSeq, req.BeforeSeq, limit)
	if req.View == sessionv1.EventView_EVENT_VIEW_BASIC {
		for i, e := range res.Events {
			res.Events[i] = BasicEvent(e)
		}
	}
	return res, nil
}

func (s *adminService) ContinueClaudeSession(ctx context.Context, req *machinev1.ContinueClaudeSessionRequest) (*machinev1.ContinueClaudeSessionResponse, error) {
	p := principalFrom(ctx)
	if p.Kind != PrincipalOwner {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("owner only"))
	}
	ss, err := s.n.ContinueClaudeSession(ctx, p, req)
	if err != nil {
		return nil, err
	}
	return &machinev1.ContinueClaudeSessionResponse{Session: ss}, nil
}

// ContinueClaudeSession goes on with one of the app's Claude Code sessions
// in Aide (工单 #181): a session of Aide's that resumes it — `claude
// --resume` with its own id, in its own folder, so Claude Code writes what
// follows in the same transcript the app shows — the owner's message its
// first turn. One already gone on with is that session (the message is not
// sent twice: send it there). A folder no project holds is said so, with
// the folder, for the owner to add it or name a project.
func (n *Machine) ContinueClaudeSession(ctx context.Context, p *Principal, req *machinev1.ContinueClaudeSessionRequest) (*sessionv1.Session, error) {
	if strings.TrimSpace(req.SessionId) == "" {
		return nil, cerr(connect.CodeInvalidArgument, "name the Claude session to go on with")
	}
	cs, _, err := claude.Read(claude.PlacesFor(shellenv.Environ()), req.SessionId, nil)
	if errors.Is(err, claude.ErrNoSession) {
		return nil, cerr(connect.CodeNotFound, "the Claude desktop app has no session %q here", req.SessionId)
	}
	if err != nil {
		return nil, err
	}
	if cs.CliSessionId == "" || cs.Error != "" {
		return nil, reason.New(connect.CodeFailedPrecondition, "claude_no_transcript",
			fmt.Sprintf("Claude Code's conversation of %q is not on this machine (%s), so it can't go on here: go on with it in Claude, or start a new task in Aide", cs.Title, strings.TrimSpace(cs.Error)))
	}
	if id := n.claudeContinuedIn(cs.CliSessionId); id != "" {
		return n.sessions.Get(p, id)
	}
	if id, err := n.st.SessionByProviderRef("claude", cs.CliSessionId); err != nil {
		return nil, err
	} else if id != "" { // Aide's own already (it started it)
		return n.sessions.Get(p, id)
	}
	if fi, err := os.Stat(cs.Cwd); err != nil || !fi.IsDir() {
		return nil, reason.New(connect.CodeFailedPrecondition, "claude_folder_gone",
			fmt.Sprintf("the folder %s it worked in is no longer here, so it can't go on in it: go on with it in Claude, or start a new task in Aide", cs.Cwd), "folder", cs.Cwd)
	}
	project := req.ProjectId
	if project == "" {
		if project, err = n.projectOfFolder(ctx, cs.Cwd); err != nil {
			return nil, err
		}
	}
	if project == "" {
		return nil, reason.New(connect.CodeFailedPrecondition, "claude_no_project",
			fmt.Sprintf("no project here holds %s, the folder it worked in: add that folder as a project (or name one) to go on with it in Aide", cs.Cwd), "folder", cs.Cwd)
	}
	ss, err := n.sessions.start(ctx, p, &sessionv1.StartSessionRequest{IdempotencyKey: req.IdempotencyKey, ProjectId: project, Agent: "claude", Prompt: req.Prompt, Title: cs.Title},
		&resumed{ref: cs.CliSessionId, dir: cs.Cwd})
	if err != nil {
		return nil, err
	}
	if err := n.st.Set(claudeContinuedKey(cs.CliSessionId), []byte(ss.SessionId)); err != nil {
		n.log.Warn("keeping which session goes on with a Claude session", "session", ss.SessionId, "err", err)
	}
	return ss, nil
}

// projectOfFolder is the project Claude Code may work in that holds dir:
// its folder or one above it, the nearest; else one whose repository dir is
// a worktree of (git's common dir is theirs); "" when none.
func (n *Machine) projectOfFolder(ctx context.Context, dir string) (string, error) {
	ps, err := n.st.Projects()
	if err != nil {
		return "", err
	}
	real := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return filepath.Clean(p)
	}
	d := real(dir)
	best, depth := "", -1
	for _, p := range ps {
		if !p.AllowsAgent("claude") {
			continue
		}
		root := real(p.Path)
		if rel, err := filepath.Rel(root, d); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			if n := len(root); n > depth {
				best, depth = p.ID, n
			}
		}
	}
	if best != "" {
		return best, nil
	}
	common := gitCommonDir(ctx, d)
	if common == "" {
		return "", nil
	}
	for _, p := range ps {
		if p.AllowsAgent("claude") && gitCommonDir(ctx, real(p.Path)) == common {
			return p.ID, nil
		}
	}
	return "", nil
}

// gitCommonDir is the repository dir's checkout shares with its worktrees;
// "" when dir is no git checkout.
func gitCommonDir(ctx context.Context, dir string) string {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return ""
	}
	c := strings.TrimSpace(string(out))
	if r, err := filepath.EvalSymlinks(c); err == nil {
		return r
	}
	return filepath.Clean(c)
}
