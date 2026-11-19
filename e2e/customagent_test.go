package e2e

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	localv1 "github.com/hermeshai/hermesh/gen/go/hermesh/local/v1"
	machinev1 "github.com/hermeshai/hermesh/gen/go/hermesh/machine/v1"
	sessionv1 "github.com/hermeshai/hermesh/gen/go/hermesh/session/v1"
)

// TestCustomAgent: the owner connects an ACP agent Hermesh has not built
// in, as the desktop app's Connect page does: tests the command (a wrong
// one says why), saves it with a key among its variables, finds it in the
// machine's agent list, starts a session with it from the app and from an
// agent; it keeps working after hermeshd restarts, with its key, which
// never was in agents.json; removed, it is gone. The same on a machine of
// the owner's that is another computer, through its tunnel.
func TestCustomAgent(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	w := newOwnerMac(t, nil, &localv1.GrantSpec{})
	ctx := context.Background()
	fake := filepath.Join(w.bin, "fakeacp"+exe)
	state := t.TempDir()
	st, err := w.app.client.GetStatus(ctx, &localv1.GetStatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	mid := st.LocalMachineId

	// A command that is not there, and one that is no ACP agent.
	test := func(machine string, a *machinev1.CustomAgent) *machinev1.TestCustomAgentResponse {
		t.Helper()
		res, err := w.app.client.TestMachineCustomAgent(ctx, &localv1.TestMachineCustomAgentRequest{MachineId: machine, Agent: a})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := test(mid, &machinev1.CustomAgent{Name: "mine", Command: "hermesh-no-such-agent"}); res.Ok || !strings.Contains(res.Reason, "hermesh-no-such-agent not found") {
		t.Fatalf("missing command: %+v", res)
	}
	if runtime.GOOS != "windows" {
		res := test(mid, &machinev1.CustomAgent{Name: "mine", Command: "/bin/sh", Args: []string{"-c", "echo 'error: unknown flag --acp' >&2; exit 3"}})
		if res.Ok || !strings.Contains(res.Reason, "initialize: it ended before answering") || !strings.Contains(res.Reason, "unknown flag --acp") {
			t.Fatalf("not an ACP agent: %+v", res)
		}
	}
	def := &machinev1.CustomAgent{Name: "mine", Command: fake, Args: []string{"acp"}, Env: []*machinev1.AgentEnv{
		{Name: "FAKE_ACP_STATE", Value: state}, {Name: "MINE_API_KEY", Value: "k-123"}}}
	if res := test(mid, def); !res.Ok || res.AgentTitle != "Fake ACP" || res.AgentVersion != "1.2.3" {
		t.Fatalf("test: %+v", res)
	}

	if _, err := w.app.client.SaveMachineCustomAgent(ctx, &localv1.SaveMachineCustomAgentRequest{MachineId: mid, Agent: def}); err != nil {
		t.Fatal(err)
	}
	listed := func(machine string) *machinev1.AgentStatus {
		res, err := w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
		if err != nil {
			return nil
		}
		for _, m := range res.Machines {
			for _, a := range m.GetStatus().GetAgents() {
				if m.MachineId == machine && a.Name == "mine" {
					return a
				}
			}
		}
		return nil
	}
	eventually(t, 30*time.Second, "the agent in the machine's list, checked", func() bool {
		a := listed(mid)
		return a != nil && a.Custom && a.Available && a.Version == "1.2.3"
	})
	agents, err := w.app.client.ListMachineCustomAgents(ctx, &localv1.ListMachineCustomAgentsRequest{MachineId: mid})
	if err != nil || len(agents.Agents) != 1 || agents.Agents[0].Env[1].Value != "" || !agents.Agents[0].Env[1].Secret {
		t.Fatalf("listed: %v %v", agents, err)
	}
	cur, _ := os.ReadFile(filepath.Join(w.tmp, "owner-mac", "owner", "current"))
	machineDir := filepath.Join(w.tmp, "owner-mac", "owner", "profiles", string(cur), "machine")
	// The folder as JSON writes it: on Windows its backslashes doubled.
	quoted, _ := json.Marshal(fake)
	if b, err := os.ReadFile(filepath.Join(machineDir, "agents.json")); err != nil || strings.Contains(string(b), "k-123") || !strings.Contains(string(b), strings.Trim(string(quoted), `"`)) {
		t.Fatalf("agents.json: %v\n%s", err, b)
	}

	// A session from the app, then one from an agent.
	res, err := w.app.client.StartSession(ctx, &localv1.StartSessionRequest{MachineId: mid,
		Session: &sessionv1.StartSessionRequest{ProjectId: "demo", Agent: "mine", Prompt: "say hello\nenv MINE_API_KEY"}})
	if err != nil {
		t.Fatal(err)
	}
	id := res.Session.SessionId
	idle := func(id, want string) bool {
		res, err := w.app.client.ListSessions(ctx, &localv1.ListSessionsRequest{})
		if err != nil {
			return false
		}
		for _, s := range res.Sessions {
			if s.SessionId == id {
				return s.Status == sessionv1.SessionStatus_SESSION_STATUS_IDLE && strings.Contains(s.Result, want)
			}
		}
		return false
	}
	eventually(t, time.Minute, "the app's session answered, with its key", func() bool { return idle(id, "env: MINE_API_KEY=k-123") })
	s, r := w.startSession(time.Minute, "--agent", "mine", "--wait", "--prompt", "say from the agent")
	if r.code != 0 || s.Status != "idle" || !strings.Contains(s.Result, "from the agent") {
		t.Fatalf("an agent's session: %+v\n%s", s, r.stdout+r.stderr)
	}

	// hermeshd restarts: the agent is there, with its key.
	w.restartOwner(false)
	eventually(t, 30*time.Second, "the agent listed after the restart", func() bool {
		a := listed(mid)
		return a != nil && a.Available
	})
	if s2, r := w.send(time.Minute, s.SessionID, "env MINE_API_KEY"); r.code != 0 || !strings.Contains(s2.Result, "env: MINE_API_KEY=k-123") {
		t.Fatalf("after the restart: %+v\n%s", s2, r.stdout+r.stderr)
	}

	// Removed: gone from the list, and no session starts with it.
	if _, err := w.app.client.RemoveMachineCustomAgent(ctx, &localv1.RemoveMachineCustomAgentRequest{MachineId: mid, Name: "mine"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, 30*time.Second, "the agent gone from the list", func() bool { return listed(mid) == nil })
	if _, r := w.startSession(time.Minute, "--agent", "mine", "--prompt", "say again"); r.code == 0 || !strings.Contains(r.stdout, `unknown agent \"mine\"`) {
		t.Fatalf("started with a removed agent: %s", r.stdout+r.stderr)
	}
	if b, _ := os.ReadFile(filepath.Join(machineDir, "keys", "agent-secrets.json")); strings.Contains(string(b), "mine") {
		t.Fatalf("its key kept: %s", b)
	}

	// Another computer of the owner's: configured through its tunnel.
	work, _ := w.otherMac("work-mac", nil)
	wid := w.logInMachineOnly(work, st, "work-mac").LocalMachineId
	eventually(t, 30*time.Second, "work-mac online", func() bool {
		res, err := w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
		if err != nil {
			return false
		}
		for _, m := range res.Machines {
			if m.MachineId == wid && m.Online {
				return true
			}
		}
		return false
	})
	def.Name = "remote"
	if res := test(wid, def); !res.Ok {
		t.Fatalf("test on work-mac: %+v", res)
	}
	if _, err := w.app.client.SaveMachineCustomAgent(ctx, &localv1.SaveMachineCustomAgentRequest{MachineId: wid, Agent: def}); err != nil {
		t.Fatal(err)
	}
	if res, err := w.app.client.ListMachineCustomAgents(ctx, &localv1.ListMachineCustomAgentsRequest{MachineId: wid}); err != nil || len(res.Agents) != 1 || res.Agents[0].Name != "remote" {
		t.Fatalf("work-mac's: %v %v", res, err)
	}
	if res, err := w.app.client.ListMachineCustomAgents(ctx, &localv1.ListMachineCustomAgentsRequest{MachineId: mid}); err != nil || len(res.Agents) != 0 {
		t.Fatalf("this Mac's: %v %v", res, err)
	}
	if _, err := w.app.client.RemoveMachineCustomAgent(ctx, &localv1.RemoveMachineCustomAgentRequest{MachineId: wid, Name: "remote"}); err != nil {
		t.Fatal(err)
	}
}
