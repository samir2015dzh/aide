package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
)

// TestRealDSHPermissionPresets is TestDSHPermissionPresets with the real
// DeepSeek Harness (AIDE_REAL_DSH: its dsh command, e.g. from
// `npm install @deepseek-ai/dsh`), in a DSH home of its own, and a stand-in
// for DeepSeek's API ($DEEPSEEK_BASE_URL) that answers a prompt "WRITE
// <file>" with DSH's write tool on that file in the session's directory,
// then with what the tool said. The owner's own DSH configuration (here a
// preset of their own, and their default for new sessions) is respected
// and never written. Each session's preset is checked in DSH's own
// records of the session.
func TestRealDSHPermissionPresets(t *testing.T) {
	dsh := os.Getenv("AIDE_REAL_DSH")
	if testing.Short() || dsh == "" {
		t.Skip("real DeepSeek Harness only: AIDE_REAL_DSH=<its dsh command>")
	}
	home, err := os.MkdirTemp("", "dsh-home")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { // after aided: its dsh processes may still be saving their sessions
		for i := 0; os.RemoveAll(home) != nil && i < 50; i++ {
			time.Sleep(100 * time.Millisecond)
		}
	})
	own := "- id: permission\n  config:\n    presets:\n      read-only: {sandbox: read-only, approval: ask}\n      workspace-write: {sandbox: workspace-write, approval: ask}\n" +
		"      danger-full-access: {sandbox: danger-full-access, approval: never}\n      review: {sandbox: read-only, approval: never, name: Review}\n" +
		"    defaultPreset: danger-full-access\n"
	if err := os.WriteFile(filepath.Join(home, "cordis.patch.yml"), []byte(own), 0o600); err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(http.HandlerFunc(fakeDeepSeek))
	t.Cleanup(api.Close)
	w := newOwnerMac(t, []string{"AIDE_DEEPSEEK_BIN=" + dsh, "PATH=" + os.Getenv("PATH"), "DSH_HOME=" + home, "DSH_TELEMETRY_DISABLED=1",
		"DEEPSEEK_BASE_URL=" + api.URL, "DEEPSEEK_API_KEY=sk-test"}, &localv1.GrantSpec{Agents: []string{"deepseek"}})

	var l struct {
		Agents []struct {
			DefaultMode string `json:"default_mode"`
			Modes       []struct{ ID, Name string }
		}
	}
	if r := w.cli(time.Minute, "agent", "list", "--agent", "deepseek", "-o", "json"); json.Unmarshal([]byte(r.stdout), &l) != nil || len(l.Agents) != 1 || len(l.Agents[0].Modes) != 4 ||
		l.Agents[0].Modes[3].Name != "Review" || l.Agents[0].DefaultMode != "danger-full-access" {
		t.Fatalf("agent list: %+v", r)
	}

	start := func(mode string) string {
		t.Helper()
		args := []string{"--agent", "deepseek", "--prompt", "WRITE out.txt"}
		if mode != "" {
			args = append(args, "--mode", mode)
		}
		s, r := w.startSession(time.Minute, args...)
		if r.code != 0 {
			t.Fatalf("start %q: %+v", mode, r)
		}
		return s.SessionID
	}
	ids := map[string]string{}
	for _, mode := range []string{"read-only", "workspace-write", "review", ""} { // at once
		ids[mode] = start(mode)
	}
	for mode, id := range ids {
		eventually(t, 2*time.Minute, "session "+mode+" done", func() bool { return w.get(id).Status == "idle" })
		s := w.get(id)
		denied := strings.Contains(s.Result, "denied under read-only mode")
		if wantDenied := mode == "read-only" || mode == "review"; denied != wantDenied || !denied && !strings.Contains(s.Result, "Created file") {
			t.Errorf("%q: %+v", mode, s)
		}
		if got := dshPreset(t, home, s.Ref); got != cmpOr(mode, "danger-full-access") {
			t.Errorf("%q: DSH records preset %q", mode, got)
		}
	}
	// Resumed by a new dsh process (aided restarted), a session keeps its
	// preset.
	w.restartOwner(false)
	if s, r := w.send(2*time.Minute, ids["read-only"], "WRITE again.txt"); r.code != 0 || !strings.Contains(s.Result, "denied under read-only mode") ||
		dshPreset(t, home, s.Ref) != "read-only" {
		t.Fatalf("resumed: %+v", r)
	}
	if b, _ := os.ReadFile(filepath.Join(home, "cordis.patch.yml")); string(b) != own {
		t.Fatalf("the owner's DSH configuration changed:\n%s", b)
	}
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// dshPreset is the permission preset DSH records for a session (its
// projection cache).
func dshPreset(t *testing.T, home, ref string) string {
	t.Helper()
	var c struct {
		Record struct {
			Rows struct {
				Permissions struct {
					Val struct{ Preset string }
				}
			}
		}
	}
	b, err := os.ReadFile(filepath.Join(home, "storages", "session_projcache", "sessions", ref+".json"))
	if err != nil || json.Unmarshal(b, &c) != nil {
		t.Fatalf("DSH's record of session %s: %v", ref, err)
	}
	return c.Record.Rows.Permissions.Val.Preset
}

var dshCwd = regexp.MustCompile(`Current working directory: \\"(.*?)\\"`)

// fakeDeepSeek answers DSH's Messages requests: "WRITE <file>" with a
// write tool call, the tool's result with what it said.
func fakeDeepSeek(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	json.Unmarshal(body, &req)
	var last []struct {
		Type    string          `json:"type"`
		Text    string          `json:"text"`
		Content json.RawMessage `json:"content"`
	}
	if len(req.Messages) > 0 {
		json.Unmarshal(req.Messages[len(req.Messages)-1].Content, &last)
	}
	w.Header().Set("content-type", "text/event-stream")
	event := func(kind string, data any) {
		b, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, b)
	}
	event("message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_1", "type": "message", "role": "assistant", "model": "m",
		"content": []any{}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1}}})
	block := func(start, delta map[string]any) {
		event("content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": start})
		event("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": delta})
		event("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	}
	stop := "end_turn"
	result := slices.IndexFunc(last, func(c struct {
		Type    string          `json:"type"`
		Text    string          `json:"text"`
		Content json.RawMessage `json:"content"`
	}) bool {
		return c.Type == "tool_result"
	})
	file, write := "", false
	for _, c := range last {
		if f, ok := strings.CutPrefix(c.Text, "WRITE "); ok {
			file, write = strings.TrimSpace(f), true
		}
	}
	m := dshCwd.FindSubmatch(body)
	switch {
	case result >= 0:
		block(map[string]any{"type": "text", "text": ""}, map[string]any{"type": "text_delta", "text": "tool: " + string(last[result].Content)})
	case write && m != nil:
		input, _ := json.Marshal(map[string]string{"file_path": filepath.Join(string(m[1]), file), "content": "hello\n"})
		block(map[string]any{"type": "tool_use", "id": "toolu_1", "name": "write", "input": map[string]any{}}, map[string]any{"type": "input_json_delta", "partial_json": string(input)})
		stop = "tool_use"
	default:
		block(map[string]any{"type": "text", "text": ""}, map[string]any{"type": "text_delta", "text": "done"})
	}
	event("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stop}, "usage": map[string]any{"output_tokens": 1}})
	event("message_stop", map[string]any{"type": "message_stop"})
}
