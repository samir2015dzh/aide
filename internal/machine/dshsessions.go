package machine

import (
	"cmp"
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"

	"connectrpc.com/connect"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/provider/acp/dshnative"
	"github.com/samir2015dzh/aide/internal/shellenv"
	"github.com/samir2015dzh/aide/pkg/identity"
)

// DeepSeek Harness's own sessions (dshnative): those the owner opened in
// DSH Desktop, or another tool started, beside the ones Aide started.
// The owner reads them, and sends messages to those they take over
// (dshtakeover.go).

func (s *adminService) ListDSHSessions(ctx context.Context, _ *machinev1.ListDSHSessionsRequest) (*machinev1.ListDSHSessionsResponse, error) {
	if principalFrom(ctx).Kind != PrincipalOwner {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("owner only"))
	}
	return s.n.DSHSessions(ctx)
}

func (s *adminService) ReadDSHSession(ctx context.Context, req *machinev1.ReadDSHSessionRequest) (*machinev1.ReadDSHSessionResponse, error) {
	if principalFrom(ctx).Kind != PrincipalOwner {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("owner only"))
	}
	return s.n.ReadDSHSession(req)
}

// The same for an agent, within its grant (dshVisible).

func (s *machineService) ListDSHSessions(ctx context.Context, _ *machinev1.ListDSHSessionsRequest) (*machinev1.ListDSHSessionsResponse, error) {
	visible, err := s.n.dshVisible(principalFrom(ctx))
	if err != nil || visible == nil {
		return &machinev1.ListDSHSessionsResponse{}, err
	}
	return s.n.dshSessions(visible)
}

func (s *machineService) ReadDSHSession(ctx context.Context, req *machinev1.ReadDSHSessionRequest) (*machinev1.ReadDSHSessionResponse, error) {
	visible, err := s.n.dshVisible(principalFrom(ctx))
	if err != nil {
		return nil, err
	}
	if visible == nil {
		return nil, cerr(connect.CodeNotFound, "DSH has no session %q here", req.SessionId)
	}
	return s.n.readDSHSession(req, visible)
}

// dshVisible is whether p may read a DSH session that works in cwd; nil
// when it may read none. The owner reads every one. No DSH session is an
// agent's own, so an agent reads them only with a grant that lets it see
// sessions it did not start (TASK_VISIBILITY_ALL), and as it sees those:
// in the projects its grant covers (a session whose folder is one of
// theirs, or in one), anywhere with all of them. The grant covers this
// machine, or the agent could not call it.
func (n *Machine) dshVisible(p *Principal) (func(cwd string) bool, error) {
	if p.Kind == PrincipalOwner {
		return func(string) bool { return true }, nil
	}
	g := p.Grant
	if p.Kind != PrincipalClient || g.GetTaskVisibility() != identityv1.TaskVisibility_TASK_VISIBILITY_ALL {
		return nil, nil
	}
	if slices.Contains(g.ProjectIds, "*") {
		return func(string) bool { return true }, nil
	}
	projects, err := n.st.Projects()
	if err != nil {
		return nil, err
	}
	var roots []string
	for _, pr := range projects {
		if identity.Allows(g.ProjectIds, pr.ID) {
			roots = append(roots, withReal(pr.Path)...)
		}
	}
	return func(cwd string) bool { return within(roots, cwd) }, nil
}

// DSHSessions lists the DSH sessions in the DSH home this machine's agents
// use, but those Aide started: by the DSH session id each recorded
// (provider_ref), and by the folder, a session's worktree (where an
// earlier DSH session of a session Aide started again, or deleted,
// worked).
// The owner also learns the state of the Aide bridge plugin.
func (n *Machine) DSHSessions(ctx context.Context) (*machinev1.ListDSHSessionsResponse, error) {
	res, err := n.dshSessions(func(string) bool { return true })
	if err != nil {
		return nil, err
	}
	if home, err := dshnative.Home(shellenv.Environ()); err == nil {
		res.Bridge = dshBridge(ctx, home)
	}
	return res, nil
}

