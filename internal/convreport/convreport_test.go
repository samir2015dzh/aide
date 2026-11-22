package convreport

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

// A conversation the hooks reported has its address under the agent that
// reported it; one not reported, none. Logged in again as another agent,
// the home forgets the old one's; a conversation not started for 90 days is
// forgotten.
func TestRecord(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	if a := Load(home).Address("conv-1"); a != "" {
		t.Fatalf("nothing reported: %q", a)
	}
	if err := Record(home, "c_desk", "conv-1", now.Add(-100*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := Record(home, "c_desk", "conv-2", now); err != nil {
		t.Fatal(err)
	}
	r := Load(home)
	if a := r.Address("conv-2"); a != "c_desk/conv-2" {
		t.Fatalf("reported: %q", a)
	}
	if a := r.Address("conv-1"); a != "" {
		t.Fatalf("not started for 100 days: %q", a)
	}
	if a := r.Address("conv-3"); a != "" {
		t.Fatalf("never reported: %q", a)
	}
	if err := Record(home, "c_other", "conv-3", now); err != nil {
		t.Fatal(err)
	}
	if r := Load(home); r.Address("conv-2") != "" || r.Address("conv-3") != "c_other/conv-3" {
		t.Fatalf("logged in as another: %+v", r)
	}
	full := Report{Agent: "c_other", Conversations: map[string]time.Time{}}
	for i := range most + 2 {
		full.Conversations[fmt.Sprintf("k%d", i)] = now.Add(time.Duration(i) * time.Second)
	}
	b, _ := json.Marshal(full)
	os.WriteFile(file(home), b, 0o600)
	Record(home, "c_other", fmt.Sprintf("k%d", most+2), now.Add(time.Hour))
	if r := Load(home); len(r.Conversations) != most || r.Address("k0") != "" || r.Address(fmt.Sprintf("k%d", most+2)) == "" {
		t.Fatalf("kept %d, the oldest %q", len(r.Conversations), r.Address("k0"))
	}
}
