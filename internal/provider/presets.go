package provider

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// The three-tier permission presets (工单 #89 / #111), least allowed first,
// as Aide's gate enforces them for an agent with no sandbox Aide can
// configure (Antigravity, and Claude Code since 工单 #180): read-only takes
// reads and searches; workspace-write also takes edits inside the session's
// workspace (symlinks resolved), and denies shell commands and network
// fetches, which Aide cannot confine to the workspace or keep offline;
// danger-full-access allows them. Any other mode is not limited here.
const (
	PresetReadOnly       = "read-only"
	PresetWorkspaceWrite = "workspace-write"
	PresetFullAccess     = "danger-full-access"
)

// PresetModes are the presets as an agent offers them.
func PresetModes() []Mode {
	return []Mode{{ID: PresetReadOnly, Name: PresetReadOnly}, {ID: PresetWorkspaceWrite, Name: PresetWorkspaceWrite}, {ID: PresetFullAccess, Name: PresetFullAccess}}
}

// LivePresetGate is PresetGate with the mode the session has now, each call
// (工单 #170: a mode changed while it runs decides its next call).
func LivePresetGate(mode func() string, workDir string, next Gate) Gate {
	return func(ctx context.Context, a ToolAction) Decision {
		return PresetGate(mode(), workDir, next)(ctx, a)
	}
}

// PresetGate enforces mode (one of the presets) before next.
func PresetGate(mode, workDir string, next Gate) Gate {
	if next == nil {
		next = func(context.Context, ToolAction) Decision { return Decision{Allow: true} }
	}
	if mode == PresetFullAccess {
		return next
	}
	cleanWork := ""
	if workDir != "" {
		if abs, err := filepath.Abs(workDir); err == nil {
			cleanWork = resolveExisting(filepath.Clean(abs))
		}
	}
	deny := func(reason string) Decision { return Decision{Allow: false, Reason: reason} }
	return func(ctx context.Context, a ToolAction) Decision {
		switch mode {
		case "read-only":
			if a.Kind != ToolRead && a.Kind != ToolSearch {
				return deny("read-only mode: modifying files or running commands is not allowed")
			}
		case "workspace-write":
			switch a.Kind {
			case ToolShell:
				return deny("workspace-write mode: Aide cannot keep shell commands inside the workspace, so they are not allowed; start the session in danger-full-access to allow them")
			case ToolNetwork:
				return deny("workspace-write mode: network access is not allowed; start the session in danger-full-access to allow it")
			case ToolEdit:
				if cleanWork == "" {
					return deny("workspace-write mode: the session has no workspace to check the edit against")
				}
				paths := editPaths(a)
				if len(paths) == 0 {
					return deny("workspace-write mode: the edit names no file, so Aide cannot check that it stays inside the workspace")
				}
				for _, p := range paths {
					if !insideDir(cleanWork, p) {
						return deny("workspace-write mode: " + p + " is outside the session workspace")
					}
				}
			}
		}
		return next(ctx, a)
	}
}

// editPaths are the files an edit names: its locations and diffs, else
// the path fields of its input (Antigravity's or other agents' spelling).
func editPaths(a ToolAction) []string {
	var out []string
	for _, p := range a.Paths {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	if len(out) > 0 {
		return out
	}
	var in map[string]any
	if json.Unmarshal(a.Raw, &in) != nil {
		return nil
	}
	for _, k := range []string{"path", "file_path", "filePath", "TargetFile", "target_file", "AbsolutePath", "absolute_path", "source", "destination", "new_path"} {
		if v, ok := in[k].(string); ok && strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	}
	return out
}

// insideDir reports whether p (relative to dir when not absolute) is dir or
// below it once symlinks in its existing part are resolved. dir is resolved.
func insideDir(dir, p string) bool {
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	rel, err := filepath.Rel(dir, resolveExisting(filepath.Clean(p)))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// resolveExisting resolves the symlinks of p's deepest existing ancestor (p
// itself when it exists) and appends the rest, which does not exist yet. A
// dangling symlink is followed to where a write through it would land.
func resolveExisting(p string) string { return resolveExistingN(p, 0) }

func resolveExistingN(p string, depth int) string {
	rest := ""
	for cur := p; ; {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(r, rest)
		}
		if fi, err := os.Lstat(cur); err == nil && fi.Mode()&os.ModeSymlink != 0 && depth < 40 {
			if t, err := os.Readlink(cur); err == nil {
				if !filepath.IsAbs(t) {
					t = filepath.Join(filepath.Dir(cur), t)
				}
				return resolveExistingN(filepath.Join(filepath.Clean(t), rest), depth+1)
			}
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}
