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

// Any agent's program is found where Windows says, the same way for each:
// by an installed program whose name contains its command (or begins with
// one of its names), in that program's folder or its bin; and the program
// of that name running now.
func TestWindowsProgram(t *testing.T) {
	base := `Software\AideTest\` + filepath.Base(t.TempDir())
	t.Cleanup(func() { deleteTree(registry.CURRENT_USER, base) })
	wasU, wasA := uninstallKeys, appPathsKeys
	t.Cleanup(func() { uninstallKeys, appPathsKeys = wasU, wasA })
	uninstallKeys = []regKey{{registry.CURRENT_USER, base + `\Uninstall`}}
	appPathsKeys = []regKey{{registry.CURRENT_USER, base + `\App Paths`}}
	entry := func(name, dir string) {
		t.Helper()
		k, _, err := registry.CreateKey(registry.CURRENT_USER, base+`\Uninstall\`+name, registry.ALL_ACCESS)
		if err != nil {
			t.Fatal(err)
		}
		k.SetStringValue("DisplayName", name)
		k.SetStringValue("InstallLocation", dir)
		k.Close()
	}
	exe := func(p string) string {
		t.Helper()
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, nil, 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}

	cli := filepath.Join(t.TempDir(), "FakeCode")
	want := exe(filepath.Join(cli, "bin", "fakecode.exe"))
	entry("FakeCode 1.4 (user)", cli)
	if got := WindowsProgram("fakecode"); !strings.EqualFold(got, want) {
		t.Fatalf("by a name containing the command: got %q, want %q", got, want)
	}

	app := filepath.Join(t.TempDir(), "Work")
	want = exe(filepath.Join(app, "fakebuddy.exe"))
	entry("FakeWork Buddy", app)
	if got := WindowsProgram("fakebuddy"); got != "" {
		t.Fatalf("found without the name: %q", got)
	}
	if got := WindowsProgram("fakebuddy", "FakeWork"); !strings.EqualFold(got, want) {
		t.Fatalf("by one of its names: got %q, want %q", got, want)
	}

	if got := WindowsProgram("aide-no-such-program"); got != "" {
		t.Fatalf("a program Windows knows nothing of: %q", got)
	}
	self, _ := os.Executable()
	if got := WindowsProgram(strings.TrimSuffix(filepath.Base(self), ".exe")); !strings.EqualFold(got, self) {
		t.Fatalf("running: got %q, want %q", got, self)
	}
}
