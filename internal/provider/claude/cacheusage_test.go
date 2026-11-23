package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeCodeConfig(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// What Claude Code here last read of the plan usage and keeps in its config
// (工单 #104): the windows as the usage endpoint named them, when Claude
// Code read them; nothing when there is no such cache, it is another
// account's, or the file is no JSON; $CLAUDE_CONFIG_DIR first, an older
// version's config last.
func TestCacheUsage(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "claudecache", "claude.json"))
	if err != nil {
		t.Fatal(err)
	}

	home := t.TempDir()
	writeCodeConfig(t, filepath.Join(home, ".claude.json"), string(fixture))
	u := (&CacheUsage{Home: home, ConfigDir: filepath.Join(home, "none")}).Read()
	if u.Unavailable != "" || !u.FetchedAt.Equal(time.UnixMilli(1763640000000)) {
		t.Fatalf("cache: %+v", u)
	}
	if len(u.Windows) != 2 || u.Windows[0].Key != "five_hour" || u.Windows[0].Utilization != 31 || u.Windows[1].Key != "seven_day" || u.Windows[1].Utilization != 64 {
		t.Fatalf("cache windows: %+v", u.Windows)
	}
	if u.Windows[0].ResetsAt.IsZero() {
		t.Fatalf("five_hour resets at %v", u.Windows[0].ResetsAt)
	}

	// $CLAUDE_CONFIG_DIR is where Claude Code looks first.
	dir := t.TempDir()
	writeCodeConfig(t, filepath.Join(dir, ".claude.json"), `{"numStartups": 1}`)
	if u := (&CacheUsage{Home: home, ConfigDir: dir}).Read(); u.Unavailable != CacheNone || !strings.Contains(u.Detail, "keeps no plan usage") {
		t.Fatalf("config dir: %+v", u)
	}

	// An older version's config.
	old := t.TempDir()
	writeCodeConfig(t, filepath.Join(old, ".claude", ".config.json"), string(fixture))
	if u := (&CacheUsage{Home: old, ConfigDir: filepath.Join(old, "none")}).Read(); u.Unavailable != "" || len(u.Windows) != 2 {
		t.Fatalf("older config: %+v", u)
	}

	other := t.TempDir()
	writeCodeConfig(t, filepath.Join(other, ".claude.json"), strings.Replace(string(fixture), `"accountUuid": "00000000-0000-4000-8000-0000000000aa", "organizationUuid"`, `"accountUuid": "00000000-0000-4000-8000-0000000000cc", "organizationUuid"`, 1))
	if u := (&CacheUsage{Home: other, ConfigDir: filepath.Join(other, "none")}).Read(); u.Unavailable != CacheOtherAccount || len(u.Windows) != 0 {
		t.Fatalf("another account's: %+v", u)
	}

	bad := t.TempDir()
	writeCodeConfig(t, filepath.Join(bad, ".claude.json"), `{"cachedUsageUtilization": `)
	if u := (&CacheUsage{Home: bad, ConfigDir: filepath.Join(bad, "none")}).Read(); u.Unavailable != CacheUnreadable {
		t.Fatalf("not JSON: %+v", u)
	}

	// A config that can't be read: said so, by the file's name, not its path.
	if os.Getuid() != 0 {
		locked := t.TempDir()
		writeCodeConfig(t, filepath.Join(locked, ".claude.json"), string(fixture))
		os.Chmod(filepath.Join(locked, ".claude.json"), 0)
		if u := (&CacheUsage{Home: locked, ConfigDir: filepath.Join(locked, "none")}).Read(); u.Unavailable != CacheUnreadable || strings.Contains(u.Detail, locked) {
			t.Fatalf("unreadable: %+v", u)
		}
	}

	empty := t.TempDir()
	if u := (&CacheUsage{Home: empty, ConfigDir: filepath.Join(empty, "none")}).Read(); u.Unavailable != CacheNone || u.Detail == "" {
		t.Fatalf("no config: %+v", u)
	}
}
