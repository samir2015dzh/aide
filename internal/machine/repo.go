package machine

import (
	"context"
	"fmt"
	neturl "net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"connectrpc.com/connect"

	identityv1 "github.com/hermeshai/hermesh/gen/go/hermesh/identity/v1"
	sessionv1 "github.com/hermeshai/hermesh/gen/go/hermesh/session/v1"
	"github.com/hermeshai/hermesh/internal/provider"
)

// A session can start in a GitHub repository instead of a project
// (StartSessionRequest.repo): the machine clones it into its data folder
// with its own git credentials (Hermesh keeps none), keeps the clone as a
// worktree project, and fetches it again for each session started there.

// githubRepo is a GitHub repository a session names.
type githubRepo struct {
	Owner, Name string
	// URL is what the clone fetches: an ssh URL as given, else githubURL.
	URL string
}

func (r githubRepo) String() string { return r.Owner + "/" + r.Name }

var (
	ghOwnerRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)
	ghNameRE  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
)

// githubURL is where git clones owner/name from: https, which the
// machine's git reaches with its own login (gh auth setup-git, Git
// Credential Manager, the keychain).
func githubURL(owner, name string) string {
	return "https://github.com/" + owner + "/" + name + ".git"
}

// parseGitHubRepo reads a repository as people write it: owner/repo,
// github.com/owner/repo, or its https or ssh URL. An ssh URL is kept, for
// a machine whose git reaches GitHub over ssh. A URL with credentials in
// it is refused: the machine's own login is what reaches the repository.
func parseGitHubRepo(s string) (githubRepo, error) {
	in := strings.TrimSpace(s)
	path, url := in, ""
	switch {
	case strings.HasPrefix(in, "git@github.com:"):
		path, url = strings.TrimPrefix(in, "git@github.com:"), in
	case strings.HasPrefix(in, "ssh://git@github.com/"):
		path, url = strings.TrimPrefix(in, "ssh://git@github.com/"), in
	case strings.Contains(in, "://"):
		u, err := neturl.Parse(in)
		switch {
		case err != nil:
		case u.User != nil:
			return githubRepo{}, fmt.Errorf("give the repository without credentials: the machine uses its own git login")
		case (u.Scheme == "https" || u.Scheme == "http") && strings.EqualFold(u.Host, "github.com") && u.RawQuery == "" && u.Fragment == "":
			path = strings.TrimPrefix(u.Path, "/")
		default:
			return githubRepo{}, fmt.Errorf("%q is not a GitHub repository: give owner/repo or its URL", in)
		}
	default:
		path = strings.TrimPrefix(in, "github.com/")
	}
	path = strings.TrimSuffix(strings.TrimSuffix(path, "/"), ".git")
	owner, name, _ := strings.Cut(path, "/")
	if !ghOwnerRE.MatchString(owner) || !ghNameRE.MatchString(name) || name == "." || name == ".." {
		return githubRepo{}, fmt.Errorf("%q is not a GitHub repository: give owner/repo or its URL", in)
	}
	if url == "" {
		url = githubURL(owner, name)
	}
	return githubRepo{Owner: owner, Name: name, URL: url}, nil
}

// projectID is the project the clone is kept as, "github.<owner>.<repo>"
// in lower case as GitHub compares names (an owner has no dot, so no two
// repositories share one).
func (r githubRepo) projectID() string { return strings.ToLower("github." + r.Owner + "." + r.Name) }

// cloneDir is where the machine keeps its clone.
func (r githubRepo) cloneDir(dataDir string) string {
	return filepath.Join(dataDir, "repos", "github.com", strings.ToLower(r.Owner), strings.ToLower(r.Name))
}

// repoProject is the project a session in r works in: the clone's, made
// for its first session.
func (s *Sessions) repoProject(proj *Project, r githubRepo) (*Project, error) {
	dir := r.cloneDir(s.dataDir)
	if proj == nil {
		return &Project{ID: r.projectID(), Path: dir, DefaultBaseRef: "origin/HEAD", Workspace: identityv1.Workspace_WORKSPACE_WORKTREE,
			Description: "GitHub " + r.String() + ", cloned by Aide"}, nil
	}
	if proj.Path != dir {
		return nil, cerr(connect.CodeFailedPrecondition, "project %q on this machine is not its clone of %s: it is %s", proj.ID, r, proj.Path)
	}
	return proj, nil
}

// remoteBranch asks the repository for branch (its default branch when
// ""), with the machine's own git credentials, and returns its name.
func remoteBranch(ctx context.Context, r githubRepo, branch string) (string, error) {
	unreachable := func(err error) error {
		return cerr(connect.CodeFailedPrecondition, "this machine cannot reach GitHub repository %s with its own git login: %v; sign in on the machine (gh auth login, or git's credentials or ssh key) or check the repository's name", r, err)
	}
	if branch == "" {
		out, err := gitRemote(ctx, "", "ls-remote", "--symref", r.URL, "HEAD")
		if err != nil {
			return "", unreachable(err)
		}
		for _, line := range strings.Split(out, "\n") {
			if ref, ok := strings.CutPrefix(line, "ref: refs/heads/"); ok {
				if b, ok := strings.CutSuffix(ref, "\tHEAD"); ok {
					return b, nil
				}
			}
		}
		return "", cerr(connect.CodeFailedPrecondition, "GitHub repository %s has no default branch (is it empty?)", r)
	}
	if strings.HasPrefix(branch, "-") || !validRefs(ctx, "", "refs/heads/"+branch) {
		return "", cerr(connect.CodeInvalidArgument, "%q is not a branch name", branch)
	}
	out, err := gitRemote(ctx, "", "ls-remote", "--heads", r.URL, "refs/heads/"+branch)
	if err != nil {
		return "", unreachable(err)
	}
	if strings.TrimSpace(out) == "" {
		return "", cerr(connect.CodeNotFound, "GitHub repository %s has no branch %q", r, branch)
	}
	return branch, nil
}

