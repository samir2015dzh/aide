package acp

import (
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
// Antigravity always routes tool permissions through Aide's gate, where the
// selected preset is enforced.
var antigravityModes = []provider.Mode{
	{ID: "read-only", Name: "read-only"},
	{ID: "workspace-write", Name: "workspace-write"},
	{ID: "danger-full-access", Name: "danger-full-access"},
}

const antigravityDefaultMode = "workspace-write"

func antigravityValidMode(mode string) bool {
	if mode == "" {
		return true
	}
	for _, m := range antigravityModes {
		if m.ID == mode {
			return true
		}
	}
	return false
}

func antigravityPermissionGate(mode, workDir string, next provider.Gate) provider.Gate {
	if mode == "" || mode == "danger-full-access" {
		return next
	}
	cleanWork := ""
	if workDir != "" {
		if abs, err := filepath.Abs(workDir); err == nil {
			cleanWork = filepath.Clean(abs)
		}
	}
	return func(ctx context.Context, a provider.ToolAction) provider.Decision {
		switch mode {
		case "read-only":
			if a.Kind != provider.ToolRead && a.Kind != provider.ToolSearch {
				return provider.Decision{
					Allow:  false,
					Reason: "read-only mode: modifying files or running commands is not allowed",
				}
			}
		case "workspace-write":
			if a.Kind == provider.ToolEdit && cleanWork != "" {
				for _, p := range a.Paths {
					if p == "" {
						continue
					}
					target := p
					if !filepath.IsAbs(target) {
						target = filepath.Join(cleanWork, target)
					}
					if abs, err := filepath.Abs(target); err == nil {
						rel, err := filepath.Rel(cleanWork, filepath.Clean(abs))
						if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
							return provider.Decision{
								Allow:  false,
								Reason: "workspace-write mode: path is outside the session workspace",
							}
						}
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
