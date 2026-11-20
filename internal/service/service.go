// Package service installs hermeshd as a per-user background service: a
// launchd LaunchAgent on macOS, a systemd user unit on Linux, a Task
// Scheduler logon task on Windows.
package service

import (
	"errors"
	"strings"
)

// The service's names. An installation from before the rename keeps its
// service under the names it had (internal/legacy): Install updates that one
// rather than add a second, and the rest manage it.
const (
	newLabel = "com.aide.daemon"
	newUnit  = "aided.service"
	newTask  = "Aide"
)

// ErrNoSystemd: this system has no systemd user manager to run the
// service (a container, WSL without systemd).
var ErrNoSystemd = errors.New("this system has no systemd for this user (a container, or WSL). Keep Aide running with\n  nohup aided run >/dev/null 2>&1 &\nor make `aided run` the container's command")

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}
