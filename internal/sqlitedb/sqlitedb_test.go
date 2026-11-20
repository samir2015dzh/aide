package sqlitedb

import (
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// The checkpointer looks less often while nothing is written, and at its
// interval again once something is.
func TestCheckpointerBacksOffWhileIdle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	db, err := Open(path, []string{"CREATE TABLE t (v INTEGER)"})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n atomic.Int64
	c, err := StartCheckpointer(path, db, 5*time.Millisecond, func(time.Duration) { n.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	time.Sleep(600 * time.Millisecond)
	// At 5 ms it would be ~120; backing off (5, 10, 20, … ms) it is under 10.
	if idle := n.Load(); idle > 12 {
		t.Fatalf("%d checkpoints in 600 ms of nothing written", idle)
	}
	// Writing: back at the interval.
	before := n.Load()
	stop := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(stop) {
		if _, err := db.Exec("INSERT INTO t VALUES (1)"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	if busy := n.Load() - before; busy < 15 {
		t.Fatalf("%d checkpoints in 300 ms of writes", busy)
	}
}
