package e2e

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
)

// TestAgentCLIUpdate (工单 #178): the owner's app sees that a newer Codex
// is out than the machine's — its release channel, npm, asked by the
// machine — updates it with Codex's own updater, is shown what it printed,
// and the machine reads the new version back: up to date now. With the
// channel unreachable it is unknown, never "up to date", and nothing
// fails.
func TestAgentCLIUpdate(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	if runtime.GOOS == "windows" {
		t.Skip("a shell script stands in for Codex")
	}
	var latest atomic.Value
	latest.Store("0.171.0")
	npm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/@openai%2Fcodex/latest" || latest.Load() == "down" {
			http.Error(w, "no", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`{"name":"@openai/codex","version":"` + latest.Load().(string) + `"}`))
	}))
	defer npm.Close()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "version"), []byte("0.170.0\n"), 0o600)
	codex := filepath.Join(dir, "codex")
	os.WriteFile(codex, []byte(`#!/bin/sh
d=$(dirname "$0")
case "$1" in
--version) echo "codex-cli $(cat "$d/version")";;
login) echo "Logged in using ChatGPT";;
update) echo "Updating Codex via npm…"; echo "npm warn: a deprecated dependency" >&2; echo 0.171.0 > "$d/version"; echo "Updated to 0.171.0";;
*) exec `+filepath.Join(binaries(t), "fakecodex")+` "$@";;
esac
`), 0o755)
	w := newOwnerMac(t, []string{"AIDE_CODEX_BIN=" + codex, "AIDE_NPM_REGISTRY=" + npm.URL}, &localv1.GrantSpec{Agents: []string{"codex"}})
	ctx := context.Background()
	row := func() (machineID string, a *machinev1.AgentStatus) {
		res, err := w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
		if err != nil || len(res.Machines) == 0 {
			return "", nil
		}
		for _, x := range res.Machines[0].Status.GetAgents() {
			if x.Name == "codex" {
				a = x
			}
		}
		return res.Machines[0].MachineId, a
	}
	var machineID string
	eventually(t, time.Minute, "a newer Codex out", func() bool {
		id, a := row()
		machineID = id
		return a.GetVersion() == "0.170.0" && a.GetUpdate().GetState() == machinev1.AgentUpdateState_AGENT_UPDATE_STATE_AVAILABLE && a.GetUpdate().GetLatest() == "0.171.0"
	})

	res, err := w.app.client.UpdateMachineAgent(ctx, &localv1.UpdateMachineAgentRequest{MachineId: machineID, Name: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Succeeded || res.Version != "0.171.0" || res.Command != codex+" update" ||
		!strings.Contains(res.Output, "Updating Codex via npm…") || !strings.Contains(res.Output, "npm warn: a deprecated dependency") || !strings.Contains(res.Output, "Updated to 0.171.0") {
		t.Fatalf("the update: %+v", res)
	}
	eventually(t, time.Minute, "up to date, the new version read back", func() bool {
		_, a := row()
		return a.GetVersion() == "0.171.0" && a.GetUpdate().GetState() == machinev1.AgentUpdateState_AGENT_UPDATE_STATE_CURRENT
	})

	latest.Store("down")
	if _, err := w.app.client.CheckAgents(ctx, &localv1.CheckAgentsRequest{MachineId: machineID, OnPurpose: true}); err != nil {
		t.Fatal(err)
	}
	eventually(t, time.Minute, "unknown with the channel down", func() bool {
		_, a := row()
		u := a.GetUpdate()
		return a.GetAvailable() && u.GetState() == machinev1.AgentUpdateState_AGENT_UPDATE_STATE_UNKNOWN && strings.Contains(u.GetDetail(), "503")
	})
}
