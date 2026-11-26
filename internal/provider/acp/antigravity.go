package acp

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/samir2015dzh/aide/internal/provider"
)

// First-class permission presets for Antigravity, aligned with DSH's three-tier
// presets (工单 #111 升级补充). On the ACP wire Aide keeps modeId="default" so
// Antigravity routes tool permissions through Aide's gate, where the selected
// preset is enforced. A session started without a mode gets the default it
// advertises (workspace-write), never a pass-through (审计 C).
//
// Antigravity has no sandbox Aide can configure, so the presets are what the
// gate can check, not what Codex's or DSH's sandboxes do: workspace-write
// takes edits inside the session's workspace (symlinks resolved), and denies
// shell commands and network fetches, which Aide cannot confine to the
// workspace or keep offline (Codex's workspace-write sandbox has no network
// and cannot write outside the workspace). danger-full-access allows them.
// A denial stops a call only when the agent asks permission for it; one the
// agent runs without asking is reported, and its turn cancelled (see
// session.unaskedDenied).
var antigravityModes = []provider.Mode{
	{ID: "read-only", Name: "read-only"},
	{ID: "workspace-write", Name: "workspace-write"},
	{ID: "danger-full-access", Name: "danger-full-access"},
}

const antigravityDefaultMode = "workspace-write"

func antigravityValidMode(mode string) bool {
	if mode == "" {
		return true // the default, antigravityDefaultMode
	}
	for _, m := range antigravityModes {
		if m.ID == mode {
			return true
		}
	}
	return false
}

// antigravityLiveGate is antigravityPermissionGate with the mode the session
// has now, each call (工单 #167: a mode changed while it runs decides its
// next call).
func antigravityLiveGate(mode func() string, workDir string, next provider.Gate) provider.Gate {
	return func(ctx context.Context, a provider.ToolAction) provider.Decision {
		return antigravityPermissionGate(mode(), workDir, next)(ctx, a)
	}
}

func antigravityPermissionGate(mode, workDir string, next provider.Gate) provider.Gate {
	mode = cmp.Or(mode, antigravityDefaultMode)
	if mode == "danger-full-access" {
		return next
	}
	cleanWork := ""
	if workDir != "" {
		if abs, err := filepath.Abs(workDir); err == nil {
			cleanWork = resolveExisting(filepath.Clean(abs))
		}
	}
	deny := func(reason string) provider.Decision { return provider.Decision{Allow: false, Reason: reason} }
	return func(ctx context.Context, a provider.ToolAction) provider.Decision {
		switch mode {
		case "read-only":
			if a.Kind != provider.ToolRead && a.Kind != provider.ToolSearch {
				return deny("read-only mode: modifying files or running commands is not allowed")
			}
		case "workspace-write":
			switch a.Kind {
			case provider.ToolShell:
				return deny("workspace-write mode: Aide cannot keep shell commands inside the workspace, so they are not allowed; start the session in danger-full-access to allow them")
			case provider.ToolNetwork:
				return deny("workspace-write mode: network access is not allowed; start the session in danger-full-access to allow it")
			case provider.ToolEdit:
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
		if next != nil {
			return next(ctx, a)
		}
		return provider.Decision{Allow: true}
	}
}

// editPaths are the files an edit names: its locations and diffs, else
// the path fields of its input (Antigravity's or other agents' spelling).
func editPaths(a provider.ToolAction) []string {
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

// antigravityModelLabel formats observed Antigravity models under the unified
// #117 naming rule ("<Provider> · <specific model ID>") when a vendor or group
// is known; unknown synthetic IDs keep their existing label or ID.
func antigravityModelLabel(id, group, name string) string {
	if p := providerPrefix(group, id); p != "" {
		_, model := modelParts(id)
		return p + " · " + model
	}
	if vendor := inferModelVendor(id); vendor != "" {
		return vendor + " · " + id
	}
	if name != "" {
		return name
	}
	return id
}

func inferModelVendor(id string) string {
	low := strings.ToLower(strings.TrimSpace(id))
	switch {
	case strings.HasPrefix(low, "gemini-"), strings.HasPrefix(low, "gemini_"), strings.HasPrefix(low, "gemma-"):
		return "Google"
	case strings.HasPrefix(low, "claude-"), strings.HasPrefix(low, "claude_"):
		return "Anthropic"
	case strings.HasPrefix(low, "gpt-"), strings.HasPrefix(low, "o1-"), strings.HasPrefix(low, "o3-"), strings.HasPrefix(low, "o4-"), strings.HasPrefix(low, "codex-"):
		return "OpenAI"
	case strings.HasPrefix(low, "deepseek-"):
		return "DeepSeek"
	default:
		return ""
	}
}

type antigravityAuth int

const (
	antigravityAuthUnconfigured antigravityAuth = iota
	antigravityAuthConfigured
	antigravityAuthUnconfirmed
)

// The official server requires an explicitly selected auth method, saved by
// authenticate. This only checks configuration; the server validates credentials
// when a session starts. Never read or copy the desktop app's credentials.
//
// Missing settings.json or an empty auth.type means explicitly not configured
// (not logged in, so stale session sign-in cache cannot resurrect a logged-out
// state — 工单 #111). Unreadable or malformed settings.json falls back to
// unconfirmed (工单 #108).
func antigravityAuthState(env []string) antigravityAuth {
	dir := ""
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, "GEMINI_HOME="); ok && v != "" {
			dir = v
		}
	}
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return antigravityAuthUnconfirmed
		}
		dir = filepath.Join(home, ".gemini")
	}
	b, err := os.ReadFile(filepath.Join(dir, "antigravity-acp", "settings.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return antigravityAuthUnconfigured
		}
		return antigravityAuthUnconfirmed
	}
	var settings struct {
		Auth struct {
			Type string `json:"type"`
		} `json:"auth"`
	}
	if json.Unmarshal(b, &settings) != nil {
		return antigravityAuthUnconfirmed
	}
	if settings.Auth.Type != "" {
		return antigravityAuthConfigured
	}
	return antigravityAuthUnconfigured
}
