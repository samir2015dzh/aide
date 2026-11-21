package dshnative

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Two DSH homes laid out unlike the owner's computer (the handover's
// forensics report is a sample, not the rule): sessions are found by
// walking the sessions tree, whatever DSH's workspace registry lists, the
// home is where DSH_HOME or the default puts it, and each session's
// directory and log are found by DSH's rules, not by names seen once.

// listed is what List shows of each session: id, title, cwd, error.
func listed(t *testing.T, home string) []string {
	t.Helper()
	list, err := List(home, nil)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, s := range list {
		out = append(out, strings.Join([]string{s.SessionId, s.Title, s.Cwd, s.Error}, " | "))
	}
	return out
}

// Layout 1: DSH_HOME set to a folder far from the user's home, its path
// with spaces and non-ASCII letters; logs not compressed; session
// directories named by bare ids; a project directory DSH names with ~HEX
// escapes; a workspace registry that lists none of the sessions; a
// session with no title (shown by its first prompt); a session directory
// with no log yet, and an empty project.
func TestLayoutCustomHomeRawLogs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Data Drive", "工具", "dsh home")
	home, err := Home([]string{"DSH_HOME=" + root})
	if err != nil || home != root {
		t.Fatalf("home: %q %v, want %q", home, err, root)
	}
	cwd := filepath.Join(t.TempDir(), "项目 一")
	titled := writeSession(t, home, map[string]any{"id": "7d1c0b6e-0a4f-4c43-9f0e-2b8f6f1d2a11", "cwd": cwd}, false,
		conversation(0, 1, "列出文件", "有 README"), events(9, ev("session/title", map[string]any{"title": "看看仓库", "source": "auto", "messageSeqs": []any{1}})))
	untitled := writeSession(t, home, map[string]any{"id": "c0ffee00-1111-2222-3333-444455556666", "cwd": cwd}, false, conversation(100, 1, "第二个问题\n后面的行", "好"))
	// DSH's own name for the project directory, ~HEX escaped: move both there.
	escaped := filepath.Join(home, "sessions", "--X-~9879~76EE-~4E00--")
	if err := os.MkdirAll(escaped, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{titled, untitled} {
		if err := os.Rename(d, filepath.Join(escaped, filepath.Base(d))); err != nil {
			t.Fatal(err)
		}
	}
	os.Remove(filepath.Dir(titled))
	os.MkdirAll(filepath.Join(escaped, "a0a0a0a0-no-log-yet"), 0o700)
	os.MkdirAll(filepath.Join(home, "sessions", "--E-empty--"), 0o700)
	os.MkdirAll(filepath.Join(home, "storages"), 0o700)
	os.WriteFile(filepath.Join(home, "storages", "workspace.json"), []byte(`{"unit":{"name":"workspace","version":2},"global":{"archivedSessionIds":[]},"tables":{"workspaces":{"w":{"path":"X:\\x","sessionIds":[]}}}}`), 0o600)

	got := listed(t, home)
	want := []string{
		"c0ffee00-1111-2222-3333-444455556666 | 第二个问题 | " + cwd + " | ",
		"7d1c0b6e-0a4f-4c43-9f0e-2b8f6f1d2a11 | 看看仓库 | " + cwd + " | ",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("listed:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	s, evs, err := Read(home, "7d1c0b6e-0a4f-4c43-9f0e-2b8f6f1d2a11")
	if err != nil || s.Title != "看看仓库" || len(evs) == 0 {
		t.Fatalf("read: %v %d %v", s, len(evs), err)
	}
}

// Layout 2: no DSH_HOME, so the default ~/.dsh under the user's home;
// compressed logs, with an older format generation kept beside the current
// one (the highest is read); session directories named "session-<id>"; a
// subagent's session (not listed); no storages folder at all.
func TestLayoutDefaultHomeGenerations(t *testing.T) {
	user := filepath.Join(t.TempDir(), "users", "someone else")
	t.Setenv("HOME", user)
	t.Setenv("USERPROFILE", user)
	home, err := Home([]string{"PATH=x"})
	if want := filepath.Join(user, ".dsh"); err != nil || home != want {
		t.Fatalf("home: %q %v, want %q", home, err, want)
	}
	cwd := filepath.Join(user, "code", "site")
	dir := writeSession(t, home, map[string]any{"id": "session-5b2e", "cwd": cwd}, true,
		conversation(0, 1, "Deploy the site", "Deployed."), events(9, ev("session/title", map[string]any{"title": "Deploy", "source": "auto", "messageSeqs": []any{1}})))
	// The generation before, which DSH keeps beside the current one.
	os.WriteFile(filepath.Join(dir, "session.v3.jsonl.zstd"), encoder.EncodeAll([]byte(`{"type":"session","version":3,"id":"session-5b2e","createdAt":1}`+"\n"), nil), 0o600)
	writeSession(t, home, map[string]any{"id": "aa_1f_sess_Qm9i", "cwd": cwd, "origin": "subagent", "parentSession": "session-5b2e"}, true, conversation(0, 1, "x", "y"))

	got := listed(t, home)
	if want := []string{"session-5b2e | Deploy | " + cwd + " | "}; !slices.Equal(got, want) {
		t.Fatalf("listed:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if _, evs, err := Read(home, "session-5b2e"); err != nil || len(evs) == 0 {
		t.Fatalf("read: %d %v", len(evs), err)
	}
}
