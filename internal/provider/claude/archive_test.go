package claude

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDesktopArchive (工单 #67): a session listed in the Claude desktop app is archived there in its record and its
// folder's archived-sessions.idx, the rest of the record kept; and read back; a session not listed is not known.
func TestDesktopArchive(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, "Library", "Application Support", "Claude", "claude-code-sessions", "acct", "org")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "local_abc.json"), []byte(`{"sessionId":"local_abc","cliSessionId":"abc","cwd":"/w","title":"t","extra":1}`), 0o644)
	p := &Provider{}
	if got := p.Archived([]string{"abc", "zzz"}); len(got) != 1 || got["abc"] {
		t.Fatalf("before: %v", got)
	}
	if err := p.Archive(context.Background(), "abc", true); err != nil {
		t.Fatal(err)
	}
	if got := p.Archived([]string{"abc"}); !got["abc"] {
		t.Fatalf("after: %v", got)
	}
	var r map[string]any
	b, _ := os.ReadFile(filepath.Join(dir, "local_abc.json"))
	json.Unmarshal(b, &r)
	if r["isArchived"] != true || r["extra"] != float64(1) {
		t.Fatalf("record: %s", b)
	}
	idx, _ := os.ReadFile(filepath.Join(dir, "archived-sessions.idx"))
	if !json.Valid(idx) || !contains(string(idx), "local_abc") {
		t.Fatalf("index: %s", idx)
	}
	p.Archive(context.Background(), "abc", false)
	if got := p.Archived([]string{"abc"}); got["abc"] {
		t.Fatalf("unarchived: %v", got)
	}
	if err := p.Archive(context.Background(), "not-listed", true); err != nil {
		t.Fatal(err)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
