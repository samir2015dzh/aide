package e2e

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
	"testing"
	"time"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
	"github.com/samir2015dzh/aide/internal/acpregistry"
)

// TestInstallAntigravityFromAide (工单 #133): a computer with the Antigravity
// desktop app only has no ACP server for Aide to run. The owner's one
// install (the Connect page's button, LocalService.InstallMachineACPRegistryAgent)
// fetches it from the official ACP registry's entry — here a stand-in
// registry serving a stand-in server — checks it, puts it where the
// built-in antigravity agent looks, and the agent is detected and runs a
// task: nothing left for the owner but signing in. The stand-in server ends
// at once on --version, as Google's gflags server does (工单 #175): found
// by its ACP handshake all the same (工单 #176).
func TestInstallAntigravityFromAide(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	url, sha := fakeAntigravityRegistry(t)
	home := t.TempDir()
	gemini := t.TempDir() // its sign-in, which only the owner gives: given here
	os.Mkdir(filepath.Join(gemini, "antigravity-acp"), 0o700)
	os.WriteFile(filepath.Join(gemini, "antigravity-acp", "settings.json"), []byte(`{"auth":{"type":"oauth-personal"}}`), 0o600)
	w := newOwnerMac(t, []string{"HOME=" + home, "USERPROFILE=" + home, "PATH=/usr/bin:/bin", "GEMINI_HOME=" + gemini,
		"AIDE_ACP_REGISTRY_URL=" + url, "FAKE_ACP_STATE=" + t.TempDir(), "FAKE_ACP_NO_VERSION_FLAG=1"}, &localv1.GrantSpec{Agents: []string{"antigravity"}})
	ctx := context.Background()
	antigravity := func() (machineID string, available bool) {
		res, err := w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
		if err != nil || len(res.Machines) == 0 {
			return "", false
		}
		for _, a := range res.Machines[0].Status.GetAgents() {
			if a.Name == "antigravity" {
				available = a.Available
			}
		}
		return res.Machines[0].MachineId, available
	}
	var machineID string
	eventually(t, 30*time.Second, "the machine", func() bool { machineID, _ = antigravity(); return machineID != "" })
	if _, ok := antigravity(); ok {
		t.Fatal("antigravity detected before its server was installed")
	}

	res, err := w.app.client.InstallMachineACPRegistryAgent(ctx, &localv1.InstallMachineACPRegistryAgentRequest{MachineId: machineID, RegistryId: "antigravity-acp"})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if res.Agent.GetName() != "antigravity" || res.Agent.GetSource() != "builtin" || res.Sha256 != sha {
		t.Fatalf("installed: %+v", res)
	}
	eventually(t, 30*time.Second, "antigravity detected", func() bool { _, ok := antigravity(); return ok })
	if s, r := w.startSession(2*time.Minute, "--agent", "antigravity", "--wait", "--prompt", "say done"); r.code != 0 || s.Status != "idle" {
		t.Fatalf("a task on the installed server: %+v %+v", s, r)
	}
}

// fakeAntigravityRegistry serves an ACP registry whose antigravity-acp is
// the stand-in ACP agent, as Google's archive has its server: its index's
// address, and the archive's SHA-256.
func fakeAntigravityRegistry(t *testing.T) (url, sha string) {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join(binaries(t), "fakeacp"+exe))
	if err != nil {
		t.Fatal(err)
	}
	cmd := "agy_acp_server.par"
	if runtime.GOOS == "windows" {
		cmd = "agy_acp_server.exe"
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create(cmd)
	f.Write(payload)
	zw.Close()
	archive := buf.Bytes()
	sum := sha256.Sum256(archive)
	sha = hex.EncodeToString(sum[:])
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/registry.json":
			json.NewEncoder(w).Encode(acpregistry.Index{Version: "1.0.0", Agents: []acpregistry.Entry{{ID: "antigravity-acp", Name: "Google Antigravity", Version: "1.5.2",
				Distribution: acpregistry.Distribution{Binary: map[string]acpregistry.BinaryTarget{
					acpregistry.CurrentPlatform(): {Archive: srvURL + "/agy.zip", Cmd: "./" + cmd, SHA256: sha}}}}}})
		case "/agy.zip":
			w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	srvURL = srv.URL
	return srv.URL + "/registry.json", sha
}
