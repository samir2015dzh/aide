package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samir2015dzh/aide/internal/provider/acp/dshnative"
)

// The desktop's dsh spec: a DSH home with one session of the owner's (not
// Aide's), and the real bridge plugin running for it in a stand-in DSH
// Host (testdata/dsh-bridge-host.mjs), which writes each prompt into the
// session's log as DSH would.

// dshDesktopHome is a new DSH home, short enough for a Unix socket's path.
func dshDesktopHome(t *testing.T) string {
	t.Helper()
	home, err := os.MkdirTemp("", "hmdsh-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	return home
}

// dshDesktopSession writes the owner's session "dsh-owners" (one turn,
// uncompressed) and returns its log.
func dshDesktopSession(t *testing.T, home string) string {
	t.Helper()
	cwd := filepath.Join(home, "work", "app")
	key := "--" + strings.Trim(strings.NewReplacer("/", "-", `\`, "-", ":", "-").Replace(cwd), "-") + "--"
	dir := filepath.Join(home, "sessions", key, "dsh-owners")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	text := func(s string) []any { return []any{map[string]any{"type": "text", "text": s}} }
	lines := []any{
		map[string]any{"type": "session", "version": 4, "id": "dsh-owners", "createdAt": now, "cwd": cwd, "isSeeded": false, "delegationDepth": 0},
		map[string]any{"type": "turn/start", "seq": 0, "time": now, "data": map[string]any{"turn": 1}},
		map[string]any{"type": "user/message", "seq": 1, "time": now, "surfaceOp": "append", "data": map[string]any{"id": "m1", "role": "user", "content": text("看一下 README"), "source": map[string]any{"kind": "user"}}},
		map[string]any{"type": "assistant/message", "seq": 2, "time": now, "surfaceOp": "append", "data": map[string]any{"message": map[string]any{"id": "a1", "role": "assistant", "content": text("README 写的是安装步骤。"), "source": map[string]any{"kind": "model"}}}},
		map[string]any{"type": "turn/end", "seq": 3, "time": now, "data": map[string]any{"turn": 1, "reason": map[string]any{"kind": "completed"}}},
	}
	var b []byte
	for _, l := range lines {
		j, _ := json.Marshal(l)
		b = append(append(b, j...), '\n')
	}
	log := filepath.Join(dir, "session.v4.jsonl")
	if err := os.WriteFile(log, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return log
}

// dshDesktopStoppedSession writes the owner's session "dsh-stopped" (工单
// #171): a turn stopped by its owner, one cut short by DSH's process
// exiting, and the turn DSH ran on with once it started again.
func dshDesktopStoppedSession(t *testing.T, home string) {
	t.Helper()
	cwd := filepath.Join(home, "work", "app")
	key := "--" + strings.Trim(strings.NewReplacer("/", "-", `\`, "-", ":", "-").Replace(cwd), "-") + "--"
	dir := filepath.Join(home, "sessions", key, "dsh-stopped")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-time.Hour).UnixMilli()
	text := func(s string) []any { return []any{map[string]any{"type": "text", "text": s}} }
	user := func(seq int, id, s string) any {
		return map[string]any{"type": "user/message", "seq": seq, "time": at, "surfaceOp": "append", "data": map[string]any{"id": id, "role": "user", "content": text(s), "source": map[string]any{"kind": "user"}}}
	}
	lines := []any{
		map[string]any{"type": "session", "version": 4, "id": "dsh-stopped", "createdAt": at, "cwd": cwd, "isSeeded": false, "delegationDepth": 0},
		map[string]any{"type": "turn/start", "seq": 0, "time": at, "data": map[string]any{"turn": 1}},
		user(1, "m1", "跑一遍全部测试"),
		map[string]any{"type": "turn/end", "seq": 2, "time": at, "data": map[string]any{"turn": 1, "reason": map[string]any{"kind": "aborted", "reason": map[string]any{"kind": "user"}}}},
		map[string]any{"type": "turn/start", "seq": 3, "time": at, "data": map[string]any{"turn": 2}},
		user(4, "m2", "只跑单测"),
		map[string]any{"type": "turn/end", "seq": 5, "time": at, "data": map[string]any{"turn": 2, "reason": map[string]any{"kind": "interrupted"}}},
		map[string]any{"type": "turn/start", "seq": 6, "time": at, "data": map[string]any{"turn": 3}},
		map[string]any{"type": "assistant/message", "seq": 7, "time": at, "surfaceOp": "append", "data": map[string]any{"message": map[string]any{"id": "a3", "role": "assistant", "content": text("单测都过了。"), "source": map[string]any{"kind": "model"}}}},
		map[string]any{"type": "turn/end", "seq": 8, "time": at, "data": map[string]any{"turn": 3, "reason": map[string]any{"kind": "completed"}}},
	}
	var b []byte
	for _, l := range lines {
		j, _ := json.Marshal(l)
		b = append(append(b, j...), '\n')
	}
	if err := os.WriteFile(filepath.Join(dir, "session.v4.jsonl"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// startDSHHost runs the bridge plugin for home in a stand-in DSH Host
// writing prompts into log, until the test ends, and returns its pid once
// the bridge listens.
func startDSHHost(t *testing.T, home, log string) int {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	entry, _ := filepath.Abs(filepath.Join("apps", "dsh-bridge", "src", "index.ts"))
	script, _ := filepath.Abs(filepath.Join("e2e", "testdata", "dsh-bridge-host.mjs"))
	cmd := exec.Command(node, script, entry)
	cmd.Env = append(os.Environ(), "DSH_HOME="+home, "FAKE_DSH_LOG="+log)
	cmd.Stderr = os.Stderr
	stdin, _ := cmd.StdinPipe() // closed when the test ends: the plugin is disposed
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stdin.Close(); cmd.Process.Kill(); cmd.Wait() })
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if _, err := os.Stat(filepath.Join(dshnative.BridgeDir(home), "bridge.json")); err == nil {
			return cmd.Process.Pid
		}
	}
	t.Fatal("the bridge plugin did not start")
	return 0
}
