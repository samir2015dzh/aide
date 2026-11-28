package machine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/provider"
	"github.com/samir2015dzh/aide/internal/shellenv"
)

// What a machine offers the owner's devices while they set it up from
// afar (docs/onboarding.md §5.5): the repositories to make projects of,
// the folders to browse for one, and a test of each coding agent.

const (
	repoDepth   = 3   // levels under the home folder
	repoLimit   = 100 // repositories listed at most
	folderLimit = 500 // folders in one folder listed at most
	branchLimit = 200 // branches of a repository listed at most
)

// repoSkip are folders never searched: dependencies, caches and builds.
var repoSkip = map[string]bool{"node_modules": true, "vendor": true, "dist": true, "build": true, "target": true, "venv": true,
	"__pycache__": true, "go": true, "snap": true}

// protectedOnMac are the folders macOS asks the person before an app reads:
// searching them would put a prompt on a screen nobody may be watching.
var protectedOnMac = map[string]bool{"Desktop": true, "Documents": true, "Downloads": true, "Library": true, "Movies": true,
	"Music": true, "Pictures": true, "Public": true, "Applications": true}

// FindRepos lists the git repositories under the home folder, newest commit
// first, as "~/…" paths; with workspace roots (roots.go), those in them.
// It reads only directory entries and the .git files that tell the branch
// and the last commit.
func FindRepos(ctx context.Context, roots []string) []*machinev1.Repo {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var repos []*machinev1.Repo
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if ctx.Err() != nil || len(repos) >= repoLimit {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || repoSkip[e.Name()] {
				continue
			}
			if depth == 1 && runtime.GOOS == "darwin" && protectedOnMac[e.Name()] {
				continue
			}
			path := filepath.Join(dir, e.Name())
			if r := repoAt(home, path); r != nil {
				if checkRoots(roots, path) == nil {
					repos = append(repos, r)
				}
				continue
			}
			if depth < repoDepth {
				walk(path, depth+1)
			}
		}
	}
	walk(home, 1)
	sort.SliceStable(repos, func(i, j int) bool { return repos[i].LastCommit.AsTime().After(repos[j].LastCommit.AsTime()) })
	return repos
}

// repoAt describes the repository at path, nil if it is none.
func repoAt(home, path string) *machinev1.Repo {
	git := filepath.Join(path, ".git")
	st, err := os.Stat(git)
	if err != nil || !st.IsDir() {
		return nil
	}
	r := &machinev1.Repo{Path: "~" + strings.TrimPrefix(path, home)}
	if head, err := os.ReadFile(filepath.Join(git, "HEAD")); err == nil {
		r.Branch = strings.TrimPrefix(strings.TrimSpace(string(head)), "ref: refs/heads/")
		if len(r.Branch) == 40 { // detached
			r.Branch = r.Branch[:7]
		}
	}
	for _, f := range []string{"logs/HEAD", "index", "HEAD"} {
		if fi, err := os.Stat(filepath.Join(git, f)); err == nil {
			r.LastCommit = timestamppb.New(fi.ModTime())
			break
		}
	}
	return r
}

// ListFolders lists the folders in one folder for Add a Project's folder
// browser, each with whether it is a git repository. path is "~/…", "~"
// (or empty) or a full path. Like FindRepos it reads only directory
// entries and .git files, and it doesn't look into the folders macOS
// protects: the person opens one of those on purpose. With workspace roots
// (roots.go) it lists only folders in them, and an empty path is the first
// root when the home folder is outside them.
func ListFolders(roots []string, path string, hidden bool) (*machinev1.ListFoldersResponse, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := path
	switch {
	case path == "" && checkRoots(roots, home) != nil && roots[0] != "":
		dir = roots[0]
	case path == "" || path == "~":
		dir = home
	case strings.HasPrefix(path, "~/"):
		dir = filepath.Join(home, path[2:])
	case !filepath.IsAbs(path):
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("%q is not a full path", path))
	}
	dir = filepath.Clean(dir)
	if _, err := os.Stat(dir); err == nil {
		if err := checkRoots(roots, dir); err != nil {
			return nil, err
		}
	}
	entries, err := os.ReadDir(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no folder %s", path))
	case errors.Is(err, fs.ErrPermission):
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("not allowed to read %s", path))
	case err != nil:
		return nil, err
	}
	res := &machinev1.ListFoldersResponse{Path: homePath(home, dir), FullPath: dir}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		res.FullPath = real
	}
	if r := repoAt(home, dir); r != nil {
		res.Repo, res.Branch, res.Branches = true, r.Branch, repoBranches(dir)
	}
	for _, e := range entries {
		isDir := e.IsDir()
		if e.Type()&fs.ModeSymlink != 0 {
			st, err := os.Stat(filepath.Join(dir, e.Name()))
			isDir = err == nil && st.IsDir() && checkRoots(roots, filepath.Join(dir, e.Name())) == nil // not one leading out
		}
		if !isDir {
			res.Files++
			continue
		}
		if !hidden && strings.HasPrefix(e.Name(), ".") || len(res.Folders) >= folderLimit {
			continue
		}
		p := filepath.Join(dir, e.Name())
		f := &machinev1.Folder{Name: e.Name(), Path: homePath(home, p), Protected: dir == home && runtime.GOOS == "darwin" && protectedOnMac[e.Name()]}
		if !f.Protected {
			if r := repoAt(home, p); r != nil {
				f.Repo, f.Branch = true, r.Branch
			}
		}
		res.Folders = append(res.Folders, f)
	}
	// As Finder sorts them: case aside.
	slices.SortStableFunc(res.Folders, func(a, b *machinev1.Folder) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return res, nil
}

