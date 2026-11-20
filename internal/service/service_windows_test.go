package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samir2015dzh/aide/internal/flock"
	"github.com/samir2015dzh/aide/internal/legacy"
	"github.com/samir2015dzh/aide/internal/paths"
)

// The task runs the windowless daemon with its arguments quoted for the
// command line, and its file reads back as written.
func TestTaskXML(t *testing.T) {
	x := taskXML(`C:\Users\A B\AppData\Local\Programs\Aide\bin\aidedw.exe`, []string{"run", "--supervise", "--data-dir", `C:\Users\A B\AppData\Local\Aide`}, "S-1-5-21-1")
	for _, want := range []string{
		`<Command>C:\Users\A B\AppData\Local\Programs\Aide\bin\aidedw.exe</Command>`,
		`<Arguments>run --supervise --data-dir "C:\Users\A B\AppData\Local\Aide"</Arguments>`,
		`<UserId>S-1-5-21-1</UserId>`,
		`<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>`,
	} {
		if !strings.Contains(x, want) {
			t.Errorf("no %s in\n%s", want, x)
		}
	}
	if got := fromUTF16(utf16File(x)); got != x {
		t.Fatalf("read back as %q", got)
	}
}

// A second supervisor of a data directory leaves at once; the only one
// ends when the daemon stops cleanly.
func TestSuperviseOnce(t *testing.T) {
	dir := t.TempDir()
	supervise := func(code string) error {
		done := make(chan error, 1)
		go func() { done <- Supervise(dir, "cmd.exe", []string{"/c", "exit", code}) }()
		select {
		case err := <-done:
			return err
		case <-time.After(30 * time.Second):
			t.Fatal("still supervising")
			return nil
		}
	}
	lock, err := flock.Acquire(filepath.Join(dir, "run", "supervisor"))
	if err != nil {
		t.Fatal(err)
	}
	if err := supervise("1"); err == nil {
		t.Fatal("a second supervisor ran")
	}
	lock.Release()
	if err := supervise("0"); err != nil {
		t.Fatal(err)
	}
}

// An installation from before the rename, after the first build that came
// after it (2026-11-20, installed on a computer): that build's task
// definition made %LocalAppData%\Aide before the first start. The data
// directory is still the old one, whose identity it holds; the task's
// definition goes in it, and the folder that only held the stray one goes.
// On a new computer everything is in %LocalAppData%\Aide.
func TestDefinitionFollowsTheDataDirectory(t *testing.T) {
	write := func(f string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("AIDE_DATA_DIR", "")
	t.Setenv(legacy.EnvPrefix+"DATA_DIR", "")

	local := t.TempDir()
	t.Setenv("LOCALAPPDATA", local)
	write(filepath.Join(local, legacy.DataDirTitle, "owner", "current"))
	write(filepath.Join(local, "Aide", "service", "aided.xml"))
	dataDir := paths.DataDir()
	if want := filepath.Join(local, legacy.DataDirTitle); dataDir != want {
		t.Fatalf("data directory %s, want %s", dataDir, want)
	}
	if want := filepath.Join(local, legacy.DataDirTitle, "service", "aided.xml"); definition(dataDir) != want {
		t.Fatalf("definition %s, want %s", definition(dataDir), want)
	}
	stray(dataDir)
	if _, err := os.Stat(filepath.Join(local, "Aide")); !os.IsNotExist(err) {
		t.Fatalf("%%LocalAppData%%\\Aide left: %v", err)
	}
	if paths.DataDir() != dataDir {
		t.Fatal("the data directory changed")
	}

	local = t.TempDir()
	t.Setenv("LOCALAPPDATA", local)
	dataDir = paths.DataDir()
	if want := filepath.Join(local, "Aide"); dataDir != want {
		t.Fatalf("new computer: data directory %s, want %s", dataDir, want)
	}
	write(definition(dataDir))
	stray(dataDir)
	if _, err := os.Stat(definition(dataDir)); err != nil {
		t.Fatalf("new computer: the definition went: %v", err)
	}
	if paths.DataDir() != dataDir {
		t.Fatal("new computer: the data directory changed")
	}
}
