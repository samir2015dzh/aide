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
	idle := n.Load()
	// Writing: the wait it had backed off to runs out, then it is back at
	// the interval (counted after that warm-up, while writes go on).
	write := func(d time.Duration) {
		for stop := time.Now().Add(d); time.Now().Before(stop); time.Sleep(2 * time.Millisecond) {
			if _, err := db.Exec("INSERT INTO t VALUES (1)"); err != nil {
				t.Fatal(err)
			}
		}
	}
	write(400 * time.Millisecond)
	before := n.Load()
	write(600 * time.Millisecond)
	if busy := n.Load() - before; busy < 15 || busy < 2*idle {
		t.Fatalf("%d checkpoints in 600 ms of writes (%d in 600 ms of nothing)", busy, idle)
	}
}
