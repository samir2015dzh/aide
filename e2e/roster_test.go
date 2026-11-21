package e2e

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
)

type rosterJSON struct {
	Endpoints []struct {
		Address, Name, Kind, Tier, State, Machine, Doing string
		Mailable, You                                    bool
		Online                                           *bool
		AgentPaused                                      bool `json:"agent_paused"`
	}
	PausedAgents []struct{ Machine, Agent string } `json:"paused_agents"`
}

// TestRoster (docs/agent-messages.md §15): an agent reads, in one list,
// everyone and every session a message can go to: itself, connected, with
// when a message reaches it; the sessions on the machines it reaches, by
// <machine>/<session name>, with their state and what they are doing; the
// coding agents the owner paused. It writes to a session by that name, or
// by the session's name alone while only one has it, and the answer says
// what became of the message and when it is read. Two sessions of one name
// are told apart by -2; writing to many at once is refused.
func TestRoster(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	w := newOwnerMac(t, []string{"AIDE_FAKE_PROVIDERS=1"}, &localv1.GrantSpec{})
	ctx := context.Background()

	// A session of its own, titled.
	s, r := w.startSession(30*time.Second, "--title", "Fix CI", "--prompt", "say all green")
	if s.SessionID == "" {
		t.Fatalf("start: %+v", r)
	}
	var roster rosterJSON
	read := func() rosterJSON {
		t.Helper()
		var got rosterJSON
		if r := w.cli(30*time.Second, "roster", "-o", "json"); r.code != 0 || json.Unmarshal([]byte(r.stdout), &got) != nil {
			t.Fatalf("roster: %+v", r)
		}
		return got
	}
	eventually(t, 30*time.Second, "the agent itself, with its inbox, and the session listed", func() bool {
		roster = read()
		me, sess := false, false
		for _, e := range roster.Endpoints {
			me = me || (e.You && e.Kind == "cli" && e.Mailable && e.Online != nil && *e.Online && e.Tier == "pull")
			sess = sess || (e.Kind == "aide_session" && e.Name == "owner-mac/fix-ci" && e.Address == w.machineID()+"/"+s.SessionID && e.Machine == "owner-mac" && e.State != "" && e.Tier == "push")
		}
		return me && sess
	})

	// The owner pauses Claude Code there: the roster says so.
	if _, err := w.app.client.SetAgentPaused(ctx, &localv1.SetAgentPausedRequest{MachineId: w.machineID(), Agent: "claude", Paused: true}); err != nil {
		t.Fatal(err)
	}
	eventually(t, 10*time.Second, "claude listed paused", func() bool {
		roster = read()
		for _, p := range roster.PausedAgents {
			if p.Agent == "claude" && p.Machine == "owner-mac" {
				return true
			}
		}
		return false
	})
	if _, err := w.app.client.SetAgentPaused(ctx, &localv1.SetAgentPausedRequest{MachineId: w.machineID(), Agent: "claude"}); err != nil {
		t.Fatal(err)
	}

	type sent struct {
		Message struct {
			To       string
			Delivery []struct{ To, Result, Tier, Expect string }
		}
		Error struct{ Kind, Message string }
	}
	send := func(to string) (sent, result) {
		t.Helper()
		r := w.cli(30*time.Second, "message", "send", "--to", to, "--message", "say done", "-o", "json")
		var got sent
		json.Unmarshal([]byte(r.stdout), &got)
		return got, r
	}
	// By <machine>/<session name>, and by the name alone while it is one.
	for _, to := range []string{"owner-mac/fix-ci", "fix-ci"} {
		got, r := send(to)
		if r.code != 0 || got.Message.To != w.machineID()+"/"+s.SessionID || len(got.Message.Delivery) != 1 ||
			got.Message.Delivery[0].Result != "handed" || got.Message.Delivery[0].Tier != "push" || got.Message.Delivery[0].Expect == "" {
			t.Fatalf("to %q: %+v %+v", to, got, r)
		}
		w.waitStatus(s.SessionID, "idle")
	}

	// A second "Fix CI" on the machine: it is fix-ci-2, the first keeps
	// fix-ci, and each is written to by its own name. (One name on two
	// machines is the ambiguity refused: TestSessionByName.)
	s2, r := w.startSession(30*time.Second, "--title", "fix ci", "--prompt", "say hi")
	if s2.SessionID == "" {
		t.Fatalf("start: %+v", r)
	}
	eventually(t, 30*time.Second, "the second one named -2", func() bool {
		for _, e := range read().Endpoints {
			if e.Name == "owner-mac/fix-ci-2" && strings.HasSuffix(e.Address, "/"+s2.SessionID) {
				return true
			}
		}
		return false
	})
	w.waitStatus(s2.SessionID, "idle")
	for to, id := range map[string]string{"fix-ci": s.SessionID, "owner-mac/fix-ci-2": s2.SessionID} {
		if got, r := send(to); r.code != 0 || got.Message.To != w.machineID()+"/"+id {
			t.Fatalf("to %q: %+v %+v", to, got, r)
		}
		w.waitStatus(id, "idle")
	}

	// Writing to many at once: the owner's alone.
	for _, to := range []string{"*", "owner-mac/*", "repo:acme/site"} {
		if got, r := send(to); r.code == 0 || got.Error.Kind != "broadcast_owner_only" {
			t.Fatalf("to %q: %+v %+v", to, got, r)
		}
	}
}

// machineID is the owner Mac's machine id, as the app lists it.
func (w *ownerMac) machineID() string {
	w.t.Helper()
	res, err := w.app.client.ListMachines(context.Background(), &localv1.ListMachinesRequest{})
	if err != nil || len(res.Machines) == 0 {
		w.t.Fatalf("machines: %v %v", res, err)
	}
	return res.Machines[0].MachineId
}

// waitStatus waits for a session's status (session get's spelling).
func (w *ownerMac) waitStatus(id, status string) {
	w.t.Helper()
	eventually(w.t, 30*time.Second, "session "+id+" "+status, func() bool { return w.get(id).Status == status })
}
