package provider

import (
	"strings"
	"testing"
)

// A tool call read from another app's records shows by its description, or
// a natural summary of its arguments, never the arguments themselves.
func TestToolTitle(t *testing.T) {
	long := strings.Repeat("x", 200)
	cases := []struct {
		name, tool, args, want string
	}{
		{"description first", "pwsh", `{"command":"cd repo\nSelect-String -Path release.yml -Pattern windows","description":"Extract the Windows build steps from release.yml"}`, "Extract the Windows build steps from release.yml"},
		{"an edit: its file, lines added and removed", "Edit", `{"file_path":"src/app.ts","old_string":"a\nb\nc","new_string":"a\nB"}`, "src/app.ts · +2 −3"},
		{"an edit that adds only", "edit", `{"path":"go.mod","old_string":"","new_string":"require x v1\n"}`, "go.mod · +1 −0"},
		{"several edits", "MultiEdit", `{"file_path":"main.go","edits":[{},{},{}]}`, "main.go · 3 edits"},
		{"a write: its file and its lines", "Write", `{"file_path":"notes.md","content":"one\ntwo\nthree\n"}`, "notes.md · +3"},
		{"a read", "read", `{"file_path":"D:/work/readme.md","offset":10}`, "D:/work/readme.md"},
		{"a command's first line", "Bash", `{"command":"go test ./...\necho done"}`, "go test ./..."},
		{"a search", "Grep", `{"pattern":"TODO","path":"internal"}`, "TODO · internal"},
		{"a fetch", "WebFetch", `{"url":"https://example.com/docs"}`, "https://example.com/docs"},
		{"nothing to say", "mystery", `{"n":3}`, ""},
		{"not JSON", "pwsh", `{"command":"cut sho`, ""},
		{"clipped", "Bash", `{"command":"` + long + `"}`, strings.Repeat("x", 119) + "…"},
	}
	for _, c := range cases {
		if got := ToolTitle(c.tool, []byte(c.args)); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
