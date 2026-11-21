package msgchain

import (
	"strings"
	"testing"
	"time"
)

// A chain starts at hop 1; a message written because of another goes on
// with its trace, one hop more.
func TestNext(t *testing.T) {
	tr, hop := New()
	if !strings.HasPrefix(tr, "tr_") || hop != 1 {
		t.Fatalf("New: %q %d", tr, hop)
	}
	if t2, h2 := Next(tr, hop); t2 != tr || h2 != 2 {
		t.Fatalf("Next: %q %d", t2, h2)
	}
	if t3, h3 := Next("", 7); t3 == "" || h3 != 1 {
		t.Fatalf("a parent from before chains starts one: %q %d", t3, h3)
	}
}

// A recipient holds a chain beyond 4 hops, and one that reaches the same
// endpoint a fourth time within the hour; other endpoints, and the next
// hour, are counted on their own.
func TestGuard(t *testing.T) {
	var g Guard
	t0 := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	if why := g.Check("m/s_1", "tr_a", 5, t0); why == "" {
		t.Fatal("hop 5 taken")
	}
	for i := range 3 {
		if why := g.Check("m/s_1", "tr_a", 2, t0.Add(time.Duration(i)*time.Minute)); why != "" {
			t.Fatalf("pass %d held: %s", i+1, why)
		}
	}
	if why := g.Check("m/s_1", "tr_a", 2, t0.Add(10*time.Minute)); why == "" {
		t.Fatal("a fourth pass within the hour taken")
	}
	if why := g.Check("m/s_2", "tr_a", 2, t0.Add(10*time.Minute)); why != "" {
		t.Fatalf("another endpoint held: %s", why)
	}
	if why := g.Check("m/s_1", "tr_a", 2, t0.Add(2*time.Hour)); why != "" {
		t.Fatalf("the next hour held: %s", why)
	}
	if why := g.Check("m/s_1", "", 1, t0); why != "" {
		t.Fatalf("a message from before chains held: %s", why)
	}
}
