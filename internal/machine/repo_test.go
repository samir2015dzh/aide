package machine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect"

	identityv1 "github.com/hermeshai/hermesh/gen/go/hermesh/identity/v1"
	sessionv1 "github.com/hermeshai/hermesh/gen/go/hermesh/session/v1"
)

func TestParseGitHubRepo(t *testing.T) {
	for in, want := range map[string]githubRepo{
		"acme/widget":                               {"acme", "widget", "https://github.com/acme/widget.git"},
		" Acme/Widget.js ":                          {"Acme", "Widget.js", "https://github.com/Acme/Widget.js.git"},
		"github.com/acme/widget":                    {"acme", "widget", "https://github.com/acme/widget.git"},
		"https://github.com/acme/widget":            {"acme", "widget", "https://github.com/acme/widget.git"},
		"https://github.com/acme/widget.git":        {"acme", "widget", "https://github.com/acme/widget.git"},
		"https://GitHub.com/acme/widget/":           {"acme", "widget", "https://github.com/acme/widget.git"},
		"git@github.com:acme/widget.git":            {"acme", "widget", "git@github.com:acme/widget.git"},
		"ssh://git@github.com/acme/my-widget_2.git": {"acme", "my-widget_2", "ssh://git@github.com/acme/my-widget_2.git"},
	} {
		got, err := parseGitHubRepo(in)
		if err != nil || got != want {
			t.Errorf("%q: %+v, %v; want %+v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "acme", "acme/", "/widget", "acme/widget/tree/main", "acme/..", "-acme/widget", "ac me/widget",
		"https://gitlab.com/acme/widget", "file:///tmp/acme/widget", "https://github.com/acme/widget?tab=readme", "git@gitlab.com:acme/widget.git"} {
		if got, err := parseGitHubRepo(in); err == nil {
			t.Errorf("%q: %+v", in, got)
		}
	}
	// Hermesh keeps no credentials, not even ones written into a URL.
	if _, err := parseGitHubRepo("https://x-access-token:ghp_secret@github.com/acme/widget.git"); err == nil || strings.Contains(err.Error(), "ghp_secret") {
		t.Errorf("a URL with credentials: %v", err)
	}
}

func TestGitHubNames(t *testing.T) {
	if got := githubURL("acme", "widget"); got != "https://github.com/acme/widget.git" {
		t.Errorf("githubURL: %s", got)
	}
	r := githubRepo{Owner: "Acme", Name: "Widget.js"}
	if got := r.projectID(); got != "github.acme.widget.js" || !projectIDRE.MatchString(got) {
		t.Errorf("projectID: %s", got)
	}
	if got := r.cloneDir("/data"); got != filepath.Join("/data", "repos", "github.com", "acme", "widget.js") {
		t.Errorf("cloneDir: %s", got)
	}
	if got := pullRequestURL(r, "main", "hermesh/s_1"); got != "https://github.com/Acme/Widget.js/compare/main...hermesh/s_1?expand=1" {
		t.Errorf("pullRequestURL: %s", got)
	}
	if got := pullRequestURL(r, "", "hermesh/s_1"); got != "https://github.com/Acme/Widget.js/compare/hermesh/s_1?expand=1" {
		t.Errorf("pullRequestURL without a base: %s", got)
	}
}

// fakeGitHub serves github.com from local bare repositories, as git's own
// url.<base>.insteadOf does for anyone: owner/name is <dir>/owner/name.git.
func fakeGitHub(t *testing.T) string {
	dir := t.TempDir()
	u := filepath.ToSlash(dir)
	if !strings.HasPrefix(u, "/") { // C:/...
		u = "/" + u
	}
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "url.file://"+u+"/.insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_0", "https://github.com/")
	return dir
}

// githubRepoAt makes owner/name on fakeGitHub: a main branch and a
// feature branch, each with a file of its own; it returns a clone to push
// from.
func githubRepoAt(t *testing.T, gh, owner, name string) string {
	bare := filepath.Join(gh, owner, name+".git")
	gitOut(t, t.TempDir(), "init", "-q", "--bare", "-b", "main", bare)
	work := testRepo(t)
	gitOut(t, work, "branch", "-M", "main")
	gitOut(t, work, "remote", "add", "origin", bare)
	gitOut(t, work, "push", "-q", "origin", "main")
	gitOut(t, work, "checkout", "-q", "-b", "feature")
	commitFile(t, work, "feature.txt", "feature\n")
	gitOut(t, work, "push", "-q", "origin", "feature")
	gitOut(t, work, "checkout", "-q", "main")
	return work
}

func commitFile(t *testing.T, repo, name, text string) {
	if err := os.WriteFile(filepath.Join(repo, name), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOut(t, repo, "add", name)
	gitOut(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", name)
}

// A session in a GitHub repository nobody registered: the machine clones it
// with its own git login, keeps the clone as a project, fetches it again
// for the next session, and pushes a session's branch only when asked.
func TestRepoSession(t *testing.T) {
	gh := fakeGitHub(t)
	h := newHarness(t, Limits{})
	work := githubRepoAt(t, gh, "acme", "widget")
	ctx := context.Background()
	start := func(req *sessionv1.StartSessionRequest) (*sessionv1.Session, error) {
		req.Agent = "fake"
		return h.s.Start(ctx, h.p, req)
	}

	s, err := start(&sessionv1.StartSessionRequest{Repo: "acme/widget", Prompt: "write made.txt by the session\nsay done"})
	if err != nil {
		t.Fatal(err)
	}
	if s.ProjectId != "github.acme.widget" || s.Repo != "acme/widget" || s.RepoBranch != "main" {
		t.Fatalf("started: %+v", s)
	}
	s = h.wait(s.SessionId, sessionv1.SessionStatus_SESSION_STATUS_IDLE)
	clone := filepath.Join(h.s.dataDir, "repos", "github.com", "acme", "widget")
	proj, err := h.n.st.Project("github.acme.widget")
	if err != nil || proj == nil || proj.Path != clone || proj.InPlace() {
		t.Fatalf("the clone's project: %+v, %v", proj, err)
	}
	wt := h.s.ws.Path(s.SessionId)
	if _, err := os.Stat(filepath.Join(wt, "made.txt")); err != nil {
		t.Fatalf("the session's work: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wt, "feature.txt")); err == nil {
		t.Fatal("the worktree is not on main")
	}
	if got := strings.TrimSpace(gitOut(t, clone, "rev-parse", "refs/remotes/origin/main")); got != strings.TrimSpace(gitOut(t, work, "rev-parse", "main")) {
		t.Fatalf("the clone's main: %s", got)
	}
	// Nothing went up by itself.
	bare := filepath.Join(gh, "acme", "widget.git")
	if out := gitOut(t, bare, "branch", "--list", "hermesh/*"); strings.TrimSpace(out) != "" {
		t.Fatalf("pushed unasked: %s", out)
	}

	// GitHub moves on; the next session, on a branch, fetches the clone.
	commitFile(t, work, "later.txt", "later\n")
	gitOut(t, work, "push", "-q", "origin", "main")
	s2, err := start(&sessionv1.StartSessionRequest{Repo: "https://github.com/acme/widget.git", BaseRef: "feature", Prompt: "say on feature"})
	if err != nil {
		t.Fatal(err)
	}
	if s2.ProjectId != s.ProjectId || s2.RepoBranch != "feature" {
		t.Fatalf("second: %+v", s2)
	}
	h.wait(s2.SessionId, sessionv1.SessionStatus_SESSION_STATUS_IDLE)
	if _, err := os.Stat(filepath.Join(h.s.ws.Path(s2.SessionId), "feature.txt")); err != nil {
		t.Fatalf("the worktree is not on feature: %v", err)
	}
	if got := strings.TrimSpace(gitOut(t, clone, "rev-parse", "refs/remotes/origin/main")); got != strings.TrimSpace(gitOut(t, work, "rev-parse", "main")) {
		t.Fatal("the clone was not fetched again")
	}

	// What the machine can't reach fails at once: no session, no project.
	live := func() int {
		h.s.mu.Lock()
		defer h.s.mu.Unlock()
		return len(h.s.live)
	}
	before := live()
	for _, c := range []struct {
		req  *sessionv1.StartSessionRequest
		code connect.Code
		say  string
	}{
		{&sessionv1.StartSessionRequest{Repo: "acme/missing", Prompt: "x"}, connect.CodeFailedPrecondition, "cannot reach GitHub repository acme/missing"},
		{&sessionv1.StartSessionRequest{Repo: "acme/widget", BaseRef: "nope", Prompt: "x"}, connect.CodeNotFound, `no branch "nope"`},
		{&sessionv1.StartSessionRequest{Repo: "acme/widget", BaseRef: "-x", Prompt: "x"}, connect.CodeInvalidArgument, "not a branch name"},
		{&sessionv1.StartSessionRequest{Repo: "acme/widget", ProjectId: "demo", Prompt: "x"}, connect.CodeInvalidArgument, "not both"},
		{&sessionv1.StartSessionRequest{Repo: "https://gitlab.com/acme/widget", Prompt: "x"}, connect.CodeInvalidArgument, "not a GitHub repository"},
	} {
		_, err := start(c.req)
		if connect.CodeOf(err) != c.code || !strings.Contains(err.Error(), c.say) {
			t.Errorf("%+v: %v", c.req, err)
		}
	}
	if p, _ := h.n.st.Project("github.acme.missing"); p != nil || live() != before {
		t.Fatalf("a failed start left a project or a session: %+v", p)
	}
	// A grant for some projects only does not reach GitHub.
	scoped := &Principal{Kind: PrincipalClient, ClientID: "c_scoped", Grant: &identityv1.Grant{GrantId: "g_scoped",
		MachineIds: []string{"*"}, ProjectIds: []string{"demo"}, Agents: []string{"*"}}}
	if _, err := h.s.Start(ctx, scoped, &sessionv1.StartSessionRequest{Repo: "acme/widget", Agent: "fake", Prompt: "x"}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("a scoped grant: %v", err)
	}

	// Pushed when asked: by its client or the owner, nobody else.
	if _, err := h.s.Push(ctx, scoped, s.SessionId); err == nil {
		t.Fatal("another client pushed")
	}
	res, err := h.s.Push(ctx, h.p, s.SessionId)
	if err != nil {
		t.Fatal(err)
	}
	branch := "hermesh/" + s.SessionId
	if res.Branch != branch || res.PullRequestUrl != "https://github.com/acme/widget/compare/main..."+branch+"?expand=1" {
		t.Fatalf("pushed: %+v", res)
	}
	if got := gitOut(t, bare, "show", branch+":made.txt"); got != "by the session\n" {
		t.Fatalf("the pushed branch: %q", got)
	}
	if _, err := h.s.Push(ctx, &Principal{Kind: PrincipalOwner}, s2.SessionId); err != nil {
		t.Fatalf("the owner's push: %v", err)
	}
}

// A clone that fails leaves nothing half made, for the next start to trip on.
func TestSyncCloneFails(t *testing.T) {
	fakeGitHub(t)
	dir := filepath.Join(t.TempDir(), "repos", "acme", "gone")
	if err := NewWorkspace(t.TempDir()).syncClone(context.Background(), githubURL("acme", "gone"), dir); err == nil {
		t.Fatal("cloned a repository that is not there")
	}
	if entries, _ := os.ReadDir(filepath.Dir(dir)); len(entries) != 0 {
		t.Fatalf("left %v", entries)
	}
}

// A project the owner made can push its sessions too, to its own origin.
func TestPushProjectSession(t *testing.T) {
	h := newHarness(t, Limits{})
	id := h.start("write x.txt x\nsay done")
	h.wait(id, sessionv1.SessionStatus_SESSION_STATUS_IDLE)
	if _, err := h.s.Push(context.Background(), h.p, id); connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "no remote origin") {
		t.Fatalf("without a remote: %v", err)
	}
	proj, _ := h.n.st.Project("demo")
	bare := filepath.Join(t.TempDir(), "origin.git")
	gitOut(t, t.TempDir(), "init", "-q", "--bare", bare)
	gitOut(t, proj.Path, "remote", "add", "origin", bare)
	res, err := h.s.Push(context.Background(), h.p, id)
	if err != nil || res.PullRequestUrl != "" {
		t.Fatalf("pushed: %+v, %v", res, err)
	}
	if got := gitOut(t, bare, "show", "hermesh/"+id+":x.txt"); got != "x\n" {
		t.Fatalf("the pushed branch: %q", got)
	}
}
