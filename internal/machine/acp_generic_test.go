package machine

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/acpregistry"
	"github.com/samir2015dzh/aide/internal/provider"
)

func buildFakeACPBinary(t *testing.T) string {
	t.Helper()
	name := "fakeacp"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-o", bin, "../provider/acp/fakeacp")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fakeacp: %v\n%s", err, out)
	}
	return bin
}

func zipFileBytes(t *testing.T, entryName string, payload []byte) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(entryName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:])
}

// TestGenericACPRegistryAndLocalEndToEnd verifies 工单 #110 end to end:
//   - No fake ACP entries appear on a machine with no ACP agents installed
//   - ACP v2 preview negotiation and automatic v1 downgrade
//   - Installing an official ACP Registry agent via SHA-256 verified binary archive
//   - Registering a local ACP command with explicit argument list (no shell expansion)
//   - Running real sessions (prompt, tool call, approval, completion) on BOTH sources
//   - Model lists match what each agent actually reports item for item
//   - Capabilities are truthfully declared (fork/rollback/steer/login_in_aide/usage)
//   - Removing the registry agent cleans up its isolated install directory
func TestGenericACPRegistryAndLocalEndToEnd(t *testing.T) {
	bin := buildFakeACPBinary(t)
	binBytes, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	entryName := "bin/regacp"
	if runtime.GOOS == "windows" {
		entryName = "bin/regacp.exe"
	}
	archiveBytes, archiveSHA := zipFileBytes(t, entryName, binBytes)

	plat := acpregistry.CurrentPlatform()
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/registry.json":
			_ = json.NewEncoder(w).Encode(acpregistry.Index{
				Version: "1.0.0",
				Agents: []acpregistry.Entry{
					{
						ID:          "reg-demo-acp",
						Name:        "Registry Demo ACP",
						Version:     "1.2.3",
						Description: "Mock official ACP Registry agent for #110 E2E",
						License:     "Apache-2.0",
						Distribution: acpregistry.Distribution{
							Binary: map[string]acpregistry.BinaryTarget{
								plat: {
									Archive: srvURL + "/regacp.zip",
									Cmd:     "./bin/regacp",
									Args:    []string{"--registry-flag"},
									Env: map[string]string{
										"FAKE_ACP_V2":     "1",
										"FAKE_ACP_CONFIG": "1",
										"FAKE_ACP_MODELS": "reg-fast,reg-pro",
									},
									SHA256: archiveSHA,
								},
							},
						},
					},
				},
			})
		case "/regacp.zip":
			_, _ = w.Write(archiveBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	srvURL = srv.URL

	workDir := t.TempDir()
	dataDir := t.TempDir()
	m, err := Open(Config{
		DataDir:   dataDir,
		Providers: map[string]provider.Provider{},
	})
	if err != nil {
		t.Fatalf("open machine: %v", err)
	}
	defer m.Close()
	m.SetACPRegistry(acpregistry.New(acpregistry.Options{
		DataDir: dataDir,
		URL:     srvURL + "/registry.json",
	}))

	// 1. Machine with no ACP agents installed must NOT show fake ACP entries in Status().
	stBefore := m.Status()
	for _, a := range stBefore.Agents {
		if a.Custom || a.Name == "reg-demo-acp" || a.Name == "local-acp" {
			t.Fatalf("unexpected fake ACP entry before install: %+v", a)
		}
	}

	// 2. List official ACP Registry -> shows reg-demo-acp as supported, SHA-256 verified, not installed.
	listed := m.ListACPRegistry(context.Background(), true)
	if listed.Source != "remote" || len(listed.Entries) != 1 {
		t.Fatalf("unexpected registry list: %+v", listed)
	}
	if !listed.Entries[0].Supported || !listed.Entries[0].Sha256Verified || listed.Entries[0].Installed {
		t.Fatalf("unexpected registry entry state: %+v", listed.Entries[0])
	}

	// 3. Source A (Registry): install with SHA-256 verification into machine-isolated dir.
	instResp, err := m.InstallACPRegistryAgent(context.Background(), &machinev1.InstallACPRegistryAgentRequest{
		RegistryId: "reg-demo-acp",
	})
	if err != nil {
		t.Fatalf("InstallACPRegistryAgent: %v", err)
	}
	if instResp.Sha256 != archiveSHA || instResp.Agent.Source != "registry" || instResp.Agent.RegistryId != "reg-demo-acp" {
		t.Fatalf("unexpected install response: %+v", instResp)
	}

	// 4. Source B (Local command): register with explicit args and v2-reject -> v1 downgrade + fork/image flags.
	localDef := &machinev1.CustomAgent{
		Name:    "local-acp",
		Command: bin,
		Args:    []string{"--local-arg", "hello world"},
		Env: []*machinev1.AgentEnv{
			{Name: "FAKE_ACP_REJECT_V2", Value: "1"},
			{Name: "FAKE_ACP_CONFIG", Value: "1"},
			{Name: "FAKE_ACP_FORK", Value: "1"},
			{Name: "FAKE_ACP_IMAGES", Value: "1"},
			{Name: "FAKE_ACP_MODELS", Value: "local-alpha,local-beta"},
		},
	}
	if err := m.SaveCustomAgent(localDef); err != nil {
		t.Fatalf("SaveCustomAgent(local-acp): %v", err)
	}

	// TestCustomAgent handshake validates v2 preview on registry agent and v1 downgrade on local agent,
	// plus truthful capabilities and real-time model lists.
	regTest, err := m.TestCustomAgent(context.Background(), instResp.Agent)
	if err != nil || !regTest.Ok {
		t.Fatalf("TestCustomAgent(registry): err=%v res=%+v", err, regTest)
	}
	if regTest.ProtocolVersion != 2 {
		t.Fatalf("expected registry agent negotiated protocol v2, got %d", regTest.ProtocolVersion)
	}
	if regTest.Capabilities.Fork || regTest.Capabilities.Rollback || regTest.Capabilities.Steer || regTest.Capabilities.LoginInAide || regTest.Capabilities.Usage {
		t.Fatalf("registry agent capabilities not truthful: %+v", regTest.Capabilities)
	}
	if got := modelIDs(regTest.Models); !slices.Equal(got, []string{"reg-fast", "reg-pro"}) {
		t.Fatalf("registry models mismatch: got %v", got)
	}

	localTest, err := m.TestCustomAgent(context.Background(), localDef)
	if err != nil || !localTest.Ok {
		t.Fatalf("TestCustomAgent(local): err=%v res=%+v", err, localTest)
	}
	if localTest.ProtocolVersion != 1 {
		t.Fatalf("expected local agent v2->v1 downgrade, got %d", localTest.ProtocolVersion)
	}
	if !localTest.Capabilities.Fork || !localTest.Capabilities.Images || localTest.Capabilities.Rollback || localTest.Capabilities.Steer {
		t.Fatalf("local agent capabilities not truthful: %+v", localTest.Capabilities)
	}
	if got := modelIDs(localTest.Models); !slices.Equal(got, []string{"local-alpha", "local-beta"}) {
		t.Fatalf("local models mismatch: got %v", got)
	}

	// Wait for background recheck so Status() reflects both installed agents and their truthful capabilities.
	regStatus := waitAgent(t, m, "reg-demo-acp", func(a *machinev1.AgentStatus) bool { return a.Available && !a.Checking })
	if regStatus.Source != "registry" || regStatus.RegistryId != "reg-demo-acp" || regStatus.ProtocolVersion != 2 || !regStatus.LoginUnconfirmed {
		t.Fatalf("unexpected registry agent status: %+v", regStatus)
	}
	localStatus := waitAgent(t, m, "local-acp", func(a *machinev1.AgentStatus) bool { return a.Available && !a.Checking })
	if localStatus.Source != "local" || localStatus.ProtocolVersion != 1 || !localStatus.Capabilities.Fork || localStatus.Capabilities.Rollback {
		t.Fatalf("unexpected local agent status: %+v", localStatus)
	}

	// 5. Run a real turn (prompt + tool call + approval gate + file write + completion) on BOTH providers!
	runRealTurn := func(agentName string, wantModels []string) {
		t.Helper()
		p := m.provider(agentName)
		if p == nil {
			t.Fatalf("provider %q missing", agentName)
		}
		var gateMu sync.Mutex
		var gatedTools []string
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		outFile := agentName + ".txt"
		sess, err := p.Start(ctx, provider.StartSpec{
			WorkDir: workDir,
			Prompt:  "write " + outFile + " hello-from-" + agentName,
			Gate: func(_ context.Context, a provider.ToolAction) provider.Decision {
				gateMu.Lock()
				gatedTools = append(gatedTools, a.Name)
				gateMu.Unlock()
				return provider.Decision{Allow: true}
			},
		})
		if err != nil {
			t.Fatalf("%s Start: %v", agentName, err)
		}
		defer sess.Close()

		var sawToolCall, sawToolResult, sawTurnDone bool
		var liveModels []string
		for ev := range sess.Events() {
			switch ev.Kind {
			case provider.EventRef:
				if ev.Offers != nil {
					for _, om := range ev.Offers.Models {
						liveModels = append(liveModels, om.ID)
					}
				}
			case provider.EventToolCall:
				sawToolCall = true
			case provider.EventToolResult:
				sawToolResult = true
			case provider.EventTurnCompleted:
				sawTurnDone = true
				goto done
			case provider.EventError:
				t.Fatalf("%s failed: %s", agentName, ev.Text)
			}
		}
	done:
		if !sawToolCall || !sawToolResult || !sawTurnDone {
			t.Fatalf("%s incomplete turn: toolCall=%v toolResult=%v turnDone=%v", agentName, sawToolCall, sawToolResult, sawTurnDone)
		}
		if !slices.Equal(liveModels, wantModels) {
			t.Fatalf("%s live session models mismatch: got %v want %v", agentName, liveModels, wantModels)
		}
		b, err := os.ReadFile(filepath.Join(workDir, outFile))
		if err != nil || strings.TrimSpace(string(b)) != "hello-from-"+agentName {
			t.Fatalf("%s expected approved file write, got %q err=%v", agentName, string(b), err)
		}
		gateMu.Lock()
		defer gateMu.Unlock()
		if len(gatedTools) == 0 {
			t.Fatalf("%s expected gate approval callback for tool call", agentName)
		}
		if err := sess.Steer(ctx, provider.Input{Text: "nudge"}); err == nil {
			t.Fatalf("%s expected Steer to return unsupported error", agentName)
		}
		// Ensure ACP provider refuses conversation rollback cleanly (#113 / #110).
		if rc, ok := p.(interface{ CanRollbackConversation() (bool, string) }); !ok {
			t.Fatalf("%s must implement CanRollbackConversation", agentName)
		} else if can, reason := rc.CanRollbackConversation(); can || !strings.Contains(reason, "ACP") {
			t.Fatalf("%s ACP provider must refuse conversation rollback, got can=%v reason=%q", agentName, can, reason)
		}
	}

	runRealTurn("reg-demo-acp", []string{"reg-fast", "reg-pro"})
	runRealTurn("local-acp", []string{"local-alpha", "local-beta"})

	// Verify Registry listing marks reg-demo-acp as installed.
	listedAfter := m.ListACPRegistry(context.Background(), false)
	if !listedAfter.Entries[0].Installed || listedAfter.Entries[0].InstalledAs != "reg-demo-acp" {
		t.Fatalf("expected reg-demo-acp marked installed, got %+v", listedAfter.Entries[0])
	}

	// 6. Removing registry agent cleans up its isolated binary folder.
	if err := m.RemoveCustomAgent("reg-demo-acp"); err != nil {
		t.Fatalf("RemoveCustomAgent(reg-demo-acp): %v", err)
	}
	if _, err := os.Stat(instResp.InstallDir); !os.IsNotExist(err) {
		t.Fatalf("expected registry install dir removed on uninstall, stat err=%v", err)
	}
}

func modelIDs(ms []*machinev1.AgentModel) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Id)
	}
	return out
}

// modesRecorder is an agent that advertises permission modes and a default.
type modesRecorder struct{ recorder }

func (r *modesRecorder) Discover(ctx context.Context) provider.Info {
	i := r.recorder.Discover(ctx)
	i.Name = "antigravity"
	i.Modes = []provider.Mode{{ID: "read-only"}, {ID: "workspace-write"}, {ID: "danger-full-access"}}
	i.Own.Mode = "workspace-write"
	return i
}

// 审计 C: an Antigravity session started without a mode runs with (and
// records) the default it advertises, not an empty pass-through mode.
func TestAntigravityStartWithoutModeTakesAdvertisedDefault(t *testing.T) {
	r := &modesRecorder{}
	h := newHarnessWith(t, DefaultLimits(), map[string]provider.Provider{"antigravity": r})
	s, err := h.s.Start(context.Background(), h.p, &sessionv1.StartSessionRequest{ProjectId: "demo", Agent: "antigravity", Prompt: "say ok"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "session idle", func() bool {
		v, err := h.s.Get(h.p, s.SessionId)
		return err == nil && v.Status == idle
	})
	if got := r.spec(0).Mode; got != "workspace-write" {
		t.Fatalf("mode passed to the agent: %q", got)
	}
}
