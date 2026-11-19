package dshnative

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sessionv1 "github.com/hermeshai/hermesh/gen/go/hermesh/session/v1"
)

// A prompt Hermesh sent through the bridge (its rpcId starts "hermesh-")
// is the owner's; one typed in DSH itself, or another tool's, names no one.
func TestUserMessageFromHermesh(t *testing.T) {
	home := t.TempDir()
	viaHermesh := ev("user/message", map[string]any{"id": "m2", "role": "user", "content": text("from Hermesh"),
		"source": map[string]any{"kind": "user", "rpcId": "hermesh-01abc"}}, "surfaceOp", "append")
	viaOther := ev("user/message", map[string]any{"id": "m3", "role": "user", "content": text("from the web page"),
		"source": map[string]any{"kind": "user", "rpcId": "web-7"}}, "surfaceOp", "append")
	writeSession(t, home, map[string]any{"id": "s"}, true, events(0, userMsg("typed in DSH"), viaHermesh, viaOther))
	_, evs, err := Read(home, "s")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range evs {
		if m, ok := e.Kind.(*sessionv1.SessionEvent_UserMessage); ok {
			got = append(got, m.UserMessage.Text+"|"+m.UserMessage.From)
		}
	}
	want := []string{"typed in DSH|", "from Hermesh|owner", "from the web page|"}
	if len(got) != len(want) {
		t.Fatalf("%v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: %q, want %q", i, got[i], want[i])
		}
	}
	if _, _, err := Held(home, "nope"); err != ErrNotFound {
		t.Errorf("Held of an unknown session: %v", err)
	}
	if held, _, err := Held(home, "s"); err != nil || held {
		t.Errorf("Held of a session nothing has open: %v %v", held, err)
	}
}

// Preparing: DSH's configuration backed up (not its sessions or packages),
// the plugin's package written; nothing DSH reads changes.
func TestPrepareBridge(t *testing.T) {
	home := t.TempDir()
	for name, body := range map[string]string{
		"cordis.patch.yml":                       "a: 1\n",
		"settings.json":                          "{}",
		"profiles/desktop/package.json":          `{"dsh":{"profile":{"bundles":[]}}}`,
		"profiles/desktop/cordis.patch.yml":      "- insert: []\n",
		"profiles/desktop/node_modules/x/a.json": "{}",
		"profiles/acp/package.json":              "{}",
		"sessions/p/s/session.v4.jsonl":          "{}\n",
	} {
		os.MkdirAll(filepath.Dir(filepath.Join(home, name)), 0o700)
		os.WriteFile(filepath.Join(home, name), []byte(body), 0o600)
	}
	before := snapshot(t, home)
	p, err := PrepareBridge(home, time.Date(2026, 11, 19, 6, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"cordis.patch.yml", "profiles/acp/package.json", "profiles/desktop/cordis.patch.yml", "profiles/desktop/package.json", "settings.json"}
	if !p.DesktopProfile || strings.Join(p.BackedUp, ",") != filepath.FromSlash(strings.Join(want, ",")) {
		t.Fatalf("backed up %v (desktop profile %v)", p.BackedUp, p.DesktopProfile)
	}
	if b, _ := os.ReadFile(filepath.Join(p.Backup, "profiles", "desktop", "package.json")); string(b) != `{"dsh":{"profile":{"bundles":[]}}}` {
		t.Fatalf("backup: %q", b)
	}
	if st, err := os.Stat(p.Package); err != nil || st.Size() == 0 || !strings.HasSuffix(p.Package, ".tgz") || filepath.Dir(p.Package) != BridgeDir(home) {
		t.Fatalf("package %s: %v", p.Package, err)
	}
	// What DSH reads is as it was (only hermesh-bridge/ is new).
	after := snapshot(t, home)
	for k, v := range before {
		if k != home && after[k] != v { // the home itself gained hermesh-bridge/
			t.Errorf("%s changed", k)
		}
	}
	for k := range after {
		if _, ok := before[k]; !ok && !strings.HasPrefix(k, BridgeDir(home)) {
			t.Errorf("new file %s", k)
		}
	}
	if _, err := PrepareBridge(filepath.Join(home, "nope"), time.Now()); err == nil {
		t.Error("no DSH home: no error")
	}
}
