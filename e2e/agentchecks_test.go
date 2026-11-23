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
	"github.com/samir2015dzh/aide/internal/provider"
	"github.com/samir2015dzh/aide/internal/shellenv"
)

// TestAgentsCheckedOnDemand: this Mac's coding agents are checked (each CLI
// run) only when something asks, never in the background: the app reading
// its state checks them the first time, polling does not check them again,
// and logging in (e.g. `codex login` run from the app) shows once the app
// asks for a new check.
func TestAgentsCheckedOnDemand(t *testing.T) {
	unixOnly(t, "its fake codex is a shell script")
	if testing.Short() {
		t.Skip("e2e")
	}
	dir := t.TempDir()
	codex := filepath.Join(dir, "codex")
	runs := filepath.Join(dir, "runs")
	write := func(loggedIn bool) {
		login := `echo "Not logged in"; exit 1`
		if loggedIn {
			login = `echo "Logged in using ChatGPT"`
		}
		os.WriteFile(codex+".new", []byte("#!/bin/sh\necho \"$1\" >> "+runs+"\ncase \"$1\" in --version) echo \"codex-cli 0.48.0\";; login) "+login+";; esac\n"), 0o755)
		os.Rename(codex+".new", codex)
	}
	write(false)
	w := newOwnerMac(t, []string{"AIDE_CODEX_BIN=" + codex}, &localv1.GrantSpec{Agents: []string{"codex"}})
	ctx := context.Background()
	loggedIn := func() (bool, bool) {
		res, err := w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
		if err != nil || len(res.Machines) == 0 {
			return false, false
		}
		for _, a := range res.Machines[0].Status.GetAgents() {
			if a.Name == "codex" {
				return a.Available, a.LoggedIn
			}
		}
		return false, false
	}
	count := func() int {
		b, _ := os.ReadFile(runs)
		return strings.Count(string(b), "--version")
	}
	eventually(t, 30*time.Second, "codex installed, not logged in", func() bool { ok, in := loggedIn(); return ok && !in })

	// Polled as the app polls, and logged in meanwhile: not checked again.
	checks := count()
	write(true)
	for range 6 {
		if _, in := loggedIn(); in {
			t.Fatal("checked again without anything asking")
		}
		time.Sleep(500 * time.Millisecond)
	}
	if count() != checks {
		t.Fatalf("checked %d times in the background", count()-checks)
	}

	// The app asks (the page waiting for the sign-in): it shows at once.
	if _, err := w.app.client.CheckAgents(ctx, &localv1.CheckAgentsRequest{}); err != nil {
		t.Fatal(err)
	}
	eventually(t, 10*time.Second, "codex logged in", func() bool { _, in := loggedIn(); return in })
}

// TestClaudeFromDesktopApp: someone with only the Claude desktop app (no
// CLI on PATH) has Claude Code all the same, the copy the app keeps for
// itself; the newest one shows as this Mac's Claude Code.
func TestClaudeFromDesktopApp(t *testing.T) {
	unixOnly(t, "its fake Claude Code is a shell script")
	if testing.Short() {
		t.Skip("e2e")
	}
	// A Claude Code the machine finds first (on the login shell's PATH, as
	// aided looks, or where Homebrew puts it) is the one it shows.
	if p := provider.LookPath("claude", shellenv.Path(), "/opt/homebrew/bin/claude", "/usr/local/bin/claude"); p != "" {
		t.Skipf("%s is installed, and is found first", p)
	}
	home := t.TempDir()
	for _, v := range []string{"2.1.99", "2.1.299"} {
		dir := filepath.Join(home, "Library/Application Support/Claude/claude-code", v, "4fa9694b7975/claude.app/Contents/MacOS")
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "claude"), []byte("#!/bin/sh\ncase \"$1\" in --version) echo \""+v+" (Claude Code)\";; auth) echo '{\"loggedIn\":true}';; esac\n"), 0o755)
	}
	w := newOwnerMac(t, []string{"HOME=" + home, "PATH=/usr/bin:/bin", "SHELL=/bin/sh"}, &localv1.GrantSpec{Agents: []string{"claude"}})
	ctx := context.Background()
	eventually(t, 30*time.Second, "Claude Code 2.1.299, logged in", func() bool {
		res, err := w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
		if err != nil || len(res.Machines) == 0 {
			return false
		}
		for _, a := range res.Machines[0].Status.GetAgents() {
			if a.Name == "claude" {
				return a.Available && a.LoggedIn && a.Version == "2.1.299"
			}
		}
		return false
	})
}

// TestClaudeSignedInInDesktopApp: the owner uses only the Claude desktop
// app, signed in there; its Claude Code has no sign-in of its own (`auth
// status` says logged out). The two are told apart (工单 #44): the app
// shows signed in (read from its settings, its data folder found as the app
// keeps it on Windows, here an MSIX package's Claude-Data\claude_<id>), the
// command line Aide runs does not, since its sessions would fail "Not
// logged in". Signed out in the app, the app shows so once checked again.
func TestClaudeSignedInInDesktopApp(t *testing.T) {
	unixOnly(t, "its fake Claude Code is a shell script")
	if testing.Short() {
		t.Skip("e2e")
	}
	home, local := t.TempDir(), t.TempDir()
	claude := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(claude, []byte("#!/bin/sh\ncase \"$1\" in --version) echo \"2.1.326 (Claude Code)\";; auth) echo '{\"loggedIn\":false}';; esac\n"), 0o755)
	data := filepath.Join(local, "Claude-Data", "claude_somepub")
	os.MkdirAll(filepath.Join(data, "claude-code", "2.1.326"), 0o755)
	config := func(signedIn bool) {
		b, _ := json.Marshal(map[string]any{"oauth:tokenCacheV2": "opaque", "lastKnownAccountUuid": "00000000-0000-4000-8000-000000000000", "windowSizeWasSignedIn": signedIn})
		if err := os.WriteFile(filepath.Join(data, "config.json"), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	config(true)
	w := newOwnerMac(t, []string{"AIDE_CLAUDE_BIN=" + claude, "HOME=" + home, "LOCALAPPDATA=" + local}, &localv1.GrantSpec{Agents: []string{"claude"}})
	ctx := context.Background()
	// Found, the command line signed in, the app signed in, and when checked.
	state := func() (bool, bool, bool, bool) {
		res, err := w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
		if err != nil || len(res.Machines) == 0 {
			return false, false, false, false
		}
		for _, a := range res.Machines[0].Status.GetAgents() {
			if a.Name == "claude" {
				return a.Available, a.LoggedIn, a.GetDesktop().GetSignedIn(), a.CheckedAt != nil
			}
		}
		return false, false, false, false
	}
	eventually(t, 30*time.Second, "the app signed in, its command line not", func() bool {
		ok, cli, app, checked := state()
		return ok && !cli && app && checked
	})

	config(false)
	if _, err := w.app.client.CheckAgents(ctx, &localv1.CheckAgentsRequest{}); err != nil {
		t.Fatal(err)
	}
	eventually(t, 30*time.Second, "the app signed out", func() bool { ok, cli, app, _ := state(); return ok && !cli && !app })
}
