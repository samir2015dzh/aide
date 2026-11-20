package provider

import (
	"path/filepath"
	"strings"
)

// Where Windows says a desktop app is (winapps_windows.go): its uninstall
// entry, its App Paths entry, or the program running now. An app installed
// somewhere else than its installer's default (another drive, a folder the
// owner chose) is found there, not only by guessing paths.

// InstalledApp is one uninstall entry's say of where an app is.
type InstalledApp struct {
	DisplayName, InstallLocation, DisplayIcon, UninstallString string
}

// Dir is the app's folder: its InstallLocation, else the folder of the
// program its DisplayIcon names (an icon is "C:\…\App.exe,0"); "" when
// neither says. The uninstaller's folder is not the app's for every
// installer (Squirrel's Update.exe is above the versions), so it is not used.
func (a InstalledApp) Dir() string {
	if d := strings.Trim(strings.TrimSpace(a.InstallLocation), `"`); d != "" {
		return filepath.Clean(d)
	}
	icon := strings.TrimSpace(a.DisplayIcon)
	if i := strings.LastIndex(icon, ","); i > 0 && !strings.ContainsAny(icon[i+1:], `\/`) {
		icon = icon[:i]
	}
	icon = strings.Trim(strings.TrimSpace(icon), `"`)
	if strings.EqualFold(filepath.Ext(icon), ".exe") {
		return filepath.Dir(icon)
	}
	return ""
}

// named says whether the entry is one of the apps named: its DisplayName
// is the name, or the name and a version after it ("DSH Desktop 0.4.1").
func (a InstalledApp) named(names []string) bool {
	d := strings.ToLower(strings.TrimSpace(a.DisplayName))
	for _, n := range names {
		n = strings.ToLower(n)
		rest, ok := strings.CutPrefix(d, n+" ")
		rest = strings.TrimPrefix(rest, "v")
		if d == n || ok && rest != "" && rest[0] >= '0' && rest[0] <= '9' {
			return true
		}
	}
	return false
}

// AppDirs are the folders Windows says the apps named are installed in
// (uninstall entries by DisplayName, App Paths by program) or run from
// now (running programs by name), each once, those found first first.
// None elsewhere than Windows.
func AppDirs(names, programs []string) []string {
	var dirs []string
	seen := map[string]bool{}
	add := func(d string) {
		if d == "" {
			return
		}
		k := strings.ToLower(filepath.Clean(d))
		if !seen[k] {
			seen[k] = true
			dirs = append(dirs, filepath.Clean(d))
		}
	}
	for _, a := range sources.installed() {
		if a.named(names) {
			add(a.Dir())
		}
	}
	for _, p := range programs {
		if exe := sources.appPath(p); exe != "" {
			add(filepath.Dir(exe))
		}
	}
	for _, exe := range sources.running(programs...) {
		add(filepath.Dir(exe))
	}
	return dirs
}

// sources are Windows' (tests replace them).
var sources = struct {
	installed func() []InstalledApp
	appPath   func(string) string
	running   func(...string) []string
}{installedApps, appPath, runningPrograms}

// RunningPrograms are the full paths of the processes running now whose
// program is one of names (case-insensitive, "App.exe"), each once; none
// elsewhere than Windows.
func RunningPrograms(names ...string) []string { return sources.running(names...) }

// UseAppSources has AppDirs read these instead of Windows' (tests: the
// installed programs' entries and the running programs' paths), until
// restore.
func UseAppSources(installed []InstalledApp, running []string) (restore func()) {
	was := sources
	sources.installed = func() []InstalledApp { return installed }
	sources.appPath = func(string) string { return "" }
	sources.running = func(names ...string) []string {
		var out []string
		for _, p := range running {
			for _, n := range names {
				if strings.EqualFold(filepath.Base(p), n) {
					out = append(out, p)
				}
			}
		}
		return out
	}
	return func() { sources = was }
}
