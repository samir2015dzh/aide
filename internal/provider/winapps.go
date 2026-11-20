package provider

import (
	"path/filepath"
	"slices"
	"strings"
)

// A WindowsApp is how Windows itself records an installed app, for finding
// it wherever it was installed rather than in guessed folders (2026-11-20:
// DSH Desktop installed and in use, and not found in the two folders
// looked in).
type WindowsApp struct {
	// Names begin the DisplayName of its entry in Windows' list of installed
	// programs (the uninstall entries), compared without case.
	Names []string
	// Exes are its program's file names, for App Paths and for the programs
	// running now.
	Exes []string
}

// WindowsAppDirs are the folders the app is installed in, as Windows says:
// where its program runs from now, its App Paths entry, and its uninstall
// entry's InstallLocation, DisplayIcon and UninstallString. Each once, in
// that order; none off Windows.
func WindowsAppDirs(app WindowsApp) []string {
	var dirs []string
	add := func(d string) {
		if d == "" || d == "." {
			return
		}
		d = filepath.Clean(d)
		if !slices.ContainsFunc(dirs, func(x string) bool { return strings.EqualFold(x, d) }) {
			dirs = append(dirs, d)
		}
	}
	for _, exe := range app.Exes {
		for _, p := range RunningExecutables(exe) {
			add(filepath.Dir(p))
		}
	}
	for _, exe := range app.Exes {
		if p := appPath(exe); p != "" {
			add(filepath.Dir(p))
		}
	}
	for _, e := range uninstallEntries() {
		if !slices.ContainsFunc(app.Names, func(n string) bool { return n != "" && hasPrefixFold(e.name, n) }) {
			continue
		}
		add(e.location)
		add(filepath.Dir(commandPath(e.icon)))
		add(filepath.Dir(commandPath(e.uninstall)))
	}
	return dirs
}

// An uninstallEntry is an entry of Windows' list of installed programs.
type uninstallEntry struct {
	name, location, icon, uninstall string
}

// commandPath is the program a DisplayIcon or UninstallString names: a
// quoted path, or a path up to its first argument (",0" for an icon's
// index, " --uninstall", " /S").
func commandPath(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, `"`) {
		if i := strings.Index(s[1:], `"`); i >= 0 {
			return s[1 : 1+i]
		}
		return strings.Trim(s, `"`)
	}
	if i := strings.LastIndex(s, ","); i >= 0 && !strings.ContainsAny(s[i:], `\/`) {
		s = s[:i]
	}
	if i := strings.Index(strings.ToLower(s), ".exe"); i >= 0 {
		return s[:i+len(".exe")]
	}
	return s
}

func hasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

// WindowsProgram is where Windows says the program named command (without
// its .exe) is, when it is not on PATH: its App Paths entry, the program
// of that name running now, or command.exe in the folder (or its bin) of
// an installed program whose DisplayName contains command or begins with
// one of names. The same for every agent, the owner's own included, so
// that none needs its folders guessed; "" off Windows and when Windows
// knows of none.
func WindowsProgram(command string, names ...string) string {
	if command == "" || strings.ContainsAny(command, `/\`) {
		return ""
	}
	exe := command
	if !strings.EqualFold(filepath.Ext(exe), ".exe") {
		exe += ".exe"
	}
	for _, p := range RunningExecutables(exe) {
		if q := LookPath("", "", p); q != "" {
			return q
		}
	}
	if p := appPath(exe); p != "" {
		if q := LookPath("", "", p); q != "" {
			return q
		}
	}
	base := strings.TrimSuffix(exe, filepath.Ext(exe))
	for _, e := range uninstallEntries() {
		named := len(base) >= 4 && strings.Contains(strings.ToLower(e.name), strings.ToLower(base))
		named = named || slices.ContainsFunc(names, func(n string) bool { return n != "" && hasPrefixFold(e.name, n) })
		if !named {
			continue
		}
		for _, d := range []string{e.location, filepath.Dir(commandPath(e.icon)), filepath.Dir(commandPath(e.uninstall))} {
			if d == "" || d == "." {
				continue
			}
			for _, p := range []string{filepath.Join(d, exe), filepath.Join(d, "bin", exe)} {
				if q := LookPath("", "", p); q != "" {
					return q
				}
			}
		}
	}
	return ""
}
