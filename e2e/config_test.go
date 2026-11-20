package e2e

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
	relayv1 "github.com/samir2015dzh/aide/gen/go/aide/relay/v1"
	"github.com/samir2015dzh/aide/gen/go/aide/relay/v1/relayv1connect"
	"github.com/samir2015dzh/aide/pkg/identity"
)

// TestConfigExportApply walks a machine's configuration as a file
// (borrowed item 17, docs/machine-ops.md §6) with real binaries: the owner,
// in a shell on a server, exports its workspace roots and projects (through
// its running service); a dry run on a clean server changes nothing; apply
// restores them there (its service stopped), and its export is the same
// file; applying again changes nothing; an agent then works in the
// restored project. Agent defaults the owner set from the app are in the
// file, but a shell applies none: it names them, refused with kind denied.
func TestConfigExportApply(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	env := []string{"AIDE_FAKE_PROVIDERS=1"}
	w := newOwnerMac(t, env, &localv1.GrantSpec{})
	ctx := context.Background()
	daemon := filepath.Join(w.bin, "aided")
	aided := func(data string, args ...string) result {
		return run(t, nil, time.Minute, daemon, append(args, "--data-dir", data)...)
	}
	online := func(name string) {
		eventually(t, 30*time.Second, name+" online", func() bool {
			res, err := w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
			return err == nil && slices.ContainsFunc(res.Machines, func(m *localv1.Machine) bool { return m.MachineName == name && m.Online })
		})
	}
	root := filepath.Join(t.TempDir(), "code")
	os.MkdirAll(filepath.Join(root, "notes"), 0o755)
	gitRepoAt(t, filepath.Join(root, "app"), map[string]string{"README.md": "# app\n"})

	// The first server, set up by hand, its service running.
	old, fresh := filepath.Join(w.tmp, "old"), filepath.Join(w.tmp, "fresh")
	oldID := w.loginMachine("old", old, env)
	w.logs = append(w.logs, start(t, "old", env, daemon, "run", "--data-dir", old))
	online("old")
	if r := aided(old, "roots", "set", root); r.code != 0 {
		t.Fatalf("roots set: %+v", r)
	}
	if r := aided(old, "project", "add", filepath.Join(root, "app"), "--agents", "claude"); r.code != 0 {
		t.Fatalf("project add app: %+v", r)
	}
	if r := aided(old, "project", "add", filepath.Join(root, "notes"), "--in-place"); r.code != 0 {
		t.Fatalf("project add notes: %+v", r)
	}
	exported := aided(old, "config", "export")
	if exported.code != 0 || !strings.Contains(exported.stdout, "id: app") || !strings.Contains(exported.stdout, "workspace: in_place") ||
		!strings.Contains(exported.stdout, "- claude") || !strings.Contains(exported.stdout, "workspace_roots:") {
		t.Fatalf("export: %+v", exported)
	}
	file := filepath.Join(t.TempDir(), "aide-machine.yaml")
	os.WriteFile(file, []byte(exported.stdout), 0o644)

	// A clean server: a dry run says what would change, and changes nothing.
	w.loginMachine("fresh", fresh, env)
	before := aided(fresh, "config", "export")
	dry := aided(fresh, "config", "apply", "-f", file, "--dry-run")
	if dry.code != 0 || !strings.Contains(dry.stdout, "projects app (would add)") || !strings.Contains(dry.stdout, "workspace_roots (would update)") ||
		!strings.Contains(dry.stdout, "nothing was changed") {
		t.Fatalf("dry run: %+v", dry)
	}
	if r := aided(fresh, "config", "export"); r.stdout != before.stdout {
		t.Fatalf("the dry run changed the machine:\n%s", r.stdout)
	}

	// Applied: the same file comes out; again, nothing changes.
	applied := aided(fresh, "config", "apply", "-f", file, "-o", "json")
	doc := jsonDoc(t, applied)
	ca, _ := doc["config_apply"].(map[string]any)
	if changes, _ := ca["changes"].([]any); applied.code != 0 || doc["ok"] != true || len(changes) != 3 || changes[1].(map[string]any)["applied"] != true {
		t.Fatalf("apply: %+v", applied)
	}
	if r := aided(fresh, "config", "export"); r.stdout != exported.stdout {
		t.Fatalf("exported after apply:\n%s\nwant:\n%s", r.stdout, exported.stdout)
	}
	if r := aided(fresh, "config", "apply", "-f", file); r.code != 0 || !strings.Contains(r.stdout, "nothing to change") {
		t.Fatalf("applied again: %+v", r)
	}

	// An agent works in the restored project.
	w.logs = append(w.logs, start(t, "fresh", env, daemon, "run", "--data-dir", fresh))
	online("fresh")
	if r := w.cli(time.Minute, "session", "start", "--project", "fresh/notes", "--wait", "--prompt", "say restored", "-o", "json"); r.code != 0 || firstSession(r.stdout).SessionID == "" {
		t.Fatalf("a session in the restored project: %+v", r)
	}

	// Agent defaults from the app are exported; a shell applies none.
	if _, err := w.app.client.SetAgentDefaults(ctx, &localv1.SetAgentDefaultsRequest{MachineId: oldID, Defaults: &identityv1.AgentDefaults{Agent: "claude", Model: "opus"}}); err != nil {
		t.Fatal(err)
	}
	withDefaults := aided(old, "config", "export")
	if !strings.Contains(withDefaults.stdout, "agent_defaults:") || !strings.Contains(withDefaults.stdout, "model: opus") {
		t.Fatalf("export with agent defaults: %+v", withDefaults)
	}
	os.WriteFile(file, []byte(withDefaults.stdout), 0o644)
	refused := aided(fresh, "config", "apply", "-f", file, "-o", "json")
	doc = jsonDoc(t, refused)
	e := errOf(doc)
	if refused.code != 3 || e["kind"] != "denied" || !strings.Contains(e["hint"].(string), "Aide app") || !strings.Contains(e["message"].(string), "agent_defaults claude") {
		t.Fatalf("agent defaults from a shell: %+v", refused)
	}
	if r := aided(fresh, "config", "export"); strings.Contains(r.stdout, "model: opus") {
		t.Fatalf("a shell changed the agent defaults:\n%s", r.stdout)
	}
	if r := aided(fresh, "config", "apply", "-f", file); r.code != 3 || !strings.Contains(r.stderr, "Hint: Set them in the Aide app") {
		t.Fatalf("text: %+v", r)
	}
}

