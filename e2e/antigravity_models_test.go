package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
)

// Exercise exactly the agent-list/session CLI used by coding_agent_list through
// the real relay and daemon. Only fakeacp runs: no Google requests or quota.
func TestAntigravityModelCatalogs(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	for _, tc := range []struct{ name, options, models, failure, state string }{
		{"unsupported", "", "small,large", "", "session_required"},
		{"supported", "1", "small,large", "", "known"},
		{"empty", "1", "", "", "known"},
		{"auth", "1", "small", "-32000", "auth_required"},
		{"error", "1", "small", "-32603", "error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, state := t.TempDir(), t.TempDir()
			os.Mkdir(filepath.Join(home, "antigravity-acp"), 0o700)
			os.WriteFile(filepath.Join(home, "antigravity-acp", "settings.json"), []byte(`{"auth":{"type":"oauth-personal"}}`), 0o600)
			log := filepath.Join(t.TempDir(), "methods")
			env := []string{"GEMINI_HOME=" + home, "FAKE_ACP_STATE=" + state, "FAKE_ACP_LOG=" + log,
				"AIDE_ANTIGRAVITY_BIN=" + filepath.Join(binaries(t), "fakeacp"), "FAKE_ACP_CONFIG=1",
				"FAKE_ACP_OPTIONS=" + tc.options, "FAKE_ACP_MODELS=" + tc.models, "FAKE_ACP_OPTIONS_ERROR=" + tc.failure}
			w := newOwnerMac(t, env, &localv1.GrantSpec{Agents: []string{"antigravity"}})
			type catalog struct {
				Scope, Source, State string
				Models               []struct{ ID string }
				DefaultModel         string `json:"default_model"`
				CurrentModel         string `json:"current_model"`
				ObservedAt           int64  `json:"observed_at_unix_ms"`
				ExpiresAt            int64  `json:"expires_at_unix_ms"`
				Stale                bool
			}
			list := func() []catalog {
				t.Helper()
				r := w.cli(30*time.Second, "agent", "list", "--agent", "antigravity", "-o", "json")
				var doc struct {
					Agents []struct {
						Models       []any
						DefaultModel string    `json:"default_model"`
						Catalogs     []catalog `json:"model_catalogs"`
					}
				}
				if r.code != 0 || json.Unmarshal([]byte(r.stdout), &doc) != nil || len(doc.Agents) != 1 {
					t.Fatalf("list: %+v", r)
				}
				a := doc.Agents[0]
				if len(a.Models) != 0 || a.DefaultModel != "" {
					t.Fatalf("scoped data became machine-wide: %+v", a)
				}
				return a.Catalogs
			}
			cs := list()
			if len(cs) != 1 || cs[0].State != tc.state || cs[0].Scope != "isolated" || cs[0].Stale || cs[0].ObservedAt == 0 || cs[0].ExpiresAt <= cs[0].ObservedAt {
				t.Fatalf("discovery: %+v", cs)
			}
			if tc.name == "supported" && (len(cs[0].Models) != 2 || cs[0].DefaultModel != "small") {
				t.Fatalf("options: %+v", cs)
			}
			if tc.name == "empty" && len(cs[0].Models) != 0 {
				t.Fatalf("empty: %+v", cs)
			}
			list()
			methods, _ := os.ReadFile(log)
			// Its check's handshake (工单 #176), then its models' own; listed again, neither again.
			want := "initialize\ninitialize\n"
			if tc.options == "1" {
				want += "session/options\n"
			}
			if string(methods) != want {
				t.Fatalf("listing created a session or re-probed: %q", methods)
			}
			files, _ := os.ReadDir(state)
			if len(files) != 0 {
				t.Fatal("discovery left session history")
			}
			if r := w.cli(30*time.Second, "agent", "list", "--agent", "antigravity"); r.code != 0 || !strings.Contains(r.stdout, tc.state) {
				t.Fatalf("human-readable state: %+v", r)
			}
			if tc.name != "unsupported" {
				return
			}
			// The user now requests a normal session. Its model data is harvested
			// before the prompt, without an extra discovery session.
			s, r := w.startSession(time.Minute, "--agent", "antigravity", "--model", "large", "--wait", "--prompt", "say observed")
			if r.code != 0 || s.Status != "idle" {
				t.Fatalf("normal session: %+v", r)
			}
			cs = list()
			sessionCatalog := func() catalog {
				t.Helper()
				for _, c := range cs {
					if strings.HasSuffix(c.Scope, "/"+s.SessionID) {
						return c
					}
				}
				t.Fatal("missing session observation")
				return catalog{}
			}
			if c := sessionCatalog(); len(cs) != 2 || c.DefaultModel != "small" || c.CurrentModel != "large" {
				t.Fatalf("normal session catalog: %+v", cs)
			}
			options := `[{"id":"model","category":"model","type":"select","currentValue":"next","options":[{"value":"next","name":"Next"}]}]`
			if r := w.cli(time.Minute, "session", "send", s.SessionID, "--wait", "--message", "model-options "+options+"\nsay updated"); r.code != 0 {
				t.Fatalf("update: %+v", r)
			}
			cs = list()
			if c := sessionCatalog(); c.Source != "acp_update" || c.DefaultModel != "small" || c.CurrentModel != "next" || len(c.Models) != 1 || c.Models[0].ID != "next" {
				t.Fatalf("updated catalog: %+v", cs)
			}
			methods, _ = os.ReadFile(log)
			if strings.Count(string(methods), "session/new\n") != 1 || strings.Count(string(methods), "session/prompt\n") != 2 || strings.Contains(string(methods), "session/options") {
				t.Fatalf("extra inference/session: %s", methods)
			}
		})
	}
}

