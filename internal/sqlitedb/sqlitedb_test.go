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
	// 25 ms: well above Windows timer resolution (~15.6 ms), which at a
	// smaller interval blurs writing and idle together.
	c, err := StartCheckpointer(path, db, 25*time.Millisecond, func(time.Duration) { n.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	time.Sleep(time.Second)
	// At 25 ms it would be ~40; backing off (25, 50, 100, … ms) it is ~5.
	if idle := n.Load(); idle > 10 {
		t.Fatalf("%d checkpoints in 1 s of nothing written", idle)
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
	write(time.Second) // longer than the 800 ms it had backed off to
	before := n.Load()
	write(time.Second)
	if busy := n.Load() - before; busy < 20 || busy < 2*idle {
		t.Fatalf("%d checkpoints in 1 s of writes (%d in 1 s of nothing)", busy, idle)
	}
}
