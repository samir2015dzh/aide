package provider

import (
	"path/filepath"
	"slices"
	"testing"
)

// An uninstall entry says where its app is by InstallLocation, else by the
// program of its icon; a version after the name is the same app.
func TestInstalledAppDir(t *testing.T) {
	for _, c := range []struct {
		a    InstalledApp
		want string
	}{
		{InstalledApp{InstallLocation: `D:\Apps\DSH Desktop\`}, filepath.Clean(`D:\Apps\DSH Desktop\`)},
		{InstalledApp{InstallLocation: `"D:\Apps\DSH"`}, filepath.Clean(`D:\Apps\DSH`)},
		{InstalledApp{DisplayIcon: `D:\Apps\DSH Desktop\DSH Desktop.exe,0`}, filepath.Dir(`D:\Apps\DSH Desktop\DSH Desktop.exe`)},
		{InstalledApp{DisplayIcon: `"D:\Apps\X\X.exe"`}, filepath.Dir(`D:\Apps\X\X.exe`)},
		{InstalledApp{DisplayIcon: `D:\Apps\X\icon.ico`}, ""},
		{InstalledApp{UninstallString: `D:\Apps\X\Uninstall.exe`}, ""},
	} {
		if got := c.a.Dir(); got != c.want {
			t.Errorf("%+v: %q, want %q", c.a, got, c.want)
		}
	}
	a := InstalledApp{DisplayName: "DSH Desktop 0.4.1"}
	if !a.named([]string{"DSH Desktop"}) || a.named([]string{"DSH"}) || !(InstalledApp{DisplayName: "DeepSeek Harness v0.2.2"}).named([]string{"DeepSeek Harness"}) ||
		(InstalledApp{DisplayName: "DSH Desktop Helper"}).named([]string{"DSH Desktop"}) {
		t.Fatal("named")
	}
}

// AppDirs: where the installed list says, then where the running app is, each once.
func TestAppDirs(t *testing.T) {
	defer UseAppSources([]InstalledApp{
		{DisplayName: "Other", InstallLocation: "/elsewhere"},
		{DisplayName: "DSH Desktop", InstallLocation: "/d/DSH Desktop"},
	}, []string{"/d/DSH Desktop/DSH Desktop.exe", "/e/portable/DSH Desktop.exe", "/f/other.exe"})()
	got := AppDirs([]string{"DSH Desktop"}, []string{"DSH Desktop.exe"})
	want := []string{filepath.Clean("/d/DSH Desktop"), filepath.Clean("/e/portable")}
	if !slices.Equal(got, want) {
		t.Fatalf("%q, want %q", got, want)
	}
}