// syncClone makes dir a clone of url, or fetches every branch of url into
// the clone already there (refs/remotes/origin/*). The clone has no
// checkout of its own: sessions work in worktrees of it.
func (w *Workspace) syncClone(ctx context.Context, url, dir string) error {
	unlock := w.lock("clone " + dir)
	defer unlock()
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		if _, err := git(ctx, dir, "remote", "set-url", "origin", url); err != nil {
			return err
		}
		_, err := gitRemoteWithin(ctx, 0, dir, "fetch", "--quiet", "--prune", "--no-recurse-submodules", "origin")
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return err
	}
	// Into a new folder, then in place: a stopped clone leaves no half one.
	tmp, err := os.MkdirTemp(filepath.Dir(dir), ".clone-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if _, err := gitRemoteWithin(ctx, 0, tmp, "clone", "--quiet", "--no-checkout", url, "."); err != nil {
		return err
	}
	return os.Rename(tmp, dir)
}

// fetchRepo brings the clone of the session's GitHub repository up to
// date, or makes it, before its worktree is made; nothing for a project's
// session.
func (s *Sessions) fetchRepo(ctx context.Context, l *live, dir string) error {
	l.mu.Lock()
	repo, url := l.row.Repo, l.row.RepoURL
	l.mu.Unlock()
	if repo == "" {
		return nil
	}
	start := time.Now()
	if err := s.ws.syncClone(ctx, url, dir); err != nil {
		return provider.Fail(sessionv1.FailureKind_FAILURE_KIND_START_FAILED,
			fmt.Sprintf("Cannot clone or fetch GitHub repository %s into %s with this machine's git login: %v.", repo, dir, err))
	}
	s.log.Info("fetched the repository", "session", l.id, "repo", repo, "took", time.Since(start).Round(time.Millisecond))
	return nil
}

// Push pushes the session's branch, as its last finished turn left it, to
// its repository's remote origin under the same name, with the machine's
// own git login: the one way Hermesh pushes, and only when asked. It is
// never forced.
func (s *Sessions) Push(ctx context.Context, p *Principal, id string) (*sessionv1.PushSessionResponse, error) {
	l, r, err := s.find(p, id, p.Kind != PrincipalOwner)
	if err != nil {
		return nil, err
	}
	if l != nil {
		l.mu.Lock()
		c := *l.row
		l.mu.Unlock()
		r = &c
	}
	if r.Workspace == identityv1.Workspace_WORKSPACE_IN_PLACE {
		return nil, cerr(connect.CodeFailedPrecondition, "session %s works in its project's folder: it has no branch of its own to push", id)
	}
	if r.Branch == "" {
		return nil, cerr(connect.CodeFailedPrecondition, "session %s has no branch yet: it gets one when its worktree is made", id)
	}
	proj, err := s.st.Project(r.ProjectID)
	if err != nil {
		return nil, err
	}
	if proj == nil {
		return nil, cerr(connect.CodeFailedPrecondition, "project %q no longer exists on this machine", r.ProjectID)
	}
	// As configured: get-url would expand insteadOf.
	origin, err := git(ctx, proj.Path, "config", "--get", "remote.origin.url")
	if err != nil {
		return nil, cerr(connect.CodeFailedPrecondition, "project %q has no remote origin to push to", r.ProjectID)
	}
	ref := "refs/heads/" + r.Branch
	if _, err := gitRemote(ctx, proj.Path, "push", "--quiet", "origin", ref+":"+ref); err != nil {
		s.audit.Log(AuditRecord{Kind: "session.push", Subject: p.ID(), Session: id, Action: r.Branch, Decision: "error", Reason: err.Error()})
		return nil, cerr(connect.CodeFailedPrecondition, "pushing %s to the remote origin of project %q with this machine's git login: %v", r.Branch, r.ProjectID, err)
	}
	s.audit.Log(AuditRecord{Kind: "session.push", Subject: p.ID(), Session: id, Action: r.Branch, Decision: "allow"})
	res := &sessionv1.PushSessionResponse{Branch: r.Branch}
	if gh, err := parseGitHubRepo(origin); err == nil {
		res.PullRequestUrl = pullRequestURL(gh, r.RepoBranch, r.Branch)
	}
	return res, nil
}

// pullRequestURL is GitHub's page that opens a pull request of branch into
// base ("": the repository's default branch).
func pullRequestURL(r githubRepo, base, branch string) string {
	if base != "" {
		branch = base + "..." + branch
	}
	return "https://github.com/" + r.Owner + "/" + r.Name + "/compare/" + branch + "?expand=1"
}
