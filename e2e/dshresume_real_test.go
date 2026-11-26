package e2e

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
)

// TestRealDSHResumeKeysStored: with DeepSeek's key kept where the DSH app
// keeps the keys its owner types in (its credentials store, not the
// environment), a session resumed by a new dsh process runs its next turn
// (工单 #172: on an owner's machine a resumed session failed at once, "llm-deepseek"
// missing its credentials).
func TestRealDSHResumeKeysStored(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(home, ".credentials.yaml"), []byte("version: 1\nrefs:\n  DEEPSEEK_API_KEY: sk-test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(http.HandlerFunc(fakeDeepSeek))
	t.Cleanup(api.Close)
	w := newOwnerMac(t, []string{"AIDE_DEEPSEEK_BIN=" + dsh, "PATH=" + os.Getenv("PATH"), "DSH_HOME=" + home, "DSH_TELEMETRY_DISABLED=1",
		"DEEPSEEK_BASE_URL=" + api.URL}, &localv1.GrantSpec{Agents: []string{"deepseek"}})
	s, r := w.startSession(time.Minute, "--agent", "deepseek", "--prompt", "WRITE out.txt")
	if r.code != 0 {
		t.Fatalf("start: %+v", r)
	}
	eventually(t, 2*time.Minute, "first turn done", func() bool { return w.get(s.SessionID).Status == "idle" })
	if got := w.get(s.SessionID); !strings.Contains(got.Result, "Created file") {
		t.Fatalf("first turn: %+v", got)
	}
	for i := range 3 { // each resume is a new dsh process
		w.restartOwner(false)
		got, r := w.send(2*time.Minute, s.SessionID, fmt.Sprintf("WRITE again%d.txt", i))
		if r.code != 0 || !strings.Contains(got.Result, "Created file") {
			t.Fatalf("resumed (%d): %+v %+v", i, r, got)
		}
	}
}
