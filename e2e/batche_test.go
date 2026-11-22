package e2e

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
)

// TestClaudeLoginFromAide (工单 #51, choice A): the owner signs Claude Code's
// command line in from the app, never opening a terminal. aided starts
// `claude auth login` on the machine (whose browser the owner would sign in
// in); while it waits the agent says so; once it ends the agent is checked
// again and the sign-in succeeded only if it now says signed in. A sign-in
// that fails says why, one cancelled says so, and one that can't start
// (no Claude Code here) is refused, never pretended.
func TestClaudeLoginFromAide(t *testing.T) {
	unixOnly(t, "its fake Claude Code is a shell script")
	if testing.Short() {
		t.Skip("e2e")
	}
	dir := t.TempDir()
	signed := filepath.Join(dir, "signed-in")
	mode := filepath.Join(dir, "mode") // what `auth login` does: ok, fail or wait
	claude := filepath.Join(dir, "claude")
	os.WriteFile(claude, []byte(`#!/bin/sh
case "$1 $2" in
"--version ") echo "2.1.330 (Claude Code)";;
"auth status") if [ -f `+signed+` ]; then echo '{"loggedIn":true}'; else echo '{"loggedIn":false}'; fi;;
"auth login") case "$(cat `+mode+`)" in
  ok) sleep 1; touch `+signed+`;;
  fail) echo "Login failed: the browser was closed (sk-ant-oat01-SECRETSECRETSECRETSECRETSECRET)" >&2; exit 1;;
  wait) sleep 60 & wait;; # a child left holding its output, as a browser opener may be
  esac;;
esac
`), 0o755)
	os.WriteFile(mode, []byte("fail"), 0o644)
	w := newOwnerMac(t, []string{"AIDE_CLAUDE_BIN=" + claude}, &localv1.GrantSpec{Agents: []string{"claude"}})
	ctx := context.Background()
	ms, err := w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	mid := ms.Machines[0].MachineId
	login := func() *machinev1.AgentStatus {
		ms, err := w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
		if err != nil || len(ms.Machines) == 0 {
			return nil
		}
		for _, a := range ms.Machines[0].Status.GetAgents() {
			if a.Name == "claude" {
				return a
			}
		}
		return nil
	}
	start := func(cancel bool) *machinev1.AgentLogin {
		t.Helper()
		r, err := w.app.client.StartMachineAgentLogin(ctx, &localv1.StartMachineAgentLoginRequest{MachineId: mid, Agent: "claude", Cancel: cancel})
		if err != nil {
			t.Fatal(err)
		}
		return r.Login
	}

	// Fails: why, in its own words, nothing token-like kept.
	if l := start(false); l.State != machinev1.AgentLoginState_AGENT_LOGIN_STATE_WAITING {
		t.Fatalf("started: %v", l)
	}
	eventually(t, 30*time.Second, "the sign-in failed, said why", func() bool {
		a := login()
		return a.GetLogin().GetState() == machinev1.AgentLoginState_AGENT_LOGIN_STATE_FAILED && strings.Contains(a.GetLogin().GetDetail(), "the browser was closed")
	})
	if d := login().GetLogin().GetDetail(); strings.Contains(d, "SECRET") {
		t.Fatalf("a token-like string kept: %q", d)
	}

	// Cancelled while it waits.
	os.WriteFile(mode, []byte("wait"), 0o644)
	start(false)
	if l := start(false); l.State != machinev1.AgentLoginState_AGENT_LOGIN_STATE_WAITING {
		t.Fatalf("asked again while it waits: %v", l)
	}
	start(true)
	eventually(t, 30*time.Second, "the sign-in cancelled", func() bool {
		return login().GetLogin().GetState() == machinev1.AgentLoginState_AGENT_LOGIN_STATE_CANCELLED
	})

	// Done in the browser: checked again, signed in.
	os.WriteFile(mode, []byte("ok"), 0o644)
	start(false)
	eventually(t, 30*time.Second, "signed in", func() bool {
		a := login()
		return a.GetLogin().GetState() == machinev1.AgentLoginState_AGENT_LOGIN_STATE_SUCCEEDED && a.LoggedIn
	})

	// Gone: refused as not installed, nothing started.
	os.Remove(claude)
	if _, err := w.app.client.StartMachineAgentLogin(ctx, &localv1.StartMachineAgentLoginRequest{MachineId: mid, Agent: "claude"}); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("without Claude Code: %v", err)
	}
	if _, err := w.app.client.StartMachineAgentLogin(ctx, &localv1.StartMachineAgentLoginRequest{MachineId: mid, Agent: "codex"}); connect.CodeOf(err) != connect.CodeUnimplemented {
		t.Fatalf("another agent: %v", err)
	}
}

// TestClientCategory (工单 #52): an agent's kind is told from its name until
// the owner sets it; what the owner set wins, everywhere (the apps' list of
// clients, the roster an agent reads), and survives (it is kept with the
// grant); cleared, the name tells again.
func TestClientCategory(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	w := newOwnerMac(t, []string{"AIDE_FAKE_PROVIDERS=1"}, &localv1.GrantSpec{})
	ctx := context.Background()
	ms, err := w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	mid := ms.Machines[0].MachineId
	var grantID string
	cs, err := w.app.client.ListClients(ctx, &localv1.ListClientsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cs.Clients {
		if c.Label == "bot" {
			grantID = c.GrantId
		}
	}
	me := func() (string, string) {
		var got struct {
			Endpoints []struct {
				You               bool
				Category, Machine string
			}
		}
		r := w.cli(30*time.Second, "roster", "-o", "json")
		if r.code != 0 || json.Unmarshal([]byte(r.stdout), &got) != nil {
			t.Fatalf("roster: %+v", r)
		}
		for _, e := range got.Endpoints {
			if e.You {
				return e.Category, e.Machine
			}
		}
		return "", ""
	}
	if c, _ := me(); c != "personal" {
		t.Fatalf("bot, by its name: %q", c)
	}
	if _, err := w.app.client.SetClientCategory(ctx, &localv1.SetClientCategoryRequest{GrantId: grantID, Category: "local", MachineId: mid}); err != nil {
		t.Fatal(err)
	}
	eventually(t, 30*time.Second, "set: the apps and the roster say local, on its machine", func() bool {
		cs, err := w.app.client.ListClients(ctx, &localv1.ListClientsRequest{})
		ok := false
		for _, c := range cs.GetClients() {
			ok = ok || (err == nil && c.GrantId == grantID && c.Category == "local" && c.CategoryMachineId == mid)
		}
		c, m := me()
		return ok && c == "local" && m == "owner-mac"
	})
	if _, err := w.app.client.SetClientCategory(ctx, &localv1.SetClientCategoryRequest{GrantId: grantID, Category: "robot"}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("an unknown kind: %v", err)
	}
	if _, err := w.app.client.SetClientCategory(ctx, &localv1.SetClientCategoryRequest{GrantId: grantID}); err != nil {
		t.Fatal(err)
	}
	eventually(t, 30*time.Second, "cleared: told by its name again", func() bool { c, _ := me(); return c == "personal" })
}
