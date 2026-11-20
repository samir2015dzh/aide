// Package paths says where Aide keeps its things on this computer
// (docs/windows.md §4.1). apps/desktop/src/main/paths.ts mirrors DataDir
// and LocalAPI.
package paths

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/samir2015dzh/aide/internal/brandenv"
	"github.com/samir2015dzh/aide/internal/legacy"
)

// DataDir is aided's data directory: $AIDE_DATA_DIR, else this
// system's place for it.
func DataDir() string {
	if v := brandenv.Get("DATA_DIR"); v != "" {
		return v
	}
	return kept(dataDir(), dataIdentity)
}

// CLIHome is where the agents' CLI keeps its key and state: $AIDE_HOME,
// else this system's place for it.
func CLIHome() string {
	if v := brandenv.Get("HOME"); v != "" {
		return v
	}
	return kept(cliHome(), cliIdentity)
}

// What in a data directory, and in a CLI home, says it holds an identity
// (the owner's device, the machine, the CLI's key): a directory with none
// of these is not in use, whatever else it holds. Mirrored in
// apps/desktop/src/main/legacy.ts.
var (
	dataIdentity = []string{
		filepath.Join("owner", "current"),               // the owner's device, in profiles
		filepath.Join("owner", "owner.db"),              // the owner's device, from before profiles
		filepath.Join("machine", "keys", "machine.key"), // the machine
	}
	cliIdentity = []string{"client.key"}
)

// kept is dir, or the directory an installation from before the rename
// made in its place (its old name, internal/legacy) while dir holds
// no identity and that one does: its worktrees, sessions and services point
// into it by absolute path, so it is used where it is, not moved. Whether
// dir exists does not decide: something can make it (a service's
// definition, the CLI's home inside it) before anything is kept there. An
// old directory without an identity is still used while dir does not exist,
// as before.
func kept(dir string, identity []string) string {
	old := legacyDir(dir)
	if old == dir || holds(dir, identity) {
		return dir
	}
	if holds(old, identity) || (!exists(dir) && exists(old)) {
		return old
	}
	return dir
}

// holds reports whether dir has any of the files that say it is in use.
func holds(dir string, identity []string) bool {
	for _, f := range identity {
		if exists(filepath.Join(dir, f)) {
			return true
		}
	}
	return false
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Local is where the machine machineID takes the connections of its own
// user's agents on this computer, so that they need not go through the
// relay to reach it (docs/technical-design.md §4.3): "" when AIDE_LOCAL
// is off. It depends on nothing but the machine and the user, so an agent
// finds it knowing neither the machine's data directory nor its own home;
// $AIDE_LOCAL_DIR puts it elsewhere.
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
