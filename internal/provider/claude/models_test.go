package claude

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/samir2015dzh/aide/internal/provider"
)

// TestListModels (工单 #99): Claude Code lists its models when asked as its
// SDK asks (Claude Code 2.1.330's answer, signed out): each alias with the
// specific model it runs here and its reasoning efforts; "default", what it
// runs unless told, is no model to pick but the specific one it runs.
func TestListModels(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "claude-2.1.330-initialize.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	models, own := parseModels(append([]byte(`{"type":"system","subtype":"ui_invalidate"}`+"\n"), b...))
	all := []string{"low", "medium", "high", "xhigh", "max"}
	want := []provider.Model{
		{ID: "sonnet", Name: "Sonnet", Resolved: "claude-sonnet-5-5", Efforts: all},
		{ID: "fable", Name: "Fable", Resolved: "claude-fable-5-2", Efforts: all},
		{ID: "opus", Name: "Opus", Resolved: "claude-opus-5-5", Efforts: all},
		{ID: "haiku", Name: "Haiku", Resolved: "claude-haiku-5-5", Efforts: all},
	}
	if !reflect.DeepEqual(models, want) || own != "claude-sonnet-5-5" {
		t.Fatalf("got %+v, own %q", models, own)
	}
	if m, o := parseModels([]byte("2.1.4 (Claude Code)\n")); m != nil || o != "" {
		t.Fatalf("an older one: %+v %q", m, o)
	}
}

// TestDiscoverListsModels: the check asks Claude Code itself, once, with
// nothing saved and no MCP server or hook of the owner's run; one that
// does not answer has the aliases it takes, their specific models unknown.
func TestDiscoverListsModels(t *testing.T) {
	scripted(t)
	dir := t.TempDir()
	args := filepath.Join(dir, "args")
	bin := filepath.Join(dir, "claude")
	answer, _ := filepath.Abs(filepath.Join("testdata", "claude-2.1.330-initialize.jsonl"))
	script := `#!/bin/sh
case "$1" in --version) echo "2.1.330 (Claude Code)"; exit;; auth) echo '{"loggedIn":true}'; exit;; esac
echo "$@" > ` + args + `
pwd >> ` + args + `
read req
case "$req" in *'"subtype":"initialize"'*) cat ` + answer + `;; esac
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	info := (&Provider{Binary: bin}).Discover(context.Background())
	if len(info.Models) != 4 || info.Models[2].ID != "opus" || info.Models[2].Resolved != "claude-opus-5-5" || info.Own.Model != "claude-sonnet-5-5" {
		t.Fatalf("got %+v own %+v", info.Models, info.Own)
	}
	got, _ := os.ReadFile(args)
	for _, want := range []string{"--no-session-persistence", "--strict-mcp-config", `{"disableAllHooks":true}`, "aide-claude-models-"} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("ran with %s, want %s", got, want)
		}
	}

	os.WriteFile(bin, []byte("#!/bin/sh\necho '2.1.4 (Claude Code)'\n"), 0o755)
	info = (&Provider{Binary: bin}).Discover(context.Background())
	if !reflect.DeepEqual(info.Models, Models) || info.Own.Model != "" || info.Models[0].Resolved != "" {
		t.Fatalf("an older one: %+v %+v", info.Models, info.Own)
	}
}
