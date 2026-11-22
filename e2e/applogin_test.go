package e2e

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
)

// Work order #39: on a computer whose Aide app is logged in, the CLI logs
// in through the app — from a terminal with nothing to type, and from the
// app's own button — with no account or password; it is an agent of its
// own, listed in the app under the name the CLI shows. Logging it out (the
// button, or aide auth logout) revokes it; the app's own login is not
// touched. No password lands in the CLI's files.
func TestCLILogsInThroughTheApp(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	appCLI := t.TempDir() // the CLI home the app's button logs in
	w := newOwnerMac(t, []string{"AIDE_FAKE_PROVIDERS=1", "AIDE_HOME=" + appCLI}, &localv1.GrantSpec{})
	ctx := context.Background()
	aide := filepath.Join(w.bin, "aide")
	clients := func() map[string]*localv1.Client {
		out := map[string]*localv1.Client{}
		res, err := w.app.client.ListClients(ctx, &localv1.ListClientsRequest{})
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range res.Clients {
			out[c.ClientId] = c
		}
		return out
	}
	type status struct {
		Auth struct {
			Identity *struct {
				Client   string `json:"client"`
				ClientID string `json:"client_id"`
			}
		}
	}
	readStatus := func(env []string) status {
		var s status
		r := run(t, env, time.Minute, aide, "auth", "status", "-o", "json")
		json.Unmarshal([]byte(r.stdout), &s)
		return s
	}

	// From a terminal: `aide auth login`, nothing else, no account, no password.
	term := []string{"AIDE_HOME=" + t.TempDir(), "AIDE_APP_SOCKET=" + w.sock}
	r := run(t, term, time.Minute, aide, "auth", "login", "--name", "desk CLI")
	if r.code != 0 || !strings.Contains(r.stdout, "through the Aide app on this computer") {
		t.Fatalf("login from a terminal: %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	s := readStatus(term)
	if s.Auth.Identity == nil || s.Auth.Identity.Client != "desk CLI" {
		t.Fatalf("auth status after: %+v", s)
	}
	if c := clients()[s.Auth.Identity.ClientID]; c == nil || c.Label != "desk CLI" || c.Revoked {
		t.Fatalf("the app's list: %+v", c)
	}
	if p := run(t, term, time.Minute, aide, "project", "list"); p.code != 0 || !strings.Contains(p.stdout, "demo") {
		t.Fatalf("project list: %d\n%s%s", p.code, p.stdout, p.stderr)
	}
	// Logging out revokes it in the app too.
	if r := run(t, term, time.Minute, aide, "auth", "logout"); r.code != 0 || !strings.Contains(r.stdout, "the Aide app revoked") {
		t.Fatalf("logout: %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	eventually(t, 30*time.Second, "revoked in the app", func() bool { c := clients()[s.Auth.Identity.ClientID]; return c == nil || c.Revoked })

	// From the app's button: the CLI beside aided, in the user's CLI home.
	cli, err := w.app.client.GetLocalCLI(ctx, &localv1.GetLocalCLIRequest{})
	if err != nil || !cli.Found || cli.LoggedIn {
		t.Fatalf("before: %+v %v", cli, err)
	}
	cli, err = w.app.client.LogInLocalCLI(ctx, &localv1.LogInLocalCLIRequest{})
	if err != nil || !cli.LoggedIn || !cli.SameOwner || cli.GrantId == "" || !strings.HasPrefix(cli.Label, "client@") {
		t.Fatalf("logged in from the app: %+v %v\n%s", cli, err, w.owner.output())
	}
	byCLI := readStatus([]string{"AIDE_HOME=" + appCLI})
	if byCLI.Auth.Identity == nil || byCLI.Auth.Identity.Client != cli.Label {
		t.Fatalf("the CLI says %+v, the app %q", byCLI.Auth.Identity, cli.Label)
	}
	// Again: already logged in, nothing new.
	if again, err := w.app.client.LogInLocalCLI(ctx, &localv1.LogInLocalCLIRequest{}); err != nil || again.ClientId != cli.ClientId {
		t.Fatalf("again: %+v %v", again, err)
	}
	for _, f := range []string{"state.json", "client.key"} {
		b, _ := os.ReadFile(filepath.Join(appCLI, f))
		if len(b) == 0 || strings.Contains(string(b), testPassword) {
			t.Fatalf("%s: %d bytes, password in it: %v", f, len(b), strings.Contains(string(b), testPassword))
		}
	}
	out, err := w.app.client.LogOutLocalCLI(ctx, &localv1.LogOutLocalCLIRequest{})
	if err != nil || out.LoggedIn {
		t.Fatalf("logged out from the app: %+v %v", out, err)
	}
	eventually(t, 30*time.Second, "revoked in the app", func() bool { c := clients()[cli.ClientId]; return c == nil || c.Revoked })
	// The app itself is still logged in.
	if st, err := w.app.client.GetStatus(ctx, &localv1.GetStatusRequest{}); err != nil || st.SignedOut || st.OwnerId == "" {
		t.Fatalf("the app after: %+v %v", st, err)
	}
	if strings.Contains(w.owner.output(), testPassword) {
		t.Fatal("the password is in aided's log")
	}
}
