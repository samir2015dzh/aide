package e2e

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
)

// signInClaude is a stand-in Claude Code whose command line keeps its own
// sign-in (工单 #108), in dir: signed in while dir/signed-in is there, its
// `auth status` saying nothing clear while dir/vague is, and saying signed
// in while dir/claims-in is whatever its sessions find (a stale answer).
// `auth login` signs it in, writing what it was run with to dir/login-args.
// A session signed in answers; one not signed in fails as Claude Code does,
// "Not logged in · Please run /login". The Claude desktop app beside it is
// signed in (LOCALAPPDATA in env), which signs none of this in.
func signInClaude(t *testing.T) (bin, dir string, env []string) {
	dir = t.TempDir()
	local := filepath.Join(dir, "local")
	data := filepath.Join(local, "Claude-Data", "claude_somepub")
	os.MkdirAll(filepath.Join(data, "claude-code", "2.1.330"), 0o755)
	b, _ := json.Marshal(map[string]any{"oauth:tokenCacheV2": "opaque", "lastKnownAccountUuid": "00000000-0000-4000-8000-000000000000", "windowSizeWasSignedIn": true})
	os.WriteFile(filepath.Join(data, "config.json"), b, 0o600)
	bin = filepath.Join(dir, "claude")
	os.WriteFile(bin, []byte(`#!/bin/sh
d=`+dir+`
case "$1 $2" in
"--version ") echo "2.1.330 (Claude Code)"; exit;;
"auth status") if [ -f $d/vague ]; then echo "unknown command: auth"; exit 1; elif [ -f $d/signed-in ] || [ -f $d/claims-in ]; then echo '{"loggedIn":true}'; else echo '{"loggedIn":false}'; fi; exit;;
"auth login") echo "$@" > $d/login-args; sleep 3; touch $d/signed-in; exit;;
esac
[ "$2" = /usage ] && exit 0
id=
while [ $# -gt 0 ]; do case "$1" in --session-id|--resume) id=$2;; esac; shift; done
read first
case "$first" in *'"aide-models"'*) exit;; esac
if [ ! -f $d/signed-in ]; then
	echo '{"type":"result","subtype":"success","is_error":true,"result":"Not logged in · Please run /login"}'
	exit 1
fi
echo '{"type":"system","subtype":"init","session_id":"'$id'","model":"claude-sonnet-5-5"}'
while :; do
	echo '{"type":"user","message":{"role":"user","content":"x"},"isReplay":true}'
	echo '{"type":"assistant","message":{"content":[{"type":"text","text":"done, signed in"}]}}'
	echo '{"type":"result","subtype":"success","result":"done, signed in"}'
	read prompt || exit 0
done
`), 0o755)
	return bin, dir, []string{"AIDE_CLAUDE_BIN=" + bin, "LOCALAPPDATA=" + local}
}

// TestClaudeSignInIsWhatSessionsRun (工单 #108): Claude Code's sign-in, as
// the apps are told it, is its command line's own, the one its sessions run
// with: unconfirmed when the command line does not say, never the Claude
// desktop app's (signed in here throughout); signed out the moment a
// session fails "Not logged in", without waiting for a check; signed in
// from Aide with `claude auth login` and then running; signed out again
// once its sign-in is gone.
func TestClaudeSignInIsWhatSessionsRun(t *testing.T) {
	unixOnly(t, "its fake Claude Code is a shell script")
	if testing.Short() {
		t.Skip("e2e")
	}
	_, dir, env := signInClaude(t)
	os.WriteFile(filepath.Join(dir, "vague"), nil, 0o644)
	w := newOwnerMac(t, env, &localv1.GrantSpec{Agents: []string{"claude"}})
	ctx := context.Background()
	ms, err := w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	mid := ms.Machines[0].MachineId
	claude := func() *machinev1.AgentStatus {
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
	check := func() {
		t.Helper()
		if _, err := w.app.client.CheckAgents(ctx, &localv1.CheckAgentsRequest{}); err != nil {
			t.Fatal(err)
		}
	}
	// A session's turn as it ended: its status and its last failure.
	session := func(prompt string) *sessionv1.Session {
		t.Helper()
		res, err := w.app.client.StartSession(ctx, &localv1.StartSessionRequest{MachineId: mid,
			Session: &sessionv1.StartSessionRequest{ProjectId: "demo", Agent: "claude", Prompt: prompt}})
		if err != nil {
			t.Fatal(err)
		}
		var s *sessionv1.Session
		eventually(t, time.Minute, "the session's turn ended: "+prompt, func() bool {
			l, err := w.app.client.ListSessions(ctx, &localv1.ListSessionsRequest{})
			for _, x := range l.GetSessions() {
				if err == nil && x.SessionId == res.Session.SessionId && (x.Status == sessionv1.SessionStatus_SESSION_STATUS_IDLE || x.Status == sessionv1.SessionStatus_SESSION_STATUS_CRASHED) {
					s = x
				}
			}
			return s != nil
		})
		return s
	}

	// The command line says nothing clear: unconfirmed, not signed in
	// because the app is.
	eventually(t, 30*time.Second, "unconfirmed, the app signed in", func() bool {
		a := claude()
		return a.GetAvailable() && !a.LoggedIn && a.LoginUnconfirmed && a.GetDesktop().GetSignedIn()
	})

	// It says signed in, but its sessions are not: the first one that
	// fails "Not logged in" signs it out at once, no check run.
	os.Remove(filepath.Join(dir, "vague"))
	os.WriteFile(filepath.Join(dir, "claims-in"), nil, 0o644)
	check()
	eventually(t, 30*time.Second, "signed in, as it claims", func() bool { return claude().GetLoggedIn() })
	if s := session("fix the header"); s.Progress.GetLastFailure().GetKind() != sessionv1.FailureKind_FAILURE_KIND_AUTH {
		t.Fatalf("not signed in, the session: %v", s)
	}
	if a := claude(); a.LoggedIn || a.LoginUnconfirmed || !a.GetDesktop().GetSignedIn() {
		t.Fatalf("after the session failed not signed in: %v", a)
	}
	os.Remove(filepath.Join(dir, "claims-in"))

	// Signed in from Aide: `claude auth login`, nothing else; then it runs.
	if _, err := w.app.client.StartMachineAgentLogin(ctx, &localv1.StartMachineAgentLoginRequest{MachineId: mid, Agent: "claude"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, 30*time.Second, "signed in from Aide", func() bool {
		a := claude()
		return a.GetLogin().GetState() == machinev1.AgentLoginState_AGENT_LOGIN_STATE_SUCCEEDED && a.LoggedIn
	})
	if b, _ := os.ReadFile(filepath.Join(dir, "login-args")); strings.TrimSpace(string(b)) != "auth login" {
		t.Fatalf("run with %q", b)
	}
	if s := session("fix the footer"); s.Status != sessionv1.SessionStatus_SESSION_STATUS_IDLE || s.Progress.GetLastFailure() != nil || !strings.Contains(s.Result, "signed in") {
		t.Fatalf("signed in, the session: %v", s)
	}

	// Its sign-in gone: signed out at the next check.
	os.Remove(filepath.Join(dir, "signed-in"))
	check()
	eventually(t, 30*time.Second, "signed out again", func() bool {
		a := claude()
		return !a.GetLoggedIn() && !a.GetLoginUnconfirmed()
	})
}
