package provider

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// The folders an app is installed in, as Windows records them: its
// uninstall entry (InstallLocation, DisplayIcon, UninstallString, with
// %VARIABLES%), its App Paths entry, and the program running now. The
// test's own keys stand in for Windows' lists.
func TestWindowsAppDirs(t *testing.T) {
	base := `Software\AideTest\` + filepath.Base(t.TempDir())
	t.Cleanup(func() { deleteTree(registry.CURRENT_USER, base) })
	set := func(path string, values map[string]string) {
		t.Helper()
		k, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.ALL_ACCESS)
		if err != nil {
			t.Fatal(err)
		}
		defer k.Close()
		for n, v := range values {
			if strings.Contains(v, "%") {
				err = k.SetExpandStringValue(n, v)
			} else {
				err = k.SetStringValue(n, v)
			}
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	wasU, wasA := uninstallKeys, appPathsKeys
	t.Cleanup(func() { uninstallKeys, appPathsKeys = wasU, wasA })
	uninstallKeys = []regKey{{registry.CURRENT_USER, base + `\Uninstall`}}
	appPathsKeys = []regKey{{registry.CURRENT_USER, base + `\App Paths`}}

	t.Setenv("AIDETESTDIR", `D:\Apps`)
	set(base+`\Uninstall\{x}`, map[string]string{"DisplayName": "Fake Harness 0.3.0", "InstallLocation": `E:\Fake Harness`})
	set(base+`\Uninstall\icon`, map[string]string{"DisplayName": "fake harness (beta)", "DisplayIcon": `"%AIDETESTDIR%\Beta\Fake Harness.exe",0`})
	set(base+`\Uninstall\other`, map[string]string{"DisplayName": "Another App", "InstallLocation": `F:\Another`})
	set(base+`\Uninstall\nsis`, map[string]string{"DisplayName": "Fake Desktop", "UninstallString": `C:\Users\u\AppData\Local\Programs\fake-desktop\Uninstall Fake Desktop.exe /currentuser`})
	set(base+`\App Paths\Fake Harness.exe`, map[string]string{"": `G:\Portable\Fake Harness.exe`})

	got := WindowsAppDirs(WindowsApp{Names: []string{"Fake Harness", "Fake Desktop"}, Exes: []string{"Fake Harness.exe"}})
	// App Paths first; the uninstall entries in the order Windows lists them.
	if len(got) == 0 || got[0] != `G:\Portable` {
		t.Fatalf("got %q, want App Paths' folder first", got)
	}
	want := []string{`C:\Users\u\AppData\Local\Programs\fake-desktop`, `D:\Apps\Beta`, `E:\Fake Harness`, `G:\Portable`}
	slices.Sort(got)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q,\nwant %q", got, want)
	}

	// The program running now: this test's own.
	self, _ := os.Executable()
	running := WindowsAppDirs(WindowsApp{Exes: []string{filepath.Base(self)}})
	if len(running) == 0 || !strings.EqualFold(running[0], filepath.Dir(self)) {
		t.Fatalf("running: got %q, want %s first", running, filepath.Dir(self))
	}
}

func deleteTree(root registry.Key, path string) {
	if k, err := registry.OpenKey(root, path, registry.ENUMERATE_SUB_KEYS); err == nil {
		names, _ := k.ReadSubKeyNames(-1)
		k.Close()
		for _, n := range names {
			deleteTree(root, path+`\`+n)
		}
	}
	registry.DeleteKey(root, path)
}
