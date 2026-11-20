package machine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	"github.com/samir2015dzh/aide/internal/provider"
)

func TestInRoot(t *testing.T) {
	sep := string(filepath.Separator)
	root := filepath.Join(sep+"srv", "code")
	for _, c := range []struct {
		path string
		in   bool
	}{
		{root, true},
		{filepath.Join(root, "app"), true},
		{root + "x", false}, // a sibling sharing the name's start
		{filepath.Dir(root), false},
		{sep, false},
	} {
		if got := inRoot(root, c.path); got != c.in {
			t.Errorf("inRoot(%q, %q) = %v", root, c.path, got)
		}
	}
	if !inRoot(sep, filepath.Join(sep, "x")) {
		t.Error("everything is under the top folder")
	}
	if runtime.GOOS == "windows" && (!inRoot(`C:\Code`, `c:\code\app`) || inRoot(`C:\Code`, `D:\Code`)) {
		t.Error("drive letters and names compare case aside on Windows, drives apart")
	}
}

// symlink makes a link to a folder, or skips the test where this user may
// not (Windows without developer mode).
func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("no symbolic links here: %v", err)
	}
}

// checkRoots lets in what is inside a root, by its real path: neither ".."
// nor a link leads out, and no roots is anywhere.
func TestCheckRoots(t *testing.T) {
	base := t.TempDir()
	root, out := filepath.Join(base, "root"), filepath.Join(base, "out")
	for _, d := range []string{filepath.Join(root, "app"), out} {
		os.MkdirAll(d, 0o755)
	}
	real, err := realDir(root)
	if err != nil {
		t.Fatal(err)
	}
	roots := []string{real}
	for _, p := range []string{out, base, filepath.Join(root, "app"), filepath.Join(root, "..", "out")} {
		if err := checkRoots(nil, filepath.Clean(p)); err != nil {
			t.Errorf("no roots, %s: %v", p, err)
		}
	}
	for _, p := range []string{root, filepath.Join(root, "app")} {
		if err := checkRoots(roots, p); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	for _, p := range []string{out, base, filepath.Clean(filepath.Join(root, "app", "..", "..", "out")), filepath.Join(root, "nowhere")} {
		err := checkRoots(roots, p)
		if connect.CodeOf(err) != connect.CodePermissionDenied || !IsOutsideRoots(err) || !strings.Contains(err.Error(), real) {
			t.Errorf("%s: %v", p, err)
		}
	}
	if IsOutsideRoots(connect.NewError(connect.CodePermissionDenied, os.ErrPermission)) {
		t.Error("another refusal is not the roots'")
	}
	if runtime.GOOS == "windows" {
		// A junction needs no privilege, and EvalSymlinks would not follow it.
		if b, err := exec.Command("cmd", "/c", "mklink", "/J", filepath.Join(root, "junction"), out).CombinedOutput(); err != nil {
			t.Fatalf("mklink /J: %v %s", err, b)
		}
		if err := checkRoots(roots, filepath.Join(root, "junction")); !IsOutsideRoots(err) {
			t.Errorf("a junction out of the root: %v", err)
		}
		if err := checkRoots(roots, strings.ToUpper(filepath.Join(root, "app"))); err != nil {
			t.Errorf("in another case, the drive letter too: %v", err)
		}
	}
	symlink(t, out, filepath.Join(root, "escape"))
	if err := checkRoots(roots, filepath.Join(root, "escape")); !IsOutsideRoots(err) {
		t.Errorf("a link out of the root: %v", err)
	}
	symlink(t, filepath.Join(root, "app"), filepath.Join(out, "in"))
	if err := checkRoots(roots, filepath.Join(out, "in")); err != nil {
		t.Errorf("a link into the root: %v", err)
	}
}

func rootsMachine(t *testing.T) *Machine {
	t.Helper()
	n, err := Open(Config{DataDir: t.TempDir(), Providers: map[string]provider.Provider{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n.Close() })
	return n
}

// The roots fence what the owner's devices browse and make projects of,
// and where agents' commands run; projects added before stay, named.
func TestWorkspaceRoots(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	code, other := filepath.Join(home, "code"), filepath.Join(home, "other")
	for _, d := range []string{filepath.Join(code, "app", ".git"), filepath.Join(other, ".git")} {
		os.MkdirAll(d, 0o755)
	}
	n := rootsMachine(t)
	ctx := context.Background()
	add := func(id, path string) error {
		_, err := n.execAdmin(ctx, &identityv1.AdminCommand{Command: &identityv1.AdminCommand_AddProject{AddProject: &identityv1.AddProject{
			ProjectId: id, Path: path, Workspace: identityv1.Workspace_WORKSPACE_IN_PLACE}}})
		return err
	}
	setRoots := func(roots ...string) (string, error) {
		return n.execAdmin(ctx, &identityv1.AdminCommand{Command: &identityv1.AdminCommand_SetWorkspaceRoots{SetWorkspaceRoots: &identityv1.SetWorkspaceRoots{Roots: roots}}})
	}

	// None set: anywhere, as before.
	if len(n.WorkspaceRoots()) != 0 || add("old", other) != nil {
		t.Fatalf("without roots: %v", n.WorkspaceRoots())
	}
	if _, err := ListFolders(n.WorkspaceRoots(), other, false); err != nil {
		t.Fatal(err)
	}
	if repos := FindRepos(ctx, n.WorkspaceRoots()); len(repos) != 2 {
		t.Fatalf("repositories without roots: %v", repos)
	}

	if _, err := setRoots("~/nowhere"); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("a root that is no folder: %v", err)
	}
	msg, err := setRoots("~/code", code)
	realCode, _ := realDir(code)
	if err != nil || !slices.Equal(n.WorkspaceRoots(), []string{realCode}) || !strings.Contains(msg, "old (") {
		t.Fatalf("set: %q %v %v", msg, n.WorkspaceRoots(), err)
	}
	if out := n.ProjectsOutsideRoots(); len(out) != 1 || !strings.HasPrefix(out[0], "old (") {
		t.Fatalf("outside: %v", out)
	}
	if ps, _ := n.st.Projects(); len(ps) != 1 {
		t.Fatalf("the project from before stays: %v", ps)
	}

	if repos := FindRepos(ctx, n.WorkspaceRoots()); len(repos) != 1 || repos[0].Path != "~"+string(filepath.Separator)+filepath.Join("code", "app") {
		t.Fatalf("repositories in the roots: %v", repos)
	}
	if err := add("new", filepath.Join(code, "app")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{other, filepath.Join(code, "app", "..", "..", "other")} {
		if err := add("bad", p); !IsOutsideRoots(err) {
			t.Fatalf("a project at %s: %v", p, err)
		}
	}

	if _, err := ListFolders(n.WorkspaceRoots(), other, false); !IsOutsideRoots(err) {
		t.Fatalf("listing outside: %v", err)
	}
	if _, err := ListFolders(n.WorkspaceRoots(), "~", false); !IsOutsideRoots(err) {
		t.Fatalf("listing the home folder outside: %v", err)
	}
	res, err := ListFolders(n.WorkspaceRoots(), "", false)
	if err != nil || res.FullPath != realCode || len(res.Folders) != 1 || res.Folders[0].Name != "app" {
		t.Fatalf("the first place to list is the first root: %v %v", res, err)
	}

	if msg, err := setRoots(); err != nil || len(n.WorkspaceRoots()) != 0 || !strings.Contains(msg, "cleared") || add("bad", other) != nil {
		t.Fatalf("cleared: %q %v", msg, err)
	}
}

// A command starts only inside the roots, and a refused one is in the ops log.
func TestExecInRoots(t *testing.T) {
	n := rootsMachine(t)
	ctx := context.Background()
	root, out := t.TempDir(), t.TempDir()
	if _, err := n.setWorkspaceRoots([]string{root}); err != nil {
		t.Fatal(err)
	}
	bot := &Principal{Kind: PrincipalClient, ClientID: "c_bot", Grant: &identityv1.Grant{MachineExec: true}}
	if _, err := n.exec(ctx, bot, &machinev1.ExecRequest{Command: "exit 0", Workdir: root}); err != nil {
		t.Fatal(err)
	}
	_, err := n.exec(ctx, bot, &machinev1.ExecRequest{Command: "exit 0", Workdir: out})
	if !IsOutsideRoots(err) {
		t.Fatalf("outside: %v", err)
	}
	log, _ := n.ExecLog()
	if r := log.Records[0]; r.Outcome != machinev1.ExecOutcome_EXEC_OUTCOME_REFUSED || !strings.Contains(r.Reason, "workspace roots") || r.Workdir != out {
		t.Fatalf("logged: %v", r)
	}
}
