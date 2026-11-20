package paths

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samir2015dzh/aide/internal/legacy"
)

// Two machines of one user never share a control or tools endpoint,
// however long their data directories.
func TestMachineEndpointsArePerDataDir(t *testing.T) {
	long := filepath.Join(t.TempDir(), strings.Repeat("x", 120))
	a, b := filepath.Join(long, "a"), filepath.Join(long, "b")
	for _, f := range []func(string) string{Control, func(d string) string { return Tools(d, "m_"+filepath.Base(d)+"0123456789abcdef") }} {
		if f(a) == f(b) {
			t.Fatalf("%s is shared", f(a))
		}
	}
}

// A machine's endpoint for its own user's agents is named by the machine
// alone, short enough for a Unix socket, movable, and none when turned off.
func TestLocalEndpoint(t *testing.T) {
	const a, b = "m_abcdefghijklmnopqrstuvwx", "m_zyxwvutsrqponmlkjihgfedc"
	t.Setenv("AIDE_LOCAL", "")
	t.Setenv("AIDE_LOCAL_DIR", "")
	if Local(a) == Local(b) || Local(a) != Local(a) || !strings.Contains(Local(a), a) || len(Local(a)) > 100 {
		t.Fatalf("%s, %s", Local(a), Local(b))
	}
	if Local("") != "" {
		t.Fatal("an endpoint for no machine")
	}
	t.Setenv("AIDE_LOCAL_DIR", "/tmp/elsewhere")
	if got := Local(a); filepath.Dir(got) != filepath.FromSlash("/tmp/elsewhere") {
		t.Fatal(got)
	}
	t.Setenv("AIDE_LOCAL", "off")
	if got := Local(a); got != "" {
		t.Fatalf("%s while off", got)
	}
}

// TestKept: the data directory of an installation from before the rename is
// kept while it holds an identity and the new one holds none, however the
// new one came to be (2026-11-20: the service's definition made it before
// the first start); a new computer uses the new one.
func TestKept(t *testing.T) {
	write := func(t *testing.T, f string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("old identity, new made by the service's definition", func(t *testing.T) {
		r := t.TempDir()
		write(t, filepath.Join(r, legacy.DataDirTitle, "owner", "current"))
		write(t, filepath.Join(r, "Aide", "service", "aided.xml"))
		if got := kept(filepath.Join(r, "Aide"), dataIdentity); got != filepath.Join(r, legacy.DataDirTitle) {
			t.Fatalf("kept = %s, want the old directory", got)
		}
	})
	t.Run("new computer", func(t *testing.T) {
		r := t.TempDir()
		if got := kept(filepath.Join(r, "Aide"), dataIdentity); got != filepath.Join(r, "Aide") {
			t.Fatalf("kept = %s", got)
		}
		write(t, filepath.Join(r, "Aide", "service", "aided.xml"))
		if got := kept(filepath.Join(r, "Aide"), dataIdentity); got != filepath.Join(r, "Aide") {
			t.Fatalf("kept = %s", got)
		}
	})
	t.Run("new identity wins", func(t *testing.T) {
		r := t.TempDir()
		write(t, filepath.Join(r, legacy.DataDir, "owner", "current"))
		write(t, filepath.Join(r, "aide", "machine", "keys", "machine.key"))
		if got := kept(filepath.Join(r, "aide"), dataIdentity); got != filepath.Join(r, "aide") {
			t.Fatalf("kept = %s", got)
		}
	})
	t.Run("machine, or owner from before profiles", func(t *testing.T) {
		for _, f := range []string{filepath.Join("machine", "keys", "machine.key"), filepath.Join("owner", "owner.db")} {
			r := t.TempDir()
			write(t, filepath.Join(r, legacy.DataDir, f))
			write(t, filepath.Join(r, "aide", "logs", "setup.log"))
			if got := kept(filepath.Join(r, "aide"), dataIdentity); got != filepath.Join(r, legacy.DataDir) {
				t.Fatalf("%s: kept = %s", f, got)
			}
		}
	})
	t.Run("old without identity only while no new", func(t *testing.T) {
		r := t.TempDir()
		write(t, filepath.Join(r, legacy.DataDirTitle, "logs", "aided.log"))
		if got := kept(filepath.Join(r, "Aide"), dataIdentity); got != filepath.Join(r, legacy.DataDirTitle) {
			t.Fatalf("kept = %s", got)
		}
		os.Mkdir(filepath.Join(r, "Aide"), 0o700)
		if got := kept(filepath.Join(r, "Aide"), dataIdentity); got != filepath.Join(r, "Aide") {
			t.Fatalf("kept = %s", got)
		}
	})
	t.Run("CLI home", func(t *testing.T) {
		r := t.TempDir()
		write(t, filepath.Join(r, legacy.DataDir, "client.key"))
		os.MkdirAll(filepath.Join(r, "aide"), 0o700)
		if got := kept(filepath.Join(r, "aide"), cliIdentity); got != filepath.Join(r, legacy.DataDir) {
			t.Fatalf("kept = %s", got)
		}
	})
}
