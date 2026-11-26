package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
)

// dshModels is what `aide agent list` says DSH offers here: its models' names.
func dshModels(t *testing.T, w *ownerMac) []string {
	t.Helper()
	var l struct {
		Agents []struct {
			Models []struct{ ID, Name string }
		}
	}
	var names []string
	for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline); time.Sleep(time.Second) {
		r := w.cli(time.Minute, "agent", "list", "--agent", "deepseek", "-o", "json")
		if json.Unmarshal([]byte(r.stdout), &l) == nil && len(l.Agents) == 1 && len(l.Agents[0].Models) > 0 {
			for _, m := range l.Agents[0].Models {
				names = append(names, m.Name)
			}
			return names
		}
	}
	return nil
}

// TestDSHModelsBeforeASession (工单 #166): on a machine where no DSH
// session has run, DSH's models are listed all the same — found by a probe
// session that runs no turn and is closed at once — not the default model
// alone with "send a first message".
func TestDSHModelsBeforeASession(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	log := filepath.Join(t.TempDir(), "acp.log")
	w := newOwnerMac(t, []string{"AIDE_DEEPSEEK_BIN=" + filepath.Join(binaries(t), "fakeacp"+exe), "FAKE_ACP_STATE=" + t.TempDir(),
		"FAKE_ACP_CONFIG=1", "FAKE_ACP_MODELS=v4-flash,v4-pro", "FAKE_ACP_PRESETS=", "FAKE_ACP_LOG=" + log}, &localv1.GrantSpec{Agents: []string{"deepseek"}})
	names := dshModels(t, w)
	if !slices.Contains(names, "v4-flash") || !slices.Contains(names, "v4-pro") {
		t.Fatalf("DSH's models before any session: %q", names)
	}
	b, _ := os.ReadFile(log)
	if calls := string(b); !strings.Contains(calls, "session/new") || !strings.Contains(calls, "session/close") || strings.Contains(calls, "session/prompt") {
		t.Fatalf("the probe: %q", calls)
	}
}

// TestRealDSHModelsBeforeASession is it with the real DeepSeek Harness
// (AIDE_REAL_DSH), in a DSH home of its own whose desktop profile has a
// gateway of the owner's (工单 #129): DeepSeek's models and the gateway's
// are listed before any session, and the probe leaves nothing in the
// owner's DSH — no session, no projection, no configuration written.
func TestRealDSHModelsBeforeASession(t *testing.T) {
	dsh := os.Getenv("AIDE_REAL_DSH")
	if testing.Short() || dsh == "" {
		t.Skip("real DeepSeek Harness only: AIDE_REAL_DSH=<its dsh command>")
	}
	home, err := os.MkdirTemp("", "dsh-home")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for i := 0; os.RemoveAll(home) != nil && i < 50; i++ {
			time.Sleep(100 * time.Millisecond)
		}
	})
	desktop := filepath.Join(home, "profiles", "desktop", "cordis.patch.yml")
	own := "- id: llm-pi-ai\n  config:\n    providers:\n      gw-test:\n        displayName: GW Test\n        apiKeyEnv: GW_TEST_KEY\n" +
		"        api: openai-completions\n        baseURL: https://gateway.example.com/v1\n        models:\n          - id: gw-think\n            name: GW Think\n            contextWindow: 131072\n"
	if err := os.MkdirAll(filepath.Dir(desktop), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(desktop, []byte(own), 0o600); err != nil {
		t.Fatal(err)
	}
	w := newOwnerMac(t, []string{"AIDE_DEEPSEEK_BIN=" + dsh, "PATH=" + os.Getenv("PATH"), "DSH_HOME=" + home, "DSH_TELEMETRY_DISABLED=1",
		"DEEPSEEK_API_KEY=sk-test", "GW_TEST_KEY=sk-gw"}, &localv1.GrantSpec{Agents: []string{"deepseek"}})
	names := dshModels(t, w)
	if !slices.Contains(names, "DeepSeek · deepseek-v4-flash") || !slices.Contains(names, "GW Test · gw-think") {
		t.Fatalf("DSH's models before any session: %q", names)
	}
	// Nothing of the probe in the owner's DSH: its sessions and their projections are where they were (none).
	for _, dir := range []string{filepath.Join(home, "sessions"), filepath.Join(home, "storages", "session_projcache", "sessions")} {
		var found []string
		filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				found = append(found, p)
			}
			return nil
		})
		if len(found) > 0 {
			t.Fatalf("the probe left a session in the owner's DSH: %q", found)
		}
	}
	if b, _ := os.ReadFile(desktop); string(b) != own {
		t.Fatalf("the desktop profile's configuration changed:\n%s", b)
	}
}
