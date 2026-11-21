package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samir2015dzh/aide/internal/provider"
)

// desktopFixture writes a record the desktop app would have written, last
// changed at mod, and returns its folder.
func desktopFixture(t *testing.T, index, account, org string, mod time.Time) string {
	t.Helper()
	dir := filepath.Join(index, account, org)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, "local_"+account+org+".json")
	if err := os.WriteFile(f, []byte(`{"sessionId":"local_x","cliSessionId":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(f, mod, mod); err != nil {
		t.Fatal(err)
	}
	return dir
}

// New records go to the account and organization the desktop app wrote
// to last, wherever it keeps its data: on macOS, %APPDATA%\Claude, the
// MSIX package's copy of it, or 3p mode's %LOCALAPPDATA%\Claude-3p.
func TestDesktopAccountDir(t *testing.T) {
	t0 := time.Now().Add(-time.Hour)
	t.Run("no desktop app", func(t *testing.T) {
		if _, err := desktopAccountDir(desktopIndexes(t.TempDir(), "", "")); err == nil {
			t.Fatal("no app: a folder")
		}
		home, appData, local := t.TempDir(), t.TempDir(), t.TempDir()
		// Installed but its Code page never used: no account folder yet.
		os.MkdirAll(filepath.Join(appData, "Claude", "claude-code-sessions"), 0o755)
		os.MkdirAll(filepath.Join(home, "Library", "Application Support", "Claude", "claude-code-sessions", "acct", "org"), 0o755)
		if _, err := desktopAccountDir(desktopIndexes(home, appData, local)); err == nil {
			t.Fatal("no record: a folder")
		}
	})
	for _, c := range []struct {
		name  string
		index func(home, appData, local string) string
	}{
		{"macOS", func(home, _, _ string) string {
			return filepath.Join(home, "Library", "Application Support", "Claude", "claude-code-sessions")
		}},
		{"macOS 3p", func(home, _, _ string) string {
			return filepath.Join(home, "Library", "Application Support", "Claude-3p", "claude-code-sessions")
		}},
		{"Windows", func(_, appData, _ string) string { return filepath.Join(appData, "Claude", "claude-code-sessions") }},
		{"Windows MSIX", func(_, _, local string) string {
			return filepath.Join(local, "Packages", "Claude_pzs8sxrjxfjjc", "LocalCache", "Roaming", "Claude", "claude-code-sessions")
		}},
		{"Windows 3p", func(_, _, local string) string { return filepath.Join(local, "Claude-3p", "claude-code-sessions") }},
	} {
		t.Run(c.name, func(t *testing.T) {
			home, appData, local := t.TempDir(), t.TempDir(), t.TempDir()
			want := desktopFixture(t, c.index(home, appData, local), "acct", "org", t0)
			if got, err := desktopAccountDir(desktopIndexes(home, appData, local)); err != nil || got != want {
				t.Fatalf("got %q, %v; want %q", got, err, want)
			}
		})
	}
	t.Run("several accounts and organizations", func(t *testing.T) {
		home, appData, local := t.TempDir(), t.TempDir(), t.TempDir()
		idx := filepath.Join(appData, "Claude", "claude-code-sessions")
		desktopFixture(t, idx, "old-acct", "org", t0.Add(3*time.Minute)) // signed out since
		desktopFixture(t, idx, "acct", "org-a", t0)
		want := desktopFixture(t, idx, "acct", "org-b", t0.Add(5*time.Minute))
		desktopFixture(t, filepath.Join(local, "Claude-3p", "claude-code-sessions"), "acct3p", "org", t0.Add(time.Minute))
		if got, err := desktopAccountDir(desktopIndexes(home, appData, local)); err != nil || got != want {
			t.Fatalf("got %q, %v; want %q", got, err, want)
		}
	})
}

// The record is the one the desktop app writes when it imports a CLI
// session (testdata/desktop-record.json), with Aide's title.
func TestDesktopRecordGolden(t *testing.T) {
	r := newDesktopRecord("6f1a3c1e-6c2a-4c55-9a51-1b2f3c4d5e6f", `C:\Users\owner\work\aide`,
		desktopTitle("aide", "Muse", "Fix the login page"), time.UnixMilli(1789000000123))
	dir := t.TempDir()
	path, err := writeDesktopRecord(dir, r)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "local_6f1a3c1e-6c2a-4c55-9a51-1b2f3c4d5e6f.json" {
		t.Fatalf("file %s", path)
	}
	got, _ := os.ReadFile(path)
	want, err := os.ReadFile(filepath.Join("testdata", "desktop-record.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
		t.Fatalf("record:\n%s\nwant:\n%s", got, want)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 {
		t.Fatalf("files left beside the record: %v", ents)
	}
	// A record already there is never replaced.
	os.WriteFile(path, []byte("{}"), 0o600)
	if _, err := writeDesktopRecord(dir, r); err == nil {
		t.Fatal("replaced a record")
	}
	if b, _ := os.ReadFile(path); string(b) != "{}" {
		t.Fatalf("record changed: %s", b)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 {
		t.Fatalf("files left beside the record: %v", ents)
	}
}

func TestDesktopTitle(t *testing.T) {
	for _, c := range []struct{ project, requester, title, want string }{
		{"aide", "Muse", "Fix the login page", "aide · Muse via Aide · Fix the login page"},
		{"aide", "", "Fix it", "aide · Aide · Fix it"},
		{"", "", "", "Aide"},
	} {
		if got := desktopTitle(c.project, c.requester, c.title); got != c.want {
			t.Errorf("%+v: got %q", c, got)
		}
	}
}

// A new session Aide starts is listed in the desktop app once Claude
// Code reports it started under the id Aide gave it; a resumed one is
// already listed, and a machine without the desktop app is left alone.
func TestStartListsSessionInDesktop(t *testing.T) {
	scripted(t)
	const id = "6f1a3c1e-6c2a-4c55-9a51-1b2f3c4d5e6f"
	start := func(t *testing.T, spec provider.StartSpec) {
		t.Helper()
		bin := filepath.Join(t.TempDir(), "claude")
		script := "#!/bin/sh\nread init\nread prompt\n" +
			`echo '{"type":"system","subtype":"init","session_id":"` + id + `","model":"claude-haiku-5-5"}'` + "\n" +
			`echo '{"type":"user","message":{"role":"user","content":"hi"},"isReplay":true}'` + "\n" +
			`echo '{"type":"result","subtype":"success","result":"done"}'` + "\nsleep 1\n"
		if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		spec.Prompt, spec.Env = "hi", os.Environ()
		s, err := (&Provider{Binary: bin}).Start(context.Background(), spec)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		for e := range s.Events() {
			if e.Kind == provider.EventTurnCompleted {
				return
			}
		}
		t.Fatal("no turn result")
	}
	setup := func(t *testing.T) (work, acct string) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("APPDATA", "")
		t.Setenv("LOCALAPPDATA", "")
		work = t.TempDir()
		acct = desktopFixture(t, filepath.Join(home, "Library", "Application Support", "Claude", "claude-code-sessions"), "acct", "org", time.Now().Add(-time.Hour))
		return work, acct
	}
	t.Run("new", func(t *testing.T) {
		work, acct := setup(t)
		start(t, provider.StartSpec{WorkDir: work, NewRef: id, Project: "demo", Requester: "Muse", Title: "Fix it"})
		b, err := os.ReadFile(filepath.Join(acct, "local_"+id+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var r desktopRecord
		json.Unmarshal(b, &r)
		real, _ := filepath.EvalSymlinks(work)
		if r.CLISessionID != id || r.SessionID != "local_"+id || r.Cwd != real || r.Title != "demo · Muse via Aide · Fix it" || r.CreatedAt == 0 {
			t.Fatalf("record %s", b)
		}
	})
	t.Run("resumed", func(t *testing.T) {
		work, acct := setup(t)
		start(t, provider.StartSpec{WorkDir: work, ResumeRef: id})
		if _, err := os.Stat(filepath.Join(acct, "local_"+id+".json")); err == nil {
			t.Fatal("a resumed session listed again")
		}
	})
	t.Run("another id", func(t *testing.T) {
		work, acct := setup(t)
		start(t, provider.StartSpec{WorkDir: work, NewRef: "00000000-0000-4000-8000-000000000000"})
		if ents, _ := os.ReadDir(acct); len(ents) != 1 {
			t.Fatalf("listed a session Claude Code did not start: %v", ents)
		}
	})
	t.Run("no desktop app", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("APPDATA", "")
		t.Setenv("LOCALAPPDATA", "")
		start(t, provider.StartSpec{WorkDir: t.TempDir(), NewRef: id})
		if ents, _ := os.ReadDir(home); len(ents) != 0 {
			t.Fatalf("wrote in a home without the desktop app: %v", ents)
		}
	})
}

// writeConfig writes the desktop app's config.json in dir with fields.
func writeConfig(t *testing.T, dir string, fields map[string]any) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(fields)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// Signed in in the desktop app is read from its settings wherever the app
// keeps them: the owner's computer (an MSIX app's Claude-Data\claude_<id>,
// beside its claude-code) is one sample; the classic installer's
// %APPDATA%\Claude, an MSIX package's redirected folder and macOS are
// others. Signed out (no token cache, no account, or the app saying so)
// is not signed in, and neither is a config.json elsewhere.
func TestDesktopSignedIn(t *testing.T) {
	signedIn := map[string]any{"oauth:tokenCacheV2": "opaque", "lastKnownAccountUuid": "00000000-0000-4000-8000-000000000000", "windowSizeWasSignedIn": true, "first_launch_at": 1}
	for _, c := range []struct {
		name string
		dir  func(home, appData, local string) string
		code bool // the app fetched Claude Code (its claude-code folder beside config.json)
	}{
		{"MSIX, Claude-Data (the owner's)", func(_, _, local string) string { return filepath.Join(local, "Claude-Data", "claude_pub1hash") }, true},
		{"installer, %APPDATA%\\Claude, Code never opened", func(_, appData, _ string) string { return filepath.Join(appData, "Claude") }, false},
		{"MSIX, redirected into its package", func(_, _, local string) string {
			return filepath.Join(local, "Packages", "Claude_other9", "LocalCache", "Roaming", "Claude")
		}, true},
		{"macOS", func(home, _, _ string) string { return filepath.Join(home, "Library", "Application Support", "Claude") }, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			home, appData, local := t.TempDir(), t.TempDir(), t.TempDir()
			dirs := func() []string { return desktopUserData(home, appData, local) }
			if desktopSignedIn(dirs()) {
				t.Fatal("no app: signed in")
			}
			dir := c.dir(home, appData, local)
			if c.code {
				os.MkdirAll(filepath.Join(dir, "claude-code", "2.1.326", "b1"), 0o755)
			}
			writeConfig(t, dir, signedIn)
			if !desktopSignedIn(dirs()) {
				t.Fatalf("signed in at %s: not seen in %v", dir, dirs())
			}
			// The older token cache alone does as well.
			writeConfig(t, dir, map[string]any{"oauth:tokenCache": "opaque", "lastKnownAccountUuid": "u", "windowSizeWasSignedIn": true})
			if !desktopSignedIn(dirs()) {
				t.Fatal("oauth:tokenCache: not signed in")
			}
			for name, f := range map[string]map[string]any{
				"signed out":        {"oauth:tokenCacheV2": "opaque", "lastKnownAccountUuid": "u", "windowSizeWasSignedIn": false},
				"no token cache":    {"lastKnownAccountUuid": "u", "windowSizeWasSignedIn": true},
				"empty token":       {"oauth:tokenCacheV2": "", "lastKnownAccountUuid": "u", "windowSizeWasSignedIn": true},
				"no account":        {"oauth:tokenCacheV2": "opaque", "windowSizeWasSignedIn": true},
				"no window flag":    {"oauth:tokenCacheV2": "opaque", "lastKnownAccountUuid": "u"},
				"not JSON at all":   nil,
				"first launch only": {"first_launch_at": 1},
			} {
				if f == nil {
					os.WriteFile(filepath.Join(dir, "config.json"), []byte("{oops"), 0o600)
				} else {
					writeConfig(t, dir, f)
				}
				if desktopSignedIn(dirs()) {
					t.Errorf("%s: signed in", name)
				}
			}
		})
	}
	// A config.json in some other folder of %LOCALAPPDATA% is not the app's.
	home, appData, local := t.TempDir(), t.TempDir(), t.TempDir()
	writeConfig(t, filepath.Join(local, "SomethingElse"), signedIn)
	if desktopSignedIn(desktopUserData(home, appData, local)) {
		t.Fatal("another app's config.json: signed in")
	}
}
