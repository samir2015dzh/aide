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
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	"github.com/samir2015dzh/aide/internal/acpregistry"
	"github.com/samir2015dzh/aide/internal/provider"
	"github.com/samir2015dzh/aide/internal/provider/acp"
)

// TestInstallAntigravityProgram (工单 #133): on a computer with no
// Antigravity ACP server, installing the registry's antigravity-acp puts
// Google's archive, its SHA-256 checked, where the built-in antigravity
// agent looks for it — not as a custom agent beside it — and it is done
// once the agent is found there. A download that is not the one the
// registry names is refused, and nothing is left for the agent to find.
func TestInstallAntigravityProgram(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	bin := buildFakeACPBinary(t)
	payload, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	cmd := "agy_acp_server.par"
	if runtime.GOOS == "windows" {
		cmd = "agy_acp_server.exe"
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, b := range map[string][]byte{cmd: payload, "localharness_external/README": []byte("kept beside the server\n")} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(b)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	archive := buf.Bytes()
	sum := sha256.Sum256(archive)
	good := hex.EncodeToString(sum[:])
	want := good
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/registry.json":
			json.NewEncoder(w).Encode(acpregistry.Index{Version: "1.0.0", Agents: []acpregistry.Entry{{
				ID: "antigravity-acp", Name: "Google Antigravity", Version: "1.5.2",
				Distribution: acpregistry.Distribution{Binary: map[string]acpregistry.BinaryTarget{
					acpregistry.CurrentPlatform(): {Archive: srvURL + "/agy.zip", Cmd: "./" + cmd, SHA256: want},
				}},
			}}})
		case "/agy.zip":
			w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	srvURL = srv.URL

	providers := map[string]provider.Provider{}
	for _, p := range acp.Providers("") {
		if p.Name() == "antigravity" {
			providers["antigravity"] = p
		}
	}
	dataDir := t.TempDir()
	m, err := Open(Config{DataDir: dataDir, Providers: providers})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	reg := func() {
		m.SetACPRegistry(acpregistry.New(acpregistry.Options{DataDir: dataDir, URL: srvURL + "/registry.json"}))
	}
	reg()
	ctx := context.Background()
	<-m.CheckAgents(true, 0)
	if m.providerInfo("antigravity").Available {
		t.Fatal("antigravity found before its server was installed")
	}
	// The registry lists it as the built-in agent's, not as one to install as a custom agent.
	if e := m.ListACPRegistry(ctx, true).Entries; len(e) != 1 || e[0].Supported || e[0].Installed {
		t.Fatalf("listed before: %+v", e)
	}

	// Not the download the registry names: refused, nothing installed.
	want = strings.Repeat("0", 64)
	reg()
	if _, err := m.InstallACPRegistryAgent(ctx, &machinev1.InstallACPRegistryAgentRequest{RegistryId: "antigravity-acp"}); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("a download not the registry's: %v", err)
	}
	dir := acp.AntigravityDir(home, runtime.GOOS)
	if _, err := os.Stat(filepath.Join(dir, cmd)); err == nil {
		t.Fatal("a refused download was installed")
	}

	want = good
	reg()
	res, err := m.InstallACPRegistryAgent(ctx, &machinev1.InstallACPRegistryAgentRequest{RegistryId: "antigravity-acp"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Agent.GetName() != "antigravity" || res.Agent.GetSource() != "builtin" || res.InstallDir != dir || res.Sha256 != good {
		t.Fatalf("installed: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, "localharness_external", "README")); err != nil {
		t.Fatalf("what the archive keeps beside the server: %v", err)
	}
	if info := m.providerInfo("antigravity"); !info.Available || info.Binary != filepath.Join(dir, cmd) {
		t.Fatalf("antigravity after the install: %+v", info)
	}
	for _, a := range m.Status().Agents {
		if a.Custom {
			t.Fatalf("installed as a custom agent too: %+v", a)
		}
	}
	if e := m.ListACPRegistry(ctx, false).Entries; len(e) != 1 || !e[0].Installed || e[0].InstalledAs != "antigravity" {
		t.Fatalf("listed after: %+v", e)
	}
}
