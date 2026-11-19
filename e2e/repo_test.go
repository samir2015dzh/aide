package e2e

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	localv1 "github.com/hermeshai/hermesh/gen/go/hermesh/local/v1"
	sessionv1 "github.com/hermeshai/hermesh/gen/go/hermesh/session/v1"
)

// A coding agent starts a session in a GitHub repository the owner never
// cloned or added: the machine clones it with its own git login (here git
// itself serves github.com from local repositories, as url.insteadOf does
// for anyone), the session runs as in any project, the owner's app shows
// where it came from, and nothing is pushed until the agent or the owner
// asks. A repository the machine can't reach fails with why, and starts
// nothing.
func TestRepoSessions(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	gh := t.TempDir()
	w := newOwnerMac(t, append([]string{"HERMESH_FAKE_PROVIDERS=1"}, githubEnv(gh)...), &localv1.GrantSpec{})
	ctx := context.Background()

	// On GitHub: acme/widget, with main and a feature branch.
	bare := filepath.Join(gh, "acme", "widget.git")
	gitOut(t, w.repo, "init", "-q", "--bare", "-b", "main", bare)
	work := filepath.Join(t.TempDir(), "work")
	gitOut(t, w.repo, "clone", "-q", bare, work)
	commit := func(name, text string) {
		if err := os.WriteFile(filepath.Join(work, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		gitOut(t, work, "add", name)
		gitOut(t, work, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", name)
	}
	gitOut(t, work, "checkout", "-q", "-b", "main")
	commit("README.md", "# widget\n")
	gitOut(t, work, "push", "-q", "origin", "main")
	gitOut(t, work, "checkout", "-q", "-b", "feature")
	commit("feature.txt", "feature\n")
	gitOut(t, work, "push", "-q", "origin", "feature")

	type repoSession struct {
		sessionJSON
		ProjectID  string `json:"project_id"`
		Repo       string `json:"repo"`
		RepoBranch string `json:"repo_branch"`
		Branch     string `json:"branch"`
	}
	start := func(args ...string) (repoSession, result) {
		r := w.cli(time.Minute, append([]string{"session", "start", "-o", "json", "--wait"}, args...)...)
		var s repoSession
		unmarshalSession(r.stdout, &s)
		return s, r
	}

	s, r := start("--repo", "acme/widget", "--prompt", "write made.txt by the session\nsay done in the repo")
	if r.code != 0 || s.Status != "idle" || s.Result != "done in the repo" {
		t.Fatalf("start in acme/widget: %+v\n%s", s, r.stdout+r.stderr)
	}
	if s.Repo != "acme/widget" || s.RepoBranch != "main" || s.ProjectID != "github.acme.widget" || s.Branch != "hermesh/"+s.SessionID {
		t.Fatalf("where it works: %+v", s)
	}
	if !strings.Contains(r.stderr, "(GitHub acme/widget, branch main)") {
		t.Fatalf("stderr: %s", r.stderr)
	}
	// Git on Windows checks text out with CRLF (core.autocrlf).
	if b, err := os.ReadFile(filepath.Join(w.worktree(s.SessionID), "README.md")); err != nil || strings.ReplaceAll(string(b), "\r\n", "\n") != "# widget\n" {
		t.Fatalf("the worktree is not GitHub's main: %q, %v", b, err)
	}
	if strings.TrimSpace(gitOut(t, bare, "branch", "--list", "hermesh/*")) != "" {
		t.Fatal("pushed unasked")
	}
	// The owner's app sees the session, and where it came from.
	res, err := w.app.client.ListSessions(ctx, &localv1.ListSessionsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var seen *sessionv1.Session
	for _, x := range res.Sessions {
		if x.SessionId == s.SessionID {
			seen = x
		}
	}
	if seen == nil || seen.Repo != "acme/widget" || seen.RepoBranch != "main" || seen.ProjectId != "github.acme.widget" {
		t.Fatalf("the owner's view: %+v", seen)
	}
	// The clone is kept as a project, which later sessions find.
	projects, err := w.app.client.ListProjects(ctx, &localv1.ListProjectsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var clone string
	for _, p := range projects.Projects {
		if p.ProjectId == "github.acme.widget" {
			clone = p.Path
		}
	}
	if clone == "" || !strings.Contains(filepath.ToSlash(clone), "/repos/github.com/acme/widget") {
		t.Fatalf("projects: %+v", projects.Projects)
	}

	// GitHub moves on; a session on the feature branch fetches it again.
	gitOut(t, work, "checkout", "-q", "main")
	commit("later.txt", "later\n")
	gitOut(t, work, "push", "-q", "origin", "main")
	s2, r := start("--repo", "https://github.com/acme/widget", "--branch", "feature", "--prompt", "say on feature")
	if r.code != 0 || s2.Status != "idle" || s2.RepoBranch != "feature" || s2.ProjectID != s.ProjectID {
		t.Fatalf("start on feature: %+v\n%s", s2, r.stdout+r.stderr)
	}
	if _, err := os.Stat(filepath.Join(w.worktree(s2.SessionID), "feature.txt")); err != nil {
		t.Fatalf("the worktree is not on feature: %v", err)
	}
	if gitOut(t, clone, "rev-parse", "refs/remotes/origin/main") != gitOut(t, work, "rev-parse", "main") {
		t.Fatal("the clone was not fetched")
	}

	// Pushed when asked: by the agent that started it, with the page to
	// open its pull request...
	r = w.cli(time.Minute, "session", "push", s.SessionID, "-o", "json")
	var pushed struct {
		Push struct {
			Branch         string `json:"branch"`
			PullRequestURL string `json:"pull_request_url"`
		} `json:"push"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &pushed); err != nil || r.code != 0 || pushed.Push.Branch != s.Branch ||
		pushed.Push.PullRequestURL != "https://github.com/acme/widget/compare/main..."+s.Branch+"?expand=1" {
		t.Fatalf("session push: %+v, %v\n%s", pushed, err, r.stdout+r.stderr)
	}
	if got := gitOut(t, bare, "show", s.Branch+":made.txt"); got != "by the session" {
		t.Fatalf("the pushed branch: %q", got)
	}
	// ...and by the owner, from the app.
	if p, err := w.app.client.PushSession(ctx, &localv1.PushSessionRequest{SessionId: s2.SessionID}); err != nil || p.Branch != s2.Branch {
		t.Fatalf("the owner's push: %+v, %v", p, err)
	}
	if exec.Command("git", "-C", bare, "rev-parse", "--verify", "--quiet", "refs/heads/"+s2.Branch).Run() != nil {
		t.Fatal("the owner's push did not reach GitHub")
	}

	// A repository the machine can't reach, or a branch it lacks: no session.
	count := func() int {
		var d struct{ Sessions []json.RawMessage }
		json.Unmarshal([]byte(w.cli(30*time.Second, "session", "list", "-o", "json").stdout), &d)
		return len(d.Sessions)
	}
	before := count()
	for _, c := range []struct {
		args []string
		say  string
	}{
		{[]string{"--repo", "acme/private"}, "this machine cannot reach GitHub repository acme/private"},
		{[]string{"--repo", "acme/widget", "--branch", "gone"}, "GitHub repository acme/widget has no branch"},
		{[]string{"--repo", "acme/widget", "--project", "demo"}, "not both"},
	} {
		_, r := start(append(c.args, "--prompt", "say never")...)
		if r.code != 2 || !strings.Contains(r.stdout, c.say) {
			t.Errorf("%v: exit %d\n%s", c.args, r.code, r.stdout+r.stderr)
		}
	}
	if after := count(); before != 2 || after != before {
		t.Fatalf("a failed start made a session: %d, then %d", before, after)
	}
	// A project the owner added works as before.
	if ps, r := w.startSession(time.Minute, "--wait", "--prompt", "say in the project"); r.code != 0 || ps.Result != "in the project" {
		t.Fatalf("project session: %+v\n%s", ps, r.stdout+r.stderr)
	}
}

// githubEnv makes git serve github.com from dir, as url.insteadOf does for
// anyone: owner/name is dir/owner/name.git.
func githubEnv(dir string) []string {
	u := filepath.ToSlash(dir)
	if !strings.HasPrefix(u, "/") { // C:/...
		u = "/" + u
	}
	return []string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=url.file://" + u + "/.insteadOf", "GIT_CONFIG_VALUE_0=https://github.com/"}
}
