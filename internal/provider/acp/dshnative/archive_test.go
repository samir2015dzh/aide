package dshnative

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestSetArchived (工单 #67): archiving a session in DSH Desktop's sidebar adds it to
// global.archivedSessionIds and keeps the rest of the file; unarchiving takes it out; no file, nothing.
func TestSetArchived(t *testing.T) {
	home := t.TempDir()
	if err := SetArchived(home, "s1", true); err != nil {
		t.Fatal(err)
	}
	if _, ok := ArchivedState(home); ok {
		t.Fatal("a state made up where DSH Desktop never ran")
	}
	path := filepath.Join(home, "storages", "workspace.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(`{"global":{"archivedSessionIds":["old"],"theme":"dark"},"workspaces":[{"id":"w"}]}`), 0o644)
	if err := SetArchived(home, "s1", true); err != nil {
		t.Fatal(err)
	}
	ids, ok := ArchivedState(home)
	if !ok || !ids["s1"] || !ids["old"] {
		t.Fatalf("archived: %v", ids)
	}
	var w map[string]any
	b, _ := os.ReadFile(path)
	json.Unmarshal(b, &w)
	if w["global"].(map[string]any)["theme"] != "dark" || len(w["workspaces"].([]any)) != 1 {
		t.Fatalf("the rest of the file: %s", b)
	}
	SetArchived(home, "s1", false)
	if ids, _ := ArchivedState(home); ids["s1"] || !ids["old"] {
		t.Fatalf("unarchived: %v", ids)
	}
}
