package machine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"connectrpc.com/connect"
	"google.golang.org/genproto/googleapis/rpc/errdetails"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
)

// Workspace roots (docs/machine-ops.md §5): the folders the owner's devices
// may browse (ListFolders), make projects of (AddProject) and agents may
// run commands in (machine exec --workdir). None set, anywhere is: as
// before there were roots, so a machine nobody configured behaves as it
// did. Projects added before the roots stay usable; `aided roots` names
// those outside them. Sessions of GitHub repositories (--repo) work in the
// machine's own data folder, which no root needs to cover.

// kRoots: the roots (identityv1.SetWorkspaceRoots), as real paths.
const kRoots = "workspace_roots"

// OutsideRootsReason is the ErrorInfo reason of a refusal for a folder
// outside the roots, by which clients tell it from a grant's.
const OutsideRootsReason = "OUTSIDE_WORKSPACE_ROOTS"

// WorkspaceRoots are the machine's roots; none: anywhere.
func (n *Machine) WorkspaceRoots() []string {
	r := &identityv1.SetWorkspaceRoots{}
	if _, err := n.st.GetProto(kRoots, r); err != nil {
		n.log.Warn("workspace roots unreadable; allowing no folder", "err", err)
		return []string{""} // a root that holds nothing: refuse rather than open up
	}
	return r.Roots
}

// setWorkspaceRoots keeps roots, each as the real path of a folder, and
// says which projects are outside them.
func (n *Machine) setWorkspaceRoots(roots []string) (string, error) {
	var real []string
	for _, r := range roots {
		p, err := realDir(expandHome(strings.TrimSpace(r)))
		if err != nil {
			return "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("root %q: %w", r, err))
		}
		if !slices.Contains(real, p) {
			real = append(real, p)
		}
	}
	if err := n.st.SetProto(kRoots, &identityv1.SetWorkspaceRoots{Roots: real}); err != nil {
		return "", err
	}
	if len(real) == 0 {
		return "workspace roots cleared: any folder, as before roots", nil
	}
	msg := "workspace roots: " + strings.Join(real, ", ")
	if out := n.ProjectsOutsideRoots(); len(out) > 0 {
		msg += "; projects outside them stay usable: " + strings.Join(out, ", ") + " (aided project remove <name> to stop using one)"
	}
	return msg, nil
}

// ProjectsOutsideRoots names the projects outside the roots ("id (path)"),
// added before them: they stay usable.
func (n *Machine) ProjectsOutsideRoots() []string {
	roots := n.WorkspaceRoots()
	projects, _ := n.st.Projects()
	clones := filepath.Join(n.cfg.DataDir, "repos")
	var out []string
	for _, p := range projects {
		if inRoot(clones, p.Path) || checkRoots(roots, p.Path) == nil {
			continue
		}
		out = append(out, p.ID+" ("+p.Path+")")
	}
	return out
}

// checkRoots refuses path unless it is in one of roots (none: anywhere).
// path is a full, clean path, as the caller then uses it: what it is in is
// told by its real path, links (and on Windows junctions) followed, so
// neither ".." nor a link leads out.
func checkRoots(roots []string, path string) error {
	if len(roots) == 0 {
		return nil
	}
	real, err := realDir(path)
	if err == nil && slices.ContainsFunc(roots, func(r string) bool { return r != "" && inRoot(r, real) }) {
		return nil
	}
	shown := strings.Join(slices.DeleteFunc(slices.Clone(roots), func(r string) bool { return r == "" }), ", ")
	ce := connect.NewError(connect.CodePermissionDenied, fmt.Errorf("%s is outside this machine's workspace roots (%s)", path, shown))
	if d, err := connect.NewErrorDetail(&errdetails.ErrorInfo{Reason: OutsideRootsReason, Domain: "aide"}); err == nil {
		ce.AddDetail(d)
	}
	return ce
}

// IsOutsideRoots tells a refusal of checkRoots.
func IsOutsideRoots(err error) bool {
	var ce *connect.Error
	if !errors.As(err, &ce) {
		return false
	}
	for _, d := range ce.Details() {
		if v, err := d.Value(); err == nil {
			if info, ok := v.(*errdetails.ErrorInfo); ok && info.Reason == OutsideRootsReason {
				return true
			}
		}
	}
	return false
}

// inRoot: path is root or under it, both real paths. Windows compares
// names as NTFS does, case aside (the drive letter too).
func inRoot(root, path string) bool {
	eq := func(a, b string) bool { return a == b }
	if runtime.GOOS == "windows" {
		eq = strings.EqualFold
	}
	if eq(root, path) {
		return true
	}
	prefix := root
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	return len(path) > len(prefix) && eq(path[:len(prefix)], prefix)
}

// expandHome writes "~" and "~/…" (on Windows also "~\…") in full.
func expandHome(p string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, "~"+string(filepath.Separator)) {
		return filepath.Join(home, p[1:])
	}
	return p
}

// realDir is the real path of the folder at path, a full path.
func realDir(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%q is not a full path (or ~/…)", path)
	}
	real, err := realPath(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(real); err != nil || !st.IsDir() {
		return "", fmt.Errorf("%s is not a folder", path)
	}
	return real, nil
}
