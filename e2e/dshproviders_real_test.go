package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
)

// TestRealDSHDesktopProviders (工单 #129) is the real DeepSeek Harness
// (AIDE_REAL_DSH), in a DSH home of its own whose desktop profile — the
// DSH desktop app's, which the CLI does not boot — has a gateway of the
// owner's own (llm-pi-ai), as the DSH settings page writes it: Aide's
// sessions, in the acp profile, offer that gateway's model beside
// DeepSeek's, and a session on it runs its turn on the gateway, with the
// key DSH resolves for it. Neither profile's configuration is written.
func TestRealDSHDesktopProviders(t *testing.T) {
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
	var mu sync.Mutex
	var asked []string // model and key of each request the gateway had
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		var req struct{ Model string }
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &req)
		mu.Lock()
		asked = append(asked, req.Model+" "+r.Header.Get("Authorization"))
		mu.Unlock()
		w.Header().Set("content-type", "text/event-stream")
		chunk := func(delta map[string]any, finish any) {
			b, _ := json.Marshal(map[string]any{"id": "c1", "object": "chat.completion.chunk", "model": req.Model,
				"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}})
			fmt.Fprintf(w, "data: %s\n\n", b)
		}
		chunk(map[string]any{"role": "assistant", "content": "answered by the gateway"}, nil)
		chunk(map[string]any{}, "stop")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(gw.Close)
	desktop := filepath.Join(home, "profiles", "desktop", "cordis.patch.yml")
	own := "- id: llm-pi-ai\n  config:\n    providers:\n      gw-test:\n        displayName: GW Test\n        apiKeyEnv: GW_TEST_KEY\n" +
		"        api: openai-completions\n        baseURL: " + gw.URL + "/v1\n        models:\n          - id: gw-think\n            name: GW Think\n            contextWindow: 131072\n"
	if err := os.MkdirAll(filepath.Dir(desktop), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(desktop, []byte(own), 0o600); err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(http.HandlerFunc(fakeDeepSeek))
	t.Cleanup(api.Close)
	w := newOwnerMac(t, []string{"AIDE_DEEPSEEK_BIN=" + dsh, "PATH=" + os.Getenv("PATH"), "DSH_HOME=" + home, "DSH_TELEMETRY_DISABLED=1",
		"DEEPSEEK_BASE_URL=" + api.URL, "DEEPSEEK_API_KEY=sk-test", "GW_TEST_KEY=sk-gw"}, &localv1.GrantSpec{Agents: []string{"deepseek"}})

	// A first session, on DSH's own default: what the agent offers is known after it.
	if s, r := w.startSession(time.Minute, "--agent", "deepseek", "--wait", "--prompt", "hello"); r.code != 0 || s.Status != "idle" {
		t.Fatalf("first session: %+v %+v", s, r)
	}
	var l struct {
		Agents []struct {
			Models []struct{ ID, Name string }
		}
	}
	if r := w.cli(time.Minute, "agent", "list", "--agent", "deepseek", "-o", "json"); json.Unmarshal([]byte(r.stdout), &l) != nil || len(l.Agents) != 1 {
		t.Fatalf("agent list: %+v", r)
	}
	// Named as every model is (工单 #99): the provider, then the model's own id.
	var names []string
	model := ""
	for _, m := range l.Agents[0].Models {
		names = append(names, m.Name)
		if m.Name == "GW Test · gw-think" {
			model = m.ID
		}
	}
	if model == "" || !slices.Contains(names, "DeepSeek · deepseek-v4-flash") {
		t.Fatalf("the desktop profile's gateway is not offered beside DeepSeek's: %q", names)
	}

	// A session on the gateway's model runs its turn there.
	s, r := w.startSession(2*time.Minute, "--agent", "deepseek", "--model", model, "--wait", "--prompt", "hello gateway")
	if r.code != 0 || s.Status != "idle" || !strings.Contains(s.Result, "answered by the gateway") {
		t.Fatalf("session on the gateway: %+v %+v", s, r)
	}
	mu.Lock()
	got := slices.Clone(asked)
	mu.Unlock()
	if len(got) == 0 || got[len(got)-1] != "gw-think Bearer sk-gw" {
		t.Fatalf("the gateway had: %q", got)
	}
	if b, _ := os.ReadFile(desktop); string(b) != own {
		t.Fatalf("the desktop profile's configuration changed:\n%s", b)
	}
	if b, err := os.ReadFile(filepath.Join(home, "profiles", "acp", "cordis.patch.yml")); err != nil || strings.Contains(string(b), "gw-test") {
		t.Fatalf("the acp profile's own layer was written: %v\n%s", err, b)
	}
}
