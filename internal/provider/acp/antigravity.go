package acp

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

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