// Uses an already-installed Electron only; unlike the general desktop harness,
// this test never invokes an installer or updates the running user's app.
func TestAntigravityModelCatalogDesktop(t *testing.T) {
	if testing.Short() || runtime.GOOS != "darwin" {
		t.Skip("desktop macOS e2e")
	}
	dir, _ := filepath.Abs(filepath.Join("apps", "desktop"))
	if _, err := os.Stat(filepath.Join(dir, "node_modules", "electron", "dist", "Electron.app", "Contents", "MacOS", "Electron")); err != nil {
		t.Skip("Electron must already be installed")
	}
	home := t.TempDir()
	os.Mkdir(filepath.Join(home, "antigravity-acp"), 0o700)
	os.WriteFile(filepath.Join(home, "antigravity-acp", "settings.json"), []byte(`{"auth":{"type":"oauth-personal"}}`), 0o600)
	w := newOwnerMac(t, []string{"GEMINI_HOME=" + home, "FAKE_ACP_STATE=" + t.TempDir(), "FAKE_ACP_CONFIG=1", "FAKE_ACP_MODELS=small,large",
		"AIDE_ANTIGRAVITY_BIN=" + filepath.Join(binaries(t), "fakeacp")}, &localv1.GrantSpec{Agents: []string{"antigravity"}})
	s, r := w.startSession(time.Minute, "--agent", "antigravity", "--model", "large", "--wait", "--prompt", "say observed")
	if r.code != 0 {
		t.Fatalf("fake session: %+v", r)
	}
	out := filepath.Join(t.TempDir(), "app")
	if r := run(t, []string{"AIDE_DESKTOP_OUT=" + out, "VERSION=0.1.0-rc7"}, 5*time.Minute, "pnpm", "--dir", dir, "build"); r.code != 0 {
		t.Fatalf("build: %+v", r)
	}
	f := desktopFixture{Socket: w.sock, Sessions: map[string]string{"observed": s.SessionID}, CLI: filepath.Join(w.bin, "aide"), CLIEnv: w.cliEnv}
	file := filepath.Join(t.TempDir(), "fixture.json")
	b, _ := json.Marshal(f)
	os.WriteFile(file, b, 0o600)
	env := []string{"AIDE_E2E_FIXTURE=" + file, "AIDE_DESKTOP_OUT=" + out, "AIDE_PW_OUT=" + filepath.Join(t.TempDir(), "results"), "AIDE_MODEL_CATALOG_TEST=1"}
	if r := run(t, env, 3*time.Minute, "pnpm", "--dir", dir, "exec", "playwright", "test", "model-catalogs.spec.ts"); r.code != 0 {
		t.Fatalf("playwright: %+v", r)
	}
}

