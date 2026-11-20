package e2e

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
)

// TestDSHPermissionPresets: DeepSeek Harness's permission presets (WO #5
// 补充), with a scripted agent that, like DSH's `acp` profile, offers no
// modes over ACP, prints its presets for --dump-config and gives a new
// session the defaultPreset of a --patch overlay. They are the deepseek
// agent's modes before any session; two sessions at once with different
// presets each run with their own (the read-only one cannot write, the
// other can); one with none has DSH's own default; a session resumed by a
// new process (aided restarted) keeps its preset; one DSH does not have
// is refused before a session starts. The owner's app shows them.
func TestDSHPermissionPresets(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	w := newOwnerMac(t, []string{"AIDE_DEEPSEEK_BIN=" + filepath.Join(binaries(t), "fakeacp"+exe), "FAKE_ACP_STATE=" + t.TempDir(), "FAKE_ACP_RESUME=resume",
		"FAKE_ACP_CONFIG=1", "FAKE_ACP_MODELS=v4-pro", "FAKE_ACP_PRESETS="}, &localv1.GrantSpec{Agents: []string{"deepseek"}})
	ctx := context.Background()
	presets := "read-only, workspace-write, danger-full-access"

	// Before any session.
	var l struct {
		Agents []struct {
			Modes []struct{ ID string }
		}
	}
	if r := w.cli(30*time.Second, "agent", "list", "--agent", "deepseek", "-o", "json"); json.Unmarshal([]byte(r.stdout), &l) != nil || len(l.Agents) != 1 || len(l.Agents[0].Modes) != 3 ||
		l.Agents[0].Modes[0].ID != "read-only" {
		t.Fatalf("agent list: %+v", r)
	}
	if _, r := w.startSession(30*time.Second, "--agent", "deepseek", "--mode", "yolo", "--prompt", "config"); r.code == 0 ||
		!strings.Contains(r.stdout, `deepseek on this machine has no mode \"yolo\" (one of: `+presets+`)`) {
		t.Fatalf("a preset DSH does not have: %+v", r)
	}

	// Two sessions at once.
	start := func(mode, text string) string {
		t.Helper()
		s, r := w.startSession(30*time.Second, "--agent", "deepseek", "--mode", mode, "--prompt", "sleep 2s\nwrite out.txt "+text+"\nconfig")
		if r.code != 0 {
			t.Fatalf("start %s: %+v", mode, r)
		}
		return s.SessionID
	}
	ro, rw := start("read-only", "from-read-only"), start("workspace-write", "from-workspace-write")
	eventually(t, 30*time.Second, "both running", func() bool { return w.get(ro).Status == "running" && w.get(rw).Status == "running" })
	eventually(t, time.Minute, "both done", func() bool { return w.get(ro).Status == "idle" && w.get(rw).Status == "idle" })
	if got := w.get(ro).Result; got != "config: v4-pro  read-only" {
		t.Fatalf("read-only: %q", got)
	}
	if got := w.get(rw).Result; got != "config: v4-pro  workspace-write" {
		t.Fatalf("workspace-write: %q", got)
	}
	if d := w.cli(30*time.Second, "session", "diff", ro); strings.Contains(d.stdout, "from-read-only") {
		t.Fatalf("the read-only session wrote: %+v", d)
	}
	if d := w.cli(30*time.Second, "session", "diff", rw); !strings.Contains(d.stdout, "+from-workspace-write") {
		t.Fatalf("the workspace-write session could not write: %+v", d)
	}

	// None chosen: DSH's own default.
	if s, r := w.startSession(time.Minute, "--agent", "deepseek", "--wait", "--prompt", "config"); r.code != 0 || s.Result != "config: v4-pro  workspace-write" {
		t.Fatalf("unchosen: %+v", r)
	}

	// Resumed by a new process, the session keeps its preset.
	w.restartOwner(false)
	if s, r := w.send(time.Minute, ro, "write again.txt still-read-only\nconfig"); r.code != 0 || s.Result != "config: v4-pro  read-only" {
		t.Fatalf("resumed: %+v", r)
	}
	if d := w.cli(30*time.Second, "session", "diff", ro); strings.Contains(d.stdout, "still-read-only") {
		t.Fatalf("the resumed read-only session wrote: %+v", d)
	}

	// The owner's app: the agent's modes (its Mode row).
	eventually(t, 30*time.Second, "deepseek's presets in the app", func() bool {
		res, err := w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
		if err != nil || len(res.Machines) == 0 {
			return false
		}
		for _, a := range res.Machines[0].Status.GetAgents() {
			if a.Name == "deepseek" && len(a.Modes) == 3 && a.Modes[2].Id == "danger-full-access" && len(a.Models) == 1 {
				return true
			}
		}
		return false
	})
}