// TestSetupCodeExpires: a new server's setup code lasts what aide-relay
// serve --setup-code-ttl says. Expired, the app's 设置新的服务器 is refused
// with what to do; aide-relay setup-code prints a new one, the old one is
// refused still, and the new one sets the server up. A server set up has
// no setup code any more, and its owner logs in as before after a restart.
func TestSetupCodeExpires(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	bin, tmp := binaries(t), t.TempDir()
	ctx := context.Background()
	relayBin, relayData := filepath.Join(bin, "aide-relay"), filepath.Join(tmp, "relay")
	port := freePort(t)
	serve := []string{"serve", "--data-dir", relayData, "--listen", fmt.Sprintf("127.0.0.1:%d", port), "--setup-code-ttl", "3s"}
	relay := start(t, "relay", nil, relayBin, serve...)
	eventually(t, 20*time.Second, "relay up", func() bool { return strings.Contains(relay.output(), "relay listening") })
	if !strings.Contains(relay.output(), "valid until") {
		t.Fatalf("the relay did not say until when its setup code is taken:\n%s", relay.output())
	}
	setupCode := func() string {
		r := run(t, nil, 10*time.Second, relayBin, "setup-code", "--data-dir", relayData, "--setup-code-ttl", "3s")
		if r.code != 0 {
			t.Fatalf("setup-code: %+v", r)
		}
		return strings.TrimSpace(r.stdout)
	}
	stale := setupCode()
	time.Sleep(4 * time.Second)

	sockDir, _ := os.MkdirTemp(shortTemp, "hm")
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	sock := filepath.Join(sockDir, "o.sock")
	start(t, "owner", nil, filepath.Join(bin, "aided"), "run", "--owner", "--data-dir", filepath.Join(tmp, "owner"), "--socket", sock)
	app := newTestApp(t, sock)
	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	setUp := func(code string) error {
		_, err := app.client.Setup(ctx, &localv1.SetupRequest{RelayUrl: url, SetupCode: code, Account: testAccount, Password: testPassword, Label: "test-mac"})
		return err
	}
	if err := setUp(stale); err == nil || !strings.Contains(err.Error(), "expired") || !strings.Contains(err.Error(), "aide-relay setup-code") {
		t.Fatalf("an expired setup code: %v", err)
	}
	next := setupCode()
	if next == stale || next == "" {
		t.Fatalf("no new setup code: %q", next)
	}
	if err := setUp(stale); err == nil {
		t.Fatal("the old setup code set the server up")
	}
	if err := setUp(next); err != nil {
		t.Fatalf("the new setup code: %v", err)
	}
	if r := run(t, nil, 10*time.Second, relayBin, "setup-code", "--data-dir", relayData, "--setup-code-ttl", "3s"); strings.TrimSpace(r.stdout) != "already set up" {
		t.Fatalf("setup-code once set up: %+v", r)
	}

	// Set up, the time passing and a restart change nothing for the owner.
	relay.stop()
	time.Sleep(4 * time.Second)
	relay = start(t, "relay", nil, relayBin, serve...)
	eventually(t, 20*time.Second, "relay up again", func() bool { return strings.Contains(relay.output(), "relay listening") })
	if strings.Contains(relay.output(), "Setup code") {
		t.Fatalf("a server set up printed a setup code:\n%s", relay.output())
	}
	k, _ := identity.GenerateKey()
	j := &relayv1.Joiner{Kind: relayv1.JoinKind_JOIN_KIND_AGENT, PublicKey: identity.MarshalPublicKey(k.Public()), Label: "agent"}
	if _, err := relayv1connect.NewRelayPublicServiceClient(http.DefaultClient, url).LogIn(ctx, &relayv1.LogInRequest{Account: testAccount, Password: testPassword, Joiner: j}); err != nil {
		t.Fatalf("logging in after the setup code's time and a restart: %v", err)
	}
}
