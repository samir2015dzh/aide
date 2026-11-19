package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	localv1 "github.com/hermeshai/hermesh/gen/go/hermesh/local/v1"
	"github.com/hermeshai/hermesh/internal/connaddr"
	"github.com/hermeshai/hermesh/pkg/identity"
)

// TestAuth walks an agent's pairing over its life with real binaries:
// checking it, the owner changing and revoking the grant, the relay going
// away, asking again without any flag, another owner's address, and
// forgetting the pairing.
func TestAuth(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	w := newOwnerMac(t, []string{"HERMESH_FAKE_PROVIDERS=1"}, &localv1.GrantSpec{})
	ctx := context.Background()
	bin := filepath.Join(w.bin, "hermesh")
	cli := func(env []string, args ...string) result { return run(t, env, time.Minute, bin, args...) }
	has := func(r result, what string, want ...string) {
		t.Helper()
		for _, s := range want {
			if !strings.Contains(r.stdout+r.stderr, s) {
				t.Fatalf("%s: no %q in\n%s%s", what, s, r.stdout, r.stderr)
			}
		}
	}
	home := filepath.Join(w.tmp, "agent")
	fresh := []string{"HERMESH_HOME=" + filepath.Join(w.tmp, "fresh")}

	// Not paired: status says so and how to pair; logout has nothing to do.
	r := cli(fresh, "auth", "status")
	if r.code != 3 {
		t.Fatalf("status, not paired: %+v", r)
	}
	has(r, "status, not logged in", "✗ identity", "not logged in", "aide auth login", "- relay")
	st := jsonDoc(t, cli(fresh, "auth", "status", "-o", "json"))
	if st["ok"] != false || check(st, "identity") != "fail" || check(st, "relay") != "skipped" {
		t.Fatalf("status json, not paired: %v", st)
	}
	if r := cli(fresh, "auth", "logout"); r.code != 0 {
		t.Fatalf("logout, not paired: %+v", r)
	} else {
		has(r, "logout, not logged in", "Not logged in")
	}
	if r := cli(fresh, "auth", "login", w.addr, "--name", strings.Repeat("x", 65)); r.code != 2 {
		t.Fatalf("long name: %+v", r)
	}

	// Paired: every check passes, in words a person reads.
	r = cli(w.cliEnv, "auth", "status")
	if r.code != 0 {
		t.Fatalf("status: %+v", r)
	}
	// Approved without a lifetime, the grant doesn't expire (WO #3).
	has(r, "status", "✓ identity", `client "bot"`, "✓ grant", "all machines", "doesn't expire", "✓ relay", "✓ grant sync", "up to date")
	if strings.Contains(r.stdout, "expires in") {
		t.Fatalf("a long-lived grant counts days:\n%s", r.stdout)
	}
	st = jsonDoc(t, cli(w.cliEnv, "auth", "status", "-o", "json"))
	if st["ok"] != true || st["exit_code"] != float64(0) || field(st, "identity", "connect_address") != w.addr || field(st, "identity", "client") != "bot" {
		t.Fatalf("status json: %v", st)
	}

	// Logging in again checks with the relay and does nothing.
	r = cli(w.cliEnv, "auth", "login", w.addr)
	if r.code != 0 || strings.Contains(r.stdout, "Pairing code") {
		t.Fatalf("login again: %+v", r)
	}
	has(r, "login again", "Already logged in")
	// The old spelling still works but is not offered; whoami and --force are gone.
	if r := cli(w.cliEnv, "login", w.addr); r.code != 0 || !strings.Contains(r.stdout, "Already logged in") {
		t.Fatalf("old login: %+v", r)
	}
	help := cli(w.cliEnv, "--help")
	if !strings.Contains(help.stdout, "auth") || strings.Contains(help.stdout, "whoami") || strings.Contains(help.stdout, "\n  login") {
		t.Fatalf("help: %s", help.stdout)
	}
	if r := cli(w.cliEnv, "whoami"); r.code != 2 {
		t.Fatalf("whoami: %+v", r)
	}
	if r := cli(w.cliEnv, "auth", "login", w.addr, "--force"); r.code != 2 {
		t.Fatalf("--force: %+v", r)
	}

	// The owner narrows the grant and shortens it: status picks the new
	// one up, and warns that it ends soon.
	clients, err := w.app.client.ListClients(ctx, &localv1.ListClientsRequest{})
	if err != nil || len(clients.Clients) != 1 {
		t.Fatalf("clients: %v %v", clients, err)
	}
	bot := clients.Clients[0]
	if _, err := w.app.client.UpdateClient(ctx, &localv1.UpdateClientRequest{GrantId: bot.GrantId,
		Spec: &localv1.GrantSpec{Label: "bot", TtlDays: 3}}); err != nil {
		t.Fatal(err)
	}
	eventually(t, 30*time.Second, "updated grant", func() bool {
		r = cli(w.cliEnv, "auth", "status")
		return strings.Contains(r.stdout, "updated v1 → v2")
	})
	if r.code != 0 {
		t.Fatalf("status, short grant: %+v", r)
	}
	has(r, "status, short grant", "! grant", "expires in 3 days", "renew")
	st = jsonDoc(t, cli(w.cliEnv, "auth", "status", "-o", "json"))
	if field(st, "grant", "version") != float64(2) || check(st, "grant") != "warn" || check(st, "grant_sync") != "ok" {
		t.Fatalf("status json, short grant: %v", st)
	}

	// The owner makes it long-lived again, as Access → Make long-lived
	// does: no new pairing, and the next status says it doesn't expire.
	if _, err := w.app.client.UpdateClient(ctx, &localv1.UpdateClientRequest{GrantId: bot.GrantId,
		Spec: &localv1.GrantSpec{Label: "bot"}}); err != nil {
		t.Fatal(err)
	}
	eventually(t, 30*time.Second, "long-lived grant", func() bool {
		r = cli(w.cliEnv, "auth", "status")
		return strings.Contains(r.stdout, "updated v2 → v3")
	})
	if r.code != 0 {
		t.Fatalf("status, long-lived again: %+v", r)
	}
	has(r, "status, long-lived again", "✓ grant", "doesn't expire")
	if cs, err := w.app.client.ListClients(ctx, &localv1.ListClientsRequest{}); err != nil || len(cs.Clients) != 1 ||
		cs.Clients[0].Spec.GetTtlDays() != 0 || !cs.Clients[0].ExpiresAt.AsTime().Equal(identity.NoExpiry) {
		t.Fatalf("listed long-lived: %v %v", cs, err)
	}
	if r := cli(w.cliEnv, "project", "list"); r.code != 0 || !strings.Contains(r.stdout, "owner-mac/demo") {
		t.Fatalf("projects with the long-lived grant: %+v", r)
	}

	// The relay goes away: what is known locally still shows.
	w.relay.stop()
	began := time.Now()
	r = cli(w.cliEnv, "auth", "status")
	if r.code != 4 || time.Since(began) > 20*time.Second {
		t.Fatalf("status, relay down (%v): %+v", time.Since(began), r)
	}
	has(r, "status, relay down", "✓ identity", "✓ grant", "doesn't expire", "✗ relay", "- grant sync")
	w.startRelay()

	// Revoked: status says so and gives the command that asks again, and
	// so does any other command.
	if _, err := w.app.client.RevokeClient(ctx, &localv1.RevokeClientRequest{GrantId: bot.GrantId}); err != nil {
		t.Fatal(err)
	}
	eventually(t, 30*time.Second, "revocation seen", func() bool {
		r = cli(w.cliEnv, "auth", "status")
		return r.code == 3
	})
	has(r, "status, revoked", "✗ grant", "revoked", "aide auth login "+w.addr)
	if strings.Contains(r.stdout, "renew") {
		t.Fatalf("a revoked grant is not to be renewed:\n%s", r.stdout)
	}
	if r := cli(w.cliEnv, "project", "list"); r.code != 3 || !strings.Contains(r.stderr, "aide auth login "+w.addr) {
		t.Fatalf("machines, revoked: %+v", r)
	}
	// Asking again needs no flag: login sees the revocation and pairs anew.
	login := start(t, "login again", append(slices.Clone(w.cliEnv), loginEnv()...), bin, "auth", "login", w.addr)
	if c := login.wait(t, 30*time.Second); c != 0 || !strings.Contains(login.output(), "aide project list") {
		t.Fatalf("login after revocation: %s", login.output())
	}
	// The machine reconnects to the restarted relay on its own.
	eventually(t, 30*time.Second, "projects after pairing again", func() bool {
		r := cli(w.cliEnv, "project", "list")
		return r.code == 0 && strings.Contains(r.stdout, "owner-mac/demo")
	})
	if r := cli(w.cliEnv, "auth", "status"); r.code != 0 || !strings.Contains(r.stdout, "✓ grant") {
		t.Fatalf("status after pairing again: %+v", r)
	}

	// Another owner's address is refused while paired with this one.
	relayURL, _, _ := connaddr.Parse(w.addr)
	r = cli(w.cliEnv, "auth", "login", connaddr.Format(relayURL, "o_"+strings.Repeat("a", 24)))
	if r.code != 2 {
		t.Fatalf("login, other owner: %+v", r)
	}
	has(r, "login, other owner", "already logged in", "aide auth logout")

	// Logout forgets the key and the grant, and says the owner still lists
	// the agent until they revoke it.
	r = cli(w.cliEnv, "auth", "logout")
	if r.code != 0 {
		t.Fatalf("logout: %+v", r)
	}
	has(r, "logout", "bot", "revoke")
	for _, f := range []string{"client.key", "state.json"} {
		if _, err := os.Stat(filepath.Join(home, f)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s after logout: %v", f, err)
		}
	}
	if r := cli(w.cliEnv, "project", "list"); r.code != 3 || !strings.Contains(r.stderr, "aide auth login") {
		t.Fatalf("machines after logout: %+v", r)
	}
}

// jsonDoc parses stdout as exactly one JSON document whose exit_code, if
// any, is the process's.
func jsonDoc(t *testing.T, r result) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(r.stdout))
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%+v", err, r)
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		t.Fatalf("stdout has more than one JSON document (%v):\n%s", err, r.stdout)
	}
	if code, ok := m["exit_code"]; ok && int(code.(float64)) != r.code {
		t.Fatalf("exit_code %v in the JSON, process exited %d", code, r.code)
	}
	return m
}

// check is the status of a named check in `auth status -o json`.
func check(doc map[string]any, name string) string {
	auth, _ := doc["auth"].(map[string]any)
	checks, _ := auth["checks"].([]any)
	for _, c := range checks {
		if c := c.(map[string]any); c["check"] == name {
			s, _ := c["status"].(string)
			return s
		}
	}
	return ""
}

// field is auth.<obj>.<key> in `auth status -o json`.
func field(doc map[string]any, obj, key string) any {
	auth, _ := doc["auth"].(map[string]any)
	o, _ := auth[obj].(map[string]any)
	return o[key]
}
