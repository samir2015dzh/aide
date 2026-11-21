package sqlitedb

import (
	"path/filepath"
	"slices"
	"sync"
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
	var mu sync.Mutex
	var at []time.Time // when each checkpoint ran
	since := func(i int) []time.Time {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(at[i:])
	}
	// 25 ms: well above Windows timer resolution (~15.6 ms), which at a
	// smaller interval blurs writing and idle together.
	c, err := StartCheckpointer(path, db, 25*time.Millisecond, func(time.Duration) {
		mu.Lock()
		at = append(at, time.Now())
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	time.Sleep(time.Second)
	// At 25 ms it would be ~40; backing off (25, 50, 100, … ms) it is ~5.
	idle := len(since(0))
	if idle > 10 {
		t.Fatalf("%d checkpoints in 1 s of nothing written", idle)
	}
	// Writing: the wait it had backed off to runs out, then it is back at
	// the interval (counted after that warm-up, while writes go on).
	write := func(d time.Duration) {
		for stop := time.Now().Add(d); time.Now().Before(stop); time.Sleep(2 * time.Millisecond) {
			if _, err := db.Exec("INSERT INTO t VALUES (1)"); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Warm-up: until two checkpoints ran after the writes began (the first
	// ends the backed-off wait, however long the timers made it), not for
	// a fixed time: on a slow runner the wait can outlast one.
	started := len(since(0))
	for deadline := time.Now().Add(10 * time.Second); len(since(started)) < 2; {
		if time.Now().After(deadline) {
			t.Fatalf("no checkpoint in 10 s of writes after %d idle", idle)
		}
		write(50 * time.Millisecond)
	}
	before := len(since(0))
	write(time.Second)
	// The gap between checkpoints, not their count: a slow runner (Windows:
	// the timer rounds 25 ms up, a checkpoint takes longer) runs fewer in a
	// second, but still about one an interval apart, where backed off they
	// are 800 ms apart or more.
	busy := since(before)
	var gaps []time.Duration
	for i := 1; i < len(busy); i++ {
		gaps = append(gaps, busy[i].Sub(busy[i-1]))
	}
	slices.Sort(gaps)
	if len(gaps) < 4 || gaps[len(gaps)/2] > 200*time.Millisecond {
		t.Fatalf("%d checkpoints in 1 s of writes, gaps %v (%d in 1 s of nothing)", len(busy), gaps, idle)
	}
}
