// Package paths says where Hermesh keeps its things on this computer
// (docs/windows.md §4.1). apps/desktop/src/main/paths.ts mirrors DataDir
// and LocalAPI.
package paths

import (
	"os"
	"strings"
	"path/filepath"

	"github.com/hermeshai/hermesh/internal/brandenv"
	"github.com/hermeshai/hermesh/internal/legacy"
)

// DataDir is hermeshd's data directory: $HERMESH_DATA_DIR, else this
// system's place for it.
func DataDir() string {
	if v := brandenv.Get("DATA_DIR"); v != "" {
		return v
	}
	return kept(dataDir())
}

// CLIHome is where the agents' CLI keeps its key and state: $HERMESH_HOME,
// else this system's place for it.
func CLIHome() string {
	if v := brandenv.Get("HOME"); v != "" {
		return v
	}
	return kept(cliHome())
}

// kept is dir, or the directory an installation from before the rename made
// in its place (…/hermesh for …/aide, internal/legacy), while dir does not
// exist and that one does: its worktrees, sessions and services point into
// it by absolute path, so it is used where it is, not moved.
func kept(dir string) string {
	if _, err := os.Stat(dir); err == nil {
		return dir
	}
	old := legacyDir(dir)
	if old == dir {
		return dir
	}
	if _, err := os.Stat(old); err == nil {
		return old
	}
	return dir
}

// Local is where the machine machineID takes the connections of its own
// user's agents on this computer, so that they need not go through the
// relay to reach it (docs/technical-design.md §4.3): "" when HERMESH_LOCAL
// is off. It depends on nothing but the machine and the user, so an agent
// finds it knowing neither the machine's data directory nor its own home;
// $HERMESH_LOCAL_DIR puts it elsewhere.
func Local(machineID string) string {
	if brandenv.Get("LOCAL") == "off" || machineID == "" {
		return ""
	}
	dir := brandenv.Get("LOCAL_DIR")
	if dir == "" {
		dir = localDir()
	}
	return filepath.Join(dir, machineID+localExt)
}

// legacyDir is dir with its brand's name, in the name of its last Aide
// element, as it was before the rename.
func legacyDir(dir string) string {
	parts := strings.Split(filepath.ToSlash(dir), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		switch parts[i] {
		case "aide":
			parts[i] = legacy.DataDir
			return filepath.FromSlash(strings.Join(parts, "/"))
		case "Aide":
			parts[i] = legacy.DataDirTitle
			return filepath.FromSlash(strings.Join(parts, "/"))
		}
	}
	return dir
}
