package machine

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
)

// Each session keeps its own port block across reopening the store; an
// archived session's block goes to the next session that needs one.
func TestSessionPorts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aided.db")
	st, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	ports := func(id string) int {
		t.Helper()
		b, err := st.SessionPorts(id)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	a, b, c := ports("a"), ports("b"), ports("c")
	if a != portFirst || b != a+portBlock || c != b+portBlock {
		t.Fatalf("blocks %d %d %d", a, b, c)
	}
	if ports("a") != a {
		t.Fatal("a session's block changed")
	}
	if err := st.ReleasePorts("b"); err != nil {
		t.Fatal(err)
	}
	if d := ports("d"); d != b {
		t.Fatalf("released block not reused: %d, want %d", d, b)
	}
	st.Close()
	if st, err = OpenStore(path); err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if ports("a") != a || ports("c") != c || ports("d") != b {
		t.Fatal("blocks changed after reopening the store")
	}
	if e := ports("e"); e != c+portBlock {
		t.Fatalf("next block %d", e)
	}
	for i := 4; i < portBlocks; i++ {
		ports(fmt.Sprint("s", i))
	}
	if _, err := st.SessionPorts("one-too-many"); !errors.Is(err, errNoPorts) {
		t.Fatalf("all blocks taken: %v", err)
	}
	if last := ports(fmt.Sprint("s", portBlocks-1)); last+portBlock-1 >= 32768 {
		t.Fatalf("last block %d reaches the ephemeral range", last)
	}
}
