package acp

import (
	"cmp"
	"context"
	"encoding/json"
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
// has now, each call (工单 #170: a mode changed while it runs decides its
// next call).
func antigravityLiveGate(mode func() string, workDir string, next provider.Gate) provider.Gate {
	return func(ctx context.Context, a provider.ToolAction) provider.Decision {
		return antigravityPermissionGate(mode(), workDir, next)(ctx, a)
	}
}

func antigravityPermissionGate(mode, workDir string, next provider.Gate) provider.Gate {
	return provider.PresetGate(cmp.Or(mode, antigravityDefaultMode), workDir, next)
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
// An empty auth.type in settings.json means the ACP server's own sign-in was
// taken back: explicitly not configured, so stale session sign-in cache
// cannot resurrect a logged-out state (工单 #111). A missing settings.json is
// no such evidence: the file appears only once the ACP authenticate flow has
// run, and someone signed in inside the Antigravity app itself
// (~/.gemini/antigravity/) has none (工单 #180) — unconfirmed, so what a
// session shows decides (工单 #108/#112). Unreadable or malformed
// settings.json is unconfirmed too.
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
		return antigravityAuthUnconfirmed // missing included (工单 #180)
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