// dshSessions is DSHSessions, of the sessions that work in a folder
// visible takes.
func (n *Machine) dshSessions(visible func(cwd string) bool) (*machinev1.ListDSHSessionsResponse, error) {
	home, err := dshnative.Home(shellenv.Environ())
	if err != nil {
		return nil, err
	}
	refs, err := n.st.ProviderRefs()
	if err != nil {
		return nil, err
	}
	roots := withReal(n.sessions.ws.root)
	list, err := dshnative.List(home, func(id, cwd string) bool {
		return refs[id] || within(roots, cwd) || !visible(cwd)
	})
	for _, s := range list {
		s.Takeover = n.dshTakenOver(s.SessionId)
	}
	return &machinev1.ListDSHSessionsResponse{Sessions: list}, err
}

// withReal is dir, and where it leads when a symbolic link is on the way.
func withReal(dir string) []string {
	out := []string{dir}
	if real, err := filepath.EvalSymlinks(dir); err == nil && real != dir {
		out = append(out, real)
	}
	return out
}

// within: cwd is one of roots, or in one.
func within(roots []string, cwd string) bool {
	return slices.ContainsFunc(roots, func(root string) bool {
		rel, err := filepath.Rel(root, cwd)
		return cwd != "" && err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	})
}

// ReadDSHSession reads a DSH session and pages through its events as
// ListEvents does.
func (n *Machine) ReadDSHSession(req *machinev1.ReadDSHSessionRequest) (*machinev1.ReadDSHSessionResponse, error) {
	return n.readDSHSession(req, nil)
}

// readDSHSession is ReadDSHSession; with visible, only of a session
// dshSessions(visible) would list (as far as the session tells: not
// whether it is a subagent's).
func (n *Machine) readDSHSession(req *machinev1.ReadDSHSessionRequest, visible func(cwd string) bool) (*machinev1.ReadDSHSessionResponse, error) {
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
	home, err := dshnative.Home(shellenv.Environ())
	if err != nil {
		return nil, err
	}
	s, evs, err := dshnative.Read(home, req.SessionId)
	if err == nil && visible != nil {
		refs, rerr := n.st.ProviderRefs()
		if rerr != nil {
			return nil, rerr
		}
		if refs[s.SessionId] || within(withReal(n.sessions.ws.root), s.Cwd) || !visible(s.Cwd) {
			err = dshnative.ErrNotFound
		}
	}
	if errors.Is(err, dshnative.ErrNotFound) {
		return nil, cerr(connect.CodeNotFound, "DSH has no session %q here", req.SessionId)
	}
	if err != nil {
		return nil, err
	}
	s.Takeover = n.dshTakenOver(s.SessionId)
	res := &machinev1.ReadDSHSessionResponse{Session: s}
	res.Events, res.HasMoreBefore, res.HasMoreAfter = eventPage(evs, req.AfterSeq, req.BeforeSeq, limit)
	if req.View == sessionv1.EventView_EVENT_VIEW_BASIC {
		for i, e := range res.Events {
			res.Events[i] = BasicEvent(e)
		}
	}
	return res, nil
}

// eventPage is Store.EventPage of events held in order of seq.
func eventPage(evs []*sessionv1.SessionEvent, afterSeq, beforeSeq uint64, limit int) (page []*sessionv1.SessionEvent, moreBefore, moreAfter bool) {
	from := func(seq uint64) int { // the first past seq
		i, _ := slices.BinarySearchFunc(evs, seq+1, func(e *sessionv1.SessionEvent, s uint64) int { return cmp.Compare(e.Seq, s) })
		return i
	}
	if afterSeq > 0 {
		i := from(afterSeq)
		j := min(i+limit, len(evs))
		return evs[i:j], i > 0, j < len(evs)
	}
	j := len(evs)
	if beforeSeq > 0 {
		j = from(beforeSeq - 1)
	}
	i := max(j-limit, 0)
	return evs[i:j], i > 0, j < len(evs)
}
