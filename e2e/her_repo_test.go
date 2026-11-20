package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	localv1 "github.com/hermeshai/hermesh/gen/go/hermesh/local/v1"
	sessionv1 "github.com/hermeshai/hermesh/gen/go/hermesh/session/v1"
)

// Her starts a coding agent in a GitHub repository the owner never cloned
// or added, on the computer she names, without asking (docs/her-mesh.md
// §5). What the machine says of a repository it can't reach, or a branch it
// lacks, reaches her as it said it. Her branch goes to GitHub only when she
// pushes it, and her push runs without asking too.
func TestHerRepoSession(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	gh := t.TempDir()
	env := newHerEnvironmentWith(t, githubEnv(gh))
	w, c, ctx := env.w, env.c, context.Background()

	// On GitHub: acme/widget, with main and a feature branch.
	bare := filepath.Join(gh, "acme", "widget.git")
	gitOut(t, w.repo, "init", "-q", "--bare", "-b", "main", bare)
	work := filepath.Join(t.TempDir(), "work")
	gitOut(t, w.repo, "clone", "-q", bare, work)
	commit := func(name string) {
		if err := os.WriteFile(filepath.Join(work, name), []byte(name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitOut(t, work, "add", name)
		gitOut(t, work, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", name)
	}
	gitOut(t, work, "checkout", "-q", "-b", "main")
	commit("README.md")
	gitOut(t, work, "push", "-q", "origin", "main")
	gitOut(t, work, "checkout", "-q", "-b", "feature")
	commit("feature.txt")
	gitOut(t, work, "push", "-q", "origin", "feature")

	var her *localv1.Client
	eventually(t, time.Minute, "Her among the owner's agents", func() bool {
		her = herClient(t, env)
		return her != nil && listedMachine(t, c, env.machine).GetHer().GetMesh()
	})
	sessions := func() []*sessionv1.Session {
		res, err := c.ListSessions(ctx, &localv1.ListSessionsRequest{})
		if err != nil {
			t.Fatal(err)
		}
		return res.Sessions
	}
	session := func(id string) *sessionv1.Session {
		for _, s := range sessions() {
			if s.SessionId == id {
				return s
			}
		}
		return nil
	}

	// The repository's feature branch, on the owner's Mac.
	said := tellHer(t, env, meshCall("session_start", map[string]any{"repo": "https://github.com/acme/widget", "branch": "feature", "machine": "owner-mac",
		"prompt": "sleep 4s\nwrite made.txt by her session\nsay built it"}), "mesh ")
	first := sessionID.FindString(said)
	if !strings.HasPrefix(said, "mesh done") || first == "" {
		t.Fatalf("session_start: %s", said)
	}
	if s := session(first); s == nil || s.Repo != "acme/widget" || s.RepoBranch != "feature" || s.ProjectId != "github.acme.widget" || s.CreatedBy != her.ClientId {
		t.Fatalf("her session: %+v", s)
	}
	herSaid(t, env, "news of the turn's end", `kind="turn_done"`, first)
	if _, err := os.Stat(filepath.Join(w.worktree(first), "feature.txt")); err != nil {
		t.Fatalf("the worktree is not on feature: %v", err)
	}
	if strings.TrimSpace(gitOut(t, bare, "branch", "--list", "hermesh/*")) != "" {
		t.Fatal("pushed unasked")
	}

	// A start by the project its clone is now runs there; one on a branch
	// GitHub lacks fails with the machine's words.
	said = tellHer(t, env, meshCall("session_start", map[string]any{"project": "owner-mac/github.acme.widget", "prompt": "sleep 4s\nsay again"}), "mesh ")
	if !strings.HasPrefix(said, "mesh done") {
		t.Fatalf("a start in the clone's project: %s", said)
	}
	herSaid(t, env, "news of the second turn", `kind="turn_done"`, sessionID.FindString(said))
	before := len(sessions())
	said = tellHer(t, env, meshCall("session_start", map[string]any{"repo": "acme/widget", "branch": "gone", "machine": "owner-mac", "prompt": "say never"}), "mesh ")
	if !strings.HasPrefix(said, "mesh failed") || !strings.Contains(said, "GitHub repository acme/widget has no branch") {
		t.Fatalf("a missing branch: %s", said)
	}
	// A repository the machine can't reach fails with why.
	said = tellHer(t, env, meshCall("session_start", map[string]any{"repo": "acme/private", "machine": "owner-mac", "prompt": "say never"}), "mesh ")
	if !strings.HasPrefix(said, "mesh failed") || !strings.Contains(said, "this machine cannot reach GitHub repository acme/private with its own git login") {
		t.Fatalf("an unreachable repository: %s", said)
	}
	// A repository without a computer: she is told to name one.
	if said = tellHer(t, env, meshCall("session_start", map[string]any{"repo": "acme/widget", "prompt": "say never"}), "mesh "); !strings.HasPrefix(said, "mesh failed") ||
		!strings.Contains(said, "machine is required with repo") {
		t.Fatalf("no computer: %s", said)
	}
	if after := len(sessions()); after != before {
		t.Fatalf("a failed start made a session: %d, then %d", before, after)
	}

	// Her pushes her session's branch, unasked as the rest, and gets the
	// page for its pull request.
	said = tellHer(t, env, meshCall("session_push", map[string]any{"session_id": first}), "mesh ")
	branch := "hermesh/" + first
	if !strings.HasPrefix(said, "mesh done") || !strings.Contains(said, "https://github.com/acme/widget/compare/feature..."+branch+"?expand=1") {
		t.Fatalf("session_push: %s", said)
	}
	if got := gitOut(t, bare, "show", branch+":made.txt"); got != "by her session" {
		t.Fatalf("the pushed branch: %q", got)
	}
	if exec.Command("git", "-C", bare, "rev-parse", "--verify", "--quiet", "refs/heads/feature").Run() != nil {
		t.Fatal("feature is gone")
	}
}
