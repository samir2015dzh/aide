package e2e

import (
	"encoding/json"
	"testing"
	"time"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
)

// TestMessageChains (docs/agent-messages.md §15): a send retried with the
// same idempotency key is one message; a reply marks what it answers
// replied, for its sender; a chain that comes round to one endpoint a
// fourth time within the hour is held for the owner; a message for
// information only starts no turn, and the next one's turn runs as ever.
func TestMessageChains(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	w := newOwnerMac(t, []string{"AIDE_FAKE_PROVIDERS=1"}, &localv1.GrantSpec{})

	var me string
	eventually(t, 30*time.Second, "this agent's own inbox", func() bool {
		var c struct {
			Contacts []struct {
				Address  string
				You      bool
				HasInbox bool `json:"has_inbox"`
			}
		}
		r := w.cli(30*time.Second, "message", "contacts", "-o", "json")
		json.Unmarshal([]byte(r.stdout), &c)
		for _, x := range c.Contacts {
			if x.You && x.HasInbox {
				me = x.Address
			}
		}
		return me != ""
	})
	type sent struct {
		Message struct {
			ID, Delivered string
			Delivery      []struct{ Result string }
		}
	}
	send := func(args ...string) sent {
		t.Helper()
		r := w.cli(30*time.Second, append([]string{"message", "send", "-o", "json"}, args...)...)
		var s sent
		if r.code != 0 || json.Unmarshal([]byte(r.stdout), &s) != nil || s.Message.ID == "" {
			t.Fatalf("send %v: %+v", args, r)
		}
		return s
	}
	list := func() []string {
		t.Helper()
		r := w.cli(30*time.Second, "message", "list", "-o", "json")
		var l struct{ Messages []struct{ ID string } }
		if r.code != 0 || json.Unmarshal([]byte(r.stdout), &l) != nil {
			t.Fatalf("list: %+v", r)
		}
		var ids []string
		for _, m := range l.Messages {
			ids = append(ids, m.ID)
		}
		return ids
	}
	state := func(id string) string {
		t.Helper()
		r := w.cli(30*time.Second, "message", "status", id, "-o", "json")
		var st struct{ States []struct{ ID, State string } }
		if json.Unmarshal([]byte(r.stdout), &st) != nil || len(st.States) != 1 {
			t.Fatalf("status: %+v", r)
		}
		return st.States[0].State
	}
	took := func(id string) {
		t.Helper()
		eventually(t, 20*time.Second, "message "+id+" in the inbox", func() bool {
			for _, x := range list() {
				if x == id {
					return true
				}
			}
			return false
		})
		w.ack(id)
	}

	// A retry with the same key is the same message.
	a := send("--to", me, "--message", "ping", "--idempotency-key", "k1")
	if b := send("--to", me, "--message", "ping", "--idempotency-key", "k1"); b.Message.ID != a.Message.ID {
		t.Fatalf("a retry is another message: %s %s", a.Message.ID, b.Message.ID)
	}
	took(a.Message.ID)

	// The reply: the first is replied, for its sender.
	b := send("--to", me, "--message", "pong", "--reply-to", a.Message.ID)
	took(b.Message.ID)
	if st := state(a.Message.ID); st != "replied" {
		t.Fatalf("state of the message replied to: %s", st)
	}

	// The chain comes round to this inbox a third time, then a fourth:
	// held for the owner.
	c := send("--to", me, "--message", "again", "--reply-to", b.Message.ID)
	took(c.Message.ID)
	d := send("--to", me, "--message", "and again", "--reply-to", c.Message.ID)
	eventually(t, 20*time.Second, "the fourth pass held", func() bool {
		for _, x := range list() {
			if x == d.Message.ID {
				t.Fatalf("the fourth pass of a chain was handed over")
			}
		}
		return state(d.Message.ID) == "held"
	})

	// For information only: to an idle session, it waits; the next
	// message's turn runs.
	s, r := w.startSession(30*time.Second, "--prompt", "say hi")
	if s.SessionID == "" {
		t.Fatalf("start: %+v", r)
	}
	w.waitStatus(s.SessionID, "idle")
	if f := send("--to", s.SessionID, "--message", "the build moved to ci-2", "--fyi"); f.Message.Delivered != "queued" {
		t.Fatalf("fyi: %+v", f)
	}
	time.Sleep(time.Second)
	if st := w.get(s.SessionID).Status; st != "idle" {
		t.Fatalf("an fyi started a turn: %s", st)
	}
	if n := send("--to", s.SessionID, "--message", "say done"); n.Message.Delivered != "turn" || n.Message.Delivery[0].Result != "handed" {
		t.Fatalf("the next message: %+v", n)
	}
	w.waitStatus(s.SessionID, "idle")
}
