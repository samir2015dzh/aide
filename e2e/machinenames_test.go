package e2e

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
)

// TestMachineNamesKept (工单 #84): an agent reads a machine by the name the
// apps show, from the relay's list, even while the machine is off and the
// relay restarted since it last reported; a machine the relay never heard a
// name from is called so in plain words with the end of its id, never by
// its whole id.
func TestMachineNamesKept(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	env := []string{"AIDE_FAKE_PROVIDERS=1"}
	w := newOwnerMac(t, env, &localv1.GrantSpec{})
	ctx := context.Background()
	daemon := filepath.Join(w.bin, "aided")

	// far-pc runs, reports its name, and goes off; never-pc never runs.
	far := filepath.Join(w.tmp, "far-pc")
	farID := w.loginMachine("far-pc", far, env)
	p := start(t, "far-pc", env, daemon, "run", "--data-dir", far)
	w.logs = append(w.logs, p)
	eventually(t, 30*time.Second, "far-pc online", func() bool {
		res, err := w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
		return err == nil && slices.ContainsFunc(res.Machines, func(m *localv1.Machine) bool { return m.MachineName == "far-pc" && m.Online })
	})
	p.stop()
	neverID := w.loginMachine("never-pc", filepath.Join(w.tmp, "never-pc"), env)
	w.restartRelay()

	type offline struct {
		Machine   string
		MachineID string `json:"machine_id"`
	}
	var got []offline
	eventually(t, 30*time.Second, "both listed offline", func() bool {
		r := run(t, append(w.cliEnv, "AIDE_LANG=zh-CN"), 30*time.Second, filepath.Join(w.bin, "aide"), "project", "list", "-o", "json")
		var doc struct{ Offline []offline }
		got = nil
		if json.Unmarshal([]byte(r.stdout), &doc) == nil {
			got = doc.Offline
		}
		return len(got) == 2
	})
	for _, o := range got {
		switch o.MachineID {
		case farID:
			if o.Machine != "far-pc" {
				t.Errorf("far-pc after the relay restarted: %q", o.Machine)
			}
		case neverID:
			if want := "未命名的机器 · " + neverID[len(neverID)-4:]; o.Machine != want || strings.Contains(o.Machine, neverID) {
				t.Errorf("never-pc: %q, want %q", o.Machine, want)
			}
		default:
			t.Errorf("offline: %+v", o)
		}
	}
}
