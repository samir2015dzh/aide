package e2e

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
)

// TestACPSessionOptions: DeepSeek Harness (played by a scripted agent that
// speaks real ACP, offering what DSH's `acp` profile does: models in a
// provider's group, the reasoning efforts of the model a session runs, and
// here also modes) tells what it offers only in a session. Its models are
// known before its first session all the same, from a probe session its
// check makes (工单 #166); its modes, and the efforts of a model no session
// has run, after its first session on the Mac. An agent sees the choices in
// `agent list` and names
// them in `session start`; the owner sees them in the app and sets the
// agent's defaults there. The session runs with what was chosen, also once
// resumed after a restart; nothing chosen, it runs as the agent's own
// configuration says. A choice the agent does not offer is refused before
// a session starts.
func TestACPSessionOptions(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	w := newOwnerMac(t, []string{"AIDE_DEEPSEEK_BIN=" + filepath.Join(binaries(t), "fakeacp"+exe), "FAKE_ACP_STATE=" + t.TempDir(), "FAKE_ACP_RESUME=resume",
		"FAKE_ACP_CONFIG=1", "FAKE_ACP_MODELS=magpie/v4-pro,magpie/v4-flash", "FAKE_ACP_EFFORTS=magpie/v4-pro=low|high;magpie/v4-flash=max", "FAKE_ACP_MODES=ask,code"},
		&localv1.GrantSpec{Agents: []string{"deepseek"}})
	ctx := context.Background()
	type listed struct {
		Agents []struct {
			Agent        string
			DefaultModel string `json:"default_model"`
			DefaultMode  string `json:"default_mode"`
			Models       []struct {
				ID, Name string
				Efforts  []string
			}
			Modes []struct{ ID, Name string }
		}
	}
	list := func() listed {
		t.Helper()
		var l listed
		r := w.cli(30*time.Second, "agent", "list", "--agent", "deepseek", "-o", "json")
		if json.Unmarshal([]byte(r.stdout), &l) != nil || len(l.Agents) != 1 {
			t.Fatalf("agent list: %+v", r)
		}
		return l
	}
	run := func(args ...string) string {
		t.Helper()
		s, r := w.startSession(time.Minute, append([]string{"--agent", "deepseek", "--prompt", "config", "--wait"}, args...)...)
		if r.code != 0 || s.Status != "idle" {
			t.Fatalf("start %v: %+v", args, r)
		}
		return s.SessionID
	}
	result := func(id string) string { t.Helper(); return w.get(id).Result }
	refused := func(want string, args ...string) {
		t.Helper()
		var failed struct {
			Error struct{ Kind, Message string }
		}
		if _, r := w.startSession(30*time.Second, append([]string{"--agent", "deepseek", "--prompt", "config"}, args...)...); r.code == 0 ||
			json.Unmarshal([]byte(r.stdout), &failed) != nil || failed.Error.Kind != "usage" || !strings.Contains(failed.Error.Message, want) {
			t.Fatalf("%v: %+v", args, r)
		}
	}

	// Before any session: its models, from the probe its check makes (工单
	// #166), and the efforts of the default one the probe's session says; no
	// modes yet. The probe can take a moment after the machine comes up.
	var before listed
	eventually(t, 30*time.Second, "deepseek's models before a session", func() bool {
		before = list()
		return len(before.Agents[0].Models) > 0
	})
	if l := before.Agents[0]; len(l.Models) != 2 || l.Models[0].ID != "magpie/v4-pro" || l.Models[1].ID != "magpie/v4-flash" ||
		strings.Join(l.Models[0].Efforts, ",") != "low,high" || len(l.Models[1].Efforts) != 0 || l.DefaultModel != "magpie/v4-pro" || len(l.Modes) != 0 {
		t.Fatalf("before a session: %+v", l)
	}
	refused("deepseek on this machine offers no session modes", "--mode", "code")

	// Nothing chosen: the agent's own configuration.
	if id := run(); result(id) != "config: magpie/v4-pro  ask" {
		t.Fatalf("unchosen: %q", result(id))
	}
	l := list().Agents[0]
	if len(l.Models) != 2 || l.Models[0].ID != "magpie/v4-pro" || l.Models[0].Name != "magpie · v4-pro" || strings.Join(l.Models[0].Efforts, ",") != "low,high" ||
		len(l.Modes) != 2 || l.Modes[1].ID != "code" || l.DefaultModel != "magpie/v4-pro" || l.DefaultMode != "ask" {
		t.Fatalf("listed after a session: %+v", l)
	}

	// Chosen by the agent starting the session.
	refused(`deepseek on this machine has no mode "yolo" (one of: ask, code)`, "--mode", "yolo")
	refused(`deepseek's model magpie/v4-pro has no reasoning effort "max" (one of: low, high)`, "--effort", "max")
	chosen := run("--model", "magpie/v4-flash", "--effort", "max", "--mode", "code")
	if got := result(chosen); got != "config: magpie/v4-flash max code" {
		t.Fatalf("chosen: %q", got)
	}
	// The flash model's efforts are known now that a session ran it.
	if l := list().Agents[0]; strings.Join(l.Models[1].Efforts, ",") != "max" {
		t.Fatalf("efforts of the model run: %+v", l.Models)
	}
	// What the app shows of the session: the model and effort it runs.
	if res, err := w.app.client.ListSessions(ctx, &localv1.ListSessionsRequest{}); err != nil || !strings.Contains(sessionModels(res), chosen+" magpie/v4-flash max") {
		t.Fatalf("app sessions: %v %v", sessionModels(res), err)
	}

	// The same session resumed by a new agent process (aided restarted)
	// runs with what it began with.
	w.restartOwner(false)
	if r := w.cli(time.Minute, "session", "send", chosen, "--wait", "--message", "config"); r.code != 0 || !strings.Contains(r.stdout, "config: magpie/v4-flash max code") {
		t.Fatalf("resumed: %+v", r)
	}

	// The owner, in the app: the agent's models and modes, and its defaults.
	var machineID string
	var status *machinev1.AgentStatus
	eventually(t, 30*time.Second, "deepseek in the app", func() bool {
		res, err := w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
		if err != nil || len(res.Machines) == 0 {
			return false
		}
		machineID = res.Machines[0].MachineId
		for _, a := range res.Machines[0].Status.GetAgents() {
			if a.Name == "deepseek" && len(a.Modes) == 2 {
				status = a
			}
		}
		return status != nil
	})
	if status.Own.GetModel() != "magpie/v4-pro" || status.Own.GetMode() != "ask" || len(status.Models) != 2 || status.Modes[1].Name != "Mode code" {
		t.Fatalf("app status: %v", status)
	}
	if _, err := w.app.client.SetAgentDefaults(ctx, &localv1.SetAgentDefaultsRequest{MachineId: machineID,
		Defaults: &identityv1.AgentDefaults{Agent: "deepseek", Model: "magpie/v4-flash", Mode: "code"}}); err != nil {
		t.Fatal(err)
	}
	if id := run(); result(id) != "config: magpie/v4-flash  code" {
		t.Fatalf("with the owner's defaults: %q", result(id))
	}
	if l := list().Agents[0]; l.DefaultModel != "magpie/v4-flash" || l.DefaultMode != "code" {
		t.Fatalf("listed defaults: %+v", l)
	}
	// The agent's choice wins over the default.
	if id := run("--mode", "ask"); result(id) != "config: magpie/v4-flash  ask" {
		t.Fatalf("over the default: %q", result(id))
	}
}

// sessionModels is each session the app lists as "id model effort".
func sessionModels(res *localv1.ListSessionsResponse) string {
	var b strings.Builder
	for _, s := range res.GetSessions() {
		b.WriteString(s.SessionId + " " + s.Model + " " + s.Effort + "\n")
	}
	return b.String()
}