// 工单 #111 升级补充: end-to-end verification that Antigravity is a first-class
// local coding agent with 3-tier permission presets, read-only enforcement,
// real session continuation, and #117 unified "Provider · Model ID" naming.
func TestAntigravityFirstClassDispatch(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	home, state := t.TempDir(), t.TempDir()
	os.Mkdir(filepath.Join(home, "antigravity-acp"), 0o700)
	os.WriteFile(filepath.Join(home, "antigravity-acp", "settings.json"), []byte(`{"auth":{"type":"oauth-personal"}}`), 0o600)
	env := []string{
		"GEMINI_HOME=" + home,
		"FAKE_ACP_STATE=" + state,
		"AIDE_ANTIGRAVITY_BIN=" + filepath.Join(binaries(t), "fakeacp"),
		"FAKE_ACP_CONFIG=1",
		"FAKE_ACP_OPTIONS=1",
		"FAKE_ACP_REQUIRE_MODE=1",
		"FAKE_ACP_RESUME=resume",
		"FAKE_ACP_MODELS=gemini-2.5-pro,claude-sonnet-4-5",
	}
	w := newOwnerMac(t, env, &localv1.GrantSpec{Agents: []string{"antigravity"}})

	// 1. agent list exposes 3 permission presets and #117 formatted observed models.
	r := w.cli(30*time.Second, "agent", "list", "--agent", "antigravity", "-o", "json")
	if r.code != 0 {
		t.Fatalf("agent list: %+v", r)
	}
	var listDoc struct {
		Agents []struct {
			Modes    []struct{ ID string }
			Catalogs []struct {
				Models []struct{ ID, Name string }
			} `json:"model_catalogs"`
		}
	}
	if err := json.Unmarshal([]byte(r.stdout), &listDoc); err != nil || len(listDoc.Agents) != 1 {
		t.Fatalf("parse agent list: %v %s", err, r.stdout)
	}
	if len(listDoc.Agents[0].Modes) != 3 || listDoc.Agents[0].Modes[0].ID != "read-only" {
		t.Fatalf("expected 3 permission presets, got %+v", listDoc.Agents[0].Modes)
	}
	if len(listDoc.Agents[0].Catalogs) == 0 || len(listDoc.Agents[0].Catalogs[0].Models) != 2 ||
		listDoc.Agents[0].Catalogs[0].Models[0].Name != "Google · gemini-2.5-pro" ||
		listDoc.Agents[0].Catalogs[0].Models[1].Name != "Anthropic · claude-sonnet-4-5" {
		t.Fatalf("expected #117 model names, got %+v", listDoc.Agents[0].Catalogs)
	}

	// 2. Dispatch with --mode read-only: write attempt is blocked by Aide's gate;
	// continuing the same session works cleanly.
	s, r := w.startSession(time.Minute, "--agent", "antigravity", "--mode", "read-only", "--wait", "--prompt", "write blocked.txt nope")
	if r.code != 0 || s.Status != "idle" {
		t.Fatalf("read-only start: %+v", r)
	}
	if ev := eventsOf(w, s.SessionID); !strings.Contains(ev, "read-only mode") {
		t.Fatalf("expected read-only gate denial in events, got:\n%s", ev)
	}
	if r := w.cli(time.Minute, "session", "send", s.SessionID, "--wait", "--message", "say continued"); r.code != 0 {
		t.Fatalf("continue session: %+v", r)
	}
	if ev := eventsOf(w, s.SessionID); !strings.Contains(ev, "continued") {
		t.Fatalf("expected continuation reply in events, got:\n%s", ev)
	}

	// 3. Invalid permission preset fails inline with truthful error.
	if r := w.cli(30*time.Second, "session", "start", "--project", "demo", "--agent", "antigravity", "--mode", "invalid-tier", "--prompt", "say no"); r.code == 0 {
		t.Fatalf("expected invalid mode to fail, got: %+v", r)
	}
}
