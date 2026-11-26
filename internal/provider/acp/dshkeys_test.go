package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/provider"
)

// fakeDSH answers ACP as DSH does around its credentials store (工单 #172):
// its first turn fails for DeepSeek's key; asked again, its model's
// provider offers three models when the key has loaded meanwhile ("late"),
// that one alone when there is none ("missing"). Each method is written to
// $FAKE_DSH_LOG, a line each.
func fakeDSH(mode string) {
	if slices.Contains(os.Args, "--dump-config") {
		return
	}
	logf, _ := os.OpenFile(os.Getenv("FAKE_DSH_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	out := json.NewEncoder(os.Stdout)
	reply := func(id json.RawMessage, result any) {
		out.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	options := func(models ...string) []any {
		var choices []any
		for _, m := range models {
			choices = append(choices, map[string]any{"value": fmt.Sprintf(`["deepseek-official",%q]`, m), "name": m})
		}
		return []any{map[string]any{"id": "model", "category": "model", "type": "select", "currentValue": `["deepseek-official","flash"]`,
			"options": []any{map[string]any{"group": "deepseek-official", "name": "DeepSeek", "options": choices}}}}
	}
	prompts := 0
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(in.Bytes(), &m) != nil || m.Method == "" {
			continue
		}
		fmt.Fprintln(logf, m.Method)
		switch m.Method {
		case "initialize":
			reply(m.ID, map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{}})
		case "session/new":
			reply(m.ID, map[string]any{"sessionId": "dsh-1", "configOptions": options("flash")})
		case "session/set_config_option":
			if mode == "late" {
				reply(m.ID, map[string]any{"configOptions": options("flash", "pro", "chat")})
			} else {
				reply(m.ID, map[string]any{"configOptions": options("flash")})
			}
		case "session/prompt":
			if prompts++; prompts == 1 || mode == "missing" {
				out.Encode(map[string]any{"jsonrpc": "2.0", "id": m.ID, "error": map[string]any{"code": -32603,
					"message": `Internal error: turn failed: llm-deepseek: no API key for provider route "deepseek-official"; store DEEPSEEK_API_KEY through the credentials service (the web Models page writes it), or export DEEPSEEK_API_KEY in the launching environment`}})
				continue
			}
			out.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "dsh-1",
				"update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "done"}}}})
			reply(m.ID, map[string]any{"stopReason": "end_turn"})
		default:
			reply(m.ID, map[string]any{})
		}
	}
}

// TestDSHKeyNotLoadedYet (工单 #172): a DSH process's first turn that failed
// for a key DSH had not loaded yet is sent again once, and runs; one whose
// key is missing indeed fails as it did, sent once.
func TestDSHKeyNotLoadedYet(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"late", "missing"} {
		t.Run(mode, func(t *testing.T) {
			log := t.TempDir() + "/calls"
			p := &Provider{Agent: Agent{Name: "deepseek", Command: "dsh", DSHPresets: true}, Binary: self}
			s, err := p.Start(context.Background(), provider.StartSpec{WorkDir: t.TempDir(), TempDir: t.TempDir(), Prompt: "hi",
				Env: append(os.Environ(), "AIDE_TEST_FAKE_DSH="+mode, "FAKE_DSH_LOG="+log, "DSH_HOME="+t.TempDir())})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { s.Close() })
			_, done := collect(t, s)
			b, _ := os.ReadFile(log)
			prompts := strings.Count(string(b), "session/prompt")
			switch mode {
			case "late":
				if done.IsError || done.Text != "done" || prompts != 2 {
					t.Fatalf("sent again once and ran: %+v, %d prompts", done, prompts)
				}
			case "missing":
				if !done.IsError || done.Failure == nil || done.Failure.Kind != sessionv1.FailureKind_FAILURE_KIND_MISSING_CREDENTIALS || prompts != 1 {
					t.Fatalf("failed as it did: %+v, %d prompts", done, prompts)
				}
			}
		})
	}
}