// homePath writes a path as the person does: "~/app" under the home folder.
func homePath(home, path string) string {
	if path == home {
		return "~"
	}
	if rel, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~/" + filepath.ToSlash(rel)
	}
	return path
}

// repoBranches lists a repository's local branches by name, from its refs
// and packed-refs: no git process.
func repoBranches(repo string) []string {
	git := filepath.Join(repo, ".git")
	names := map[string]bool{}
	heads := filepath.Join(git, "refs", "heads")
	filepath.WalkDir(heads, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if rel, err := filepath.Rel(heads, p); err == nil {
				names[filepath.ToSlash(rel)] = true
			}
		}
		return nil
	})
	if b, err := os.ReadFile(filepath.Join(git, "packed-refs")); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if _, ref, ok := strings.Cut(strings.TrimSpace(line), " "); ok {
				if name, ok := strings.CutPrefix(ref, "refs/heads/"); ok {
					names[name] = true
				}
			}
		}
	}
	out := slices.Sorted(maps.Keys(names))
	return out[:min(len(out), branchLimit)]
}

// testPrompt is the turn Test runs: short, no tools, one word back.
const testPrompt = "Reply with the single word ready, and nothing else. Use no tools."

// testTimeout bounds a test turn, sign-in prompts and slow starts included.
const testTimeout = 90 * time.Second

// notSignedInReply is an agent's reply saying it is not signed in.
var notSignedInReply = regexp.MustCompile(`(?i)not logged in|please run /login|invalid api key`)

// TestAgent runs one short turn of a coding agent in a scratch folder and
// reports its answer: proof that it is installed and signed in. Nothing is
// kept and no tool may run.
func (n *Machine) TestAgent(ctx context.Context, name string) (*machinev1.TestAgentResponse, error) {
	p := n.provider(name)
	if p == nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no coding agent %q here", name))
	}
	dir, err := os.MkdirTemp("", "aide-test-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	tmp := filepath.Join(dir, "tmp")
	os.Mkdir(tmp, 0o700)
	ctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()
	start := time.Now()
	n.audit.Log(AuditRecord{Kind: "test", Subject: name})
	sess, err := p.Start(ctx, provider.StartSpec{WorkDir: dir, TempDir: tmp, Prompt: testPrompt, Env: shellenv.Env(tmp),
		Gate: func(context.Context, provider.ToolAction) provider.Decision {
			return provider.Decision{Reason: "a test turn uses no tools"}
		}})
	if err != nil {
		n.testSawSignIn(name, provider.FailureOf(err, sessionv1.FailureKind_FAILURE_KIND_UNKNOWN))
		return &machinev1.TestAgentResponse{Reply: err.Error(), Millis: uint32(time.Since(start).Milliseconds())}, nil
	}
	defer sess.Close()
	var reply string
	for {
		select {
		case <-ctx.Done():
			return &machinev1.TestAgentResponse{Reply: "no answer in time", Millis: uint32(time.Since(start).Milliseconds())}, nil
		case <-sess.Done():
			why := "it stopped before answering"
			if err := sess.Err(); err != nil && !errors.Is(err, fs.ErrClosed) {
				why = err.Error()
			}
			return &machinev1.TestAgentResponse{Reply: why, Millis: uint32(time.Since(start).Milliseconds())}, nil
		case e := <-sess.Events():
			switch e.Kind {
			case provider.EventAgentMessage:
				reply = strings.TrimSpace(e.Text)
			case provider.EventError:
				n.testSawSignIn(name, eventFailure(e))
				return &machinev1.TestAgentResponse{Reply: e.Text, Millis: uint32(time.Since(start).Milliseconds())}, nil
			case provider.EventTurnCompleted:
				if reply == "" {
					reply = strings.TrimSpace(e.Text)
				}
				// Claude Code answers "Not logged in · Please run /login" as a
				// message: that is no answer (工单 #44).
				ok := !e.IsError && reply != "" && !notSignedInReply.MatchString(reply)
				switch {
				case ok:
					n.agentSignedIn(name, true, "")
				case notSignedInReply.MatchString(reply):
					n.agentSignedIn(name, false, reply)
				case e.IsError:
					n.testSawSignIn(name, eventFailure(e))
				}
				return &machinev1.TestAgentResponse{Ok: ok, Reply: reply, Millis: uint32(time.Since(start).Milliseconds())}, nil
			}
		}
	}
}

// testSawSignIn keeps what a failed test turn showed of the agent's sign-in
// (工单 #180): a turn that failed "not logged in" is signed out, as a session's
// is (Sessions.sawSignIn); any other failure says nothing of it. One that
// answered is signed in (TestAgent): Run Test confirms an ACP agent's sign-in
// as a session does, not only the task it starts.
func (n *Machine) testSawSignIn(name string, f *provider.Failure) {
	if f.GetKind() == sessionv1.FailureKind_FAILURE_KIND_AUTH {
		n.agentSignedIn(name, false, f.Message)
	}
}

// eventFailure is why an error or failed turn ended: its own failure, else
// its text classified.
func eventFailure(e provider.Event) *provider.Failure {
	if e.Failure != nil {
		return e.Failure
	}
	return &provider.Failure{Kind: provider.Classify(e.Text), Message: e.Text}
}
