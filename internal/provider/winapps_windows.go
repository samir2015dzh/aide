package provider

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// RunningExecutables are the full paths of the programs named name (its
// file name, without case) running now, as far as this user may ask
// (another user's and protected processes are left out).
func RunningExecutables(name string) []string {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	var paths []string
	e := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err := windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if !strings.EqualFold(windows.UTF16ToString(e.ExeFile[:]), name) {
			continue
		}
		if p := imagePath(e.ProcessID); p != "" {
			paths = append(paths, p)
		}
	}
	return paths
}

func imagePath(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if windows.QueryFullProcessImageName(h, 0, &buf[0], &n) != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}

// The registry keys read: this user's, then the machine's (64- and 32-bit).
// Tests point them at keys of their own.
var (
	appPathsKeys = []regKey{
		{registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\App Paths`},
		{registry.LOCAL_MACHINE, `Software\Microsoft\Windows\CurrentVersion\App Paths`},
	}
	uninstallKeys = []regKey{
		{registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.LOCAL_MACHINE, `Software\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.LOCAL_MACHINE, `Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`},
	}
)

type regKey struct {
	root registry.Key
	path string
}

// appPath is exe's App Paths entry: the program Windows starts for it.
func appPath(exe string) string {
	for _, k := range appPathsKeys {
		key, err := registry.OpenKey(k.root, k.path+`\`+exe, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		v, _, err := key.GetStringValue("")
		key.Close()
		if err == nil && v != "" {
			return commandPath(expand(v))
		}
	}
	return ""
}

func uninstallEntries() []uninstallEntry {
	var entries []uninstallEntry
	for _, k := range uninstallKeys {
		key, err := registry.OpenKey(k.root, k.path, registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			continue
		}
		names, _ := key.ReadSubKeyNames(-1)
		key.Close()
		for _, n := range names {
			sub, err := registry.OpenKey(k.root, k.path+`\`+n, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			get := func(v string) string { s, _, _ := sub.GetStringValue(v); return expand(s) }
			entries = append(entries, uninstallEntry{name: get("DisplayName"), location: get("InstallLocation"), icon: get("DisplayIcon"), uninstall: get("UninstallString")})
			sub.Close()
		}
	}
	return entries
}

// expand fills in %VARIABLES% (values stored as REG_EXPAND_SZ come back
// unexpanded).
func expand(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	if x, err := registry.ExpandString(s); err == nil {
		return x
	}
	return s
}

// appxPackagesKey lists the Store (MSIX) packages registered for this
// user, readable without PowerShell or access to WindowsApps.
var appxPackagesKey = regKey{registry.CURRENT_USER, `Software\Classes\Local Settings\Software\Microsoft\Windows\CurrentVersion\AppModel\Repository\Packages`}

// WindowsStorePackages are the full names of the Store (MSIX) packages
// registered for this user: name_version_architecture_resourceid_publisher.
func WindowsStorePackages() []string {
	k, err := registry.OpenKey(appxPackagesKey.root, appxPackagesKey.path, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil
	}
	defer k.Close()
	names, _ := k.ReadSubKeyNames(-1)
	return names
}

// registryRoots are where electron-builder registers an app: this user's,
// then the machine's. Tests point them at keys of their own.
var registryRoots = []regKey{{registry.CURRENT_USER, `Software`}, {registry.LOCAL_MACHINE, `Software`}}

// registeredLocation is the InstallLocation electron-builder's installer
// registered the app with guid under (Software\<guid>), "" for none.
func registeredLocation(guid string) string {
	for _, r := range registryRoots {
		k, err := registry.OpenKey(r.root, r.path+`\`+guid, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		v, _, _ := k.GetStringValue("InstallLocation")
		k.Close()
		if v = strings.Trim(strings.TrimSpace(expand(v)), `"`); v != "" {
			return v
		}
	}
	return ""
}
