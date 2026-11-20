package machine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/samir2015dzh/aide/internal/legacy"
)

// A machine from before the rename keeps its database: it is renamed, with
// its write-ahead log, at the first start; a machine with a database of the
// new name keeps that one.
func TestKeepStoreFromBeforeTheRename(t *testing.T) {
	dir := t.TempDir()
	st, err := OpenStore(filepath.Join(dir, legacy.Daemon+".db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Set("kept", []byte("yes")); err != nil {
		t.Fatal(err)
	}
	st.Close()
	os.WriteFile(filepath.Join(dir, legacy.Daemon+".db-wal"), nil, 0o600)
	if err := keepStore(dir); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{legacy.Daemon + ".db", legacy.Daemon + ".db-wal"} {
		if _, err := os.Stat(filepath.Join(dir, f)); !os.IsNotExist(err) {
			t.Fatalf("%s left: %v", f, err)
		}
	}
	st, err = OpenStore(filepath.Join(dir, storeName))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if got, _ := st.Get("kept"); string(got) != "yes" {
		t.Fatalf("kept = %q after the rename, want what was set before it", got)
	}

	// Both there: the new one is kept, the old one left alone.
	os.WriteFile(filepath.Join(dir, legacy.Daemon+".db"), []byte("old"), 0o600)
	if err := keepStore(dir); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, legacy.Daemon+".db")); string(b) != "old" {
		t.Fatal("the old database was touched")
	}
}
