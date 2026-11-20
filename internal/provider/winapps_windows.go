package provider

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall`

// installedApps reads the uninstall entries of this user and of the
// machine (both views of the machine's: 64- and 32-bit installers).
func installedApps() []InstalledApp {
	var out []InstalledApp
	for _, root := range []struct {
		k      registry.Key
		access uint32
	}{
		{registry.CURRENT_USER, 0},
		{registry.LOCAL_MACHINE, registry.WOW64_64KEY},
		{registry.LOCAL_MACHINE, registry.WOW64_32KEY},
	} {
		k, err := registry.OpenKey(root.k, uninstallKey, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE|root.access)
		if err != nil {
			continue
		}
		names, _ := k.ReadSubKeyNames(-1)
		for _, n := range names {
			sk, err := registry.OpenKey(k, n, registry.QUERY_VALUE|root.access)
			if err != nil {
				continue
			}
			get := func(v string) string { s, _, _ := sk.GetStringValue(v); return s }
			out = append(out, InstalledApp{DisplayName: get("DisplayName"), InstallLocation: get("InstallLocation"),
				DisplayIcon: get("DisplayIcon"), UninstallString: get("UninstallString")})
			sk.Close()
		}
		k.Close()
	}
	return out
}

// appPath is the program's App Paths entry (this user's, else the
// machine's): where Windows starts "program" from the Run box.
func appPath(program string) string {
	for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
		k, err := registry.OpenKey(root, `Software\Microsoft\Windows\CurrentVersion\App Paths\`+program, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		v, _, err := k.GetStringValue("")
		k.Close()
		if v = strings.Trim(strings.TrimSpace(v), `"`); err == nil && v != "" {
			return v
		}
	}
	return ""
}

// runningPrograms are the full paths of the processes running now whose
// program is one of names (case-insensitive, "App.exe"), each once.
func runningPrograms(names ...string) []string {
	if len(names) == 0 {
		return nil
	}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	var out []string
	seen := map[string]bool{}
	e := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err := windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		exe := windows.UTF16ToString(e.ExeFile[:])
		match := false
		for _, n := range names {
			match = match || strings.EqualFold(exe, n)
		}
		if !match {
			continue
		}
		if p := processPath(e.ProcessID); p != "" && !seen[strings.ToLower(p)] {
			seen[strings.ToLower(p)] = true
			out = append(out, p)
		}
	}
	return out
}

func processPath(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}
