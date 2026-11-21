package machine

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"connectrpc.com/connect"

	inboxv1 "github.com/samir2015dzh/aide/gen/go/aide/inbox/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
)

func writeMark(t *testing.T, dir, body string) {
	t.Helper()
	os.MkdirAll(dir, 0o755)
	if err := os.WriteFile(filepath.Join(dir, WorkspaceMarker), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFindWorkspace(t *testing.T) {
	base := t.TempDir()
	if w := findWorkspace(base); w != nil {
		t.Fatalf("no marker: %+v", w)
	}
	root := filepath.Join(base, "工作 区")
	repo := filepath.Join(root, "repos", "演示 仓库", "src")
	os.MkdirAll(repo, 0o755)
	writeMark(t, root, `{"workspace_id":"ws-1","rules_file":"rules/R.md","requests_file":"../outside.md","tools_manifest":"/abs/MANIFEST.md",
		"channel":{"coordinator":"c_coord","coordinator_name":"Coord","provisioner":"not-a-client"}}`)
	w := findWorkspace(repo)
	if w == nil || w.Root != root || w.Mark.ID != "ws-1" || w.coordinator() != "c_coord" || w.Mark.Channel.Provisioner != "" {
		t.Fatalf("found: %+v", w)
	}
	if w.Mark.RulesFile != filepath.Join("rules", "R.md") || w.Mark.RequestsFile != filepath.Join("_inbox", "requests.md") ||
		w.Mark.ToolsManifest != filepath.Join("_tools", "MANIFEST.md") {
		t.Fatalf("paths stay inside the root: %+v", w.Mark)
	}
	// The nearest marker wins; one that doesn't parse marks a workspace
	// without a channel; a coordinator that is no client means no channel.
	writeMark(t, filepath.Join(root, "repos"), `{not json`)
	if w := findWorkspace(repo); w == nil || w.Root != filepath.Join(root, "repos") || w.coordinator() != "" || w.Mark.RulesFile != "CLAUDE-WORKSPACE-RULES.md" {
		t.Fatalf("a broken marker: %+v", w)
	}
	writeMark(t, filepath.Join(root, "repos"), `{"channel":{"coordinator":"Muse"}}`)
	if w := findWorkspace(repo); w == nil || w.coordinator() != "" {
		t.Fatalf("a coordinator by name only: %+v", w)
	}
}

func TestWorkspacePrompt(t *testing.T) {
	base := t.TempDir()
	if p := workspacePrompt(base); p != "" {
		t.Fatalf("outside a managed workspace: %q", p)
	}
	root := filepath.Join(base, "ws")
	writeMark(t, root, `{"workspace_id":"ws-9","channel":{"coordinator":"c_coord","coordinator_name":"Coord"}}`)
	p := workspacePrompt(filepath.Join(root, "repos", "app"))
	for _, want := range []string{"ws-9", root, "CLAUDE-WORKSPACE-RULES.md", "Coord（c_coord）", "workspace_request", "workspace_confirm",
		"pending", "approved", "rejected", "provisioned", "confirmed", "探测类", "防冒充", "批准 ≠ 自己动手"} {
		if !strings.Contains(p, want) {
			t.Fatalf("the prompt lacks %q:\n%s", want, p)
		}
	}
	if strings.ContainsAny(p, "{}") {
		t.Fatalf("a placeholder left:\n%s", p)
	}
	writeMark(t, root, `{"workspace_id":"ws-9"}`)
	p = workspacePrompt(filepath.Join(root, "repos", "app"))
	if !strings.Contains(p, filepath.Join("_inbox", "requests.md")) || strings.Contains(p, "workspace_request") || strings.ContainsAny(p, "{}") {
		t.Fatalf("the file channel's prompt:\n%s", p)
	}
}

// TestWorkspaceTemplateNamesNoMachine: the fixed text names no drive,
// home directory, host or address of any machine.
func TestWorkspaceTemplateNamesNoMachine(t *testing.T) {
	bad := regexp.MustCompile(`(?i)[a-z]:\\|/home/|/users/|\.local\b|\.lan\b|@|https?://|\d+\.\d+\.\d+\.\d+`)
	for _, s := range []string{workspacePromptTemplate, workspaceChannelOnline, workspaceChannelFile} {
		if m := bad.FindString(s); m != "" {
			t.Fatalf("the template names %q", m)
		}
	}
}

func TestCheckWorkspaceRequest(t *testing.T) {
	for _, c := range []struct {
		r    *sessionv1.WorkspaceRequest
		want string
	}{
		{&sessionv1.WorkspaceRequest{Kind: "download", Item: "x", Purpose: "y"}, "kind is one of"},
		{&sessionv1.WorkspaceRequest{Kind: "tool", Item: "rg", Purpose: "search"}, "needs version, repo, source"},
		{&sessionv1.WorkspaceRequest{Kind: "permission", Item: "write", Purpose: "p"}, "needs scope"},
		{&sessionv1.WorkspaceRequest{Kind: "system_change", Item: "service", Purpose: "p"}, "needs rollback"},
		{&sessionv1.WorkspaceRequest{Kind: "info", Item: "timezone", Purpose: strings.Repeat("x", 2001)}, "too long"},
		{&sessionv1.WorkspaceRequest{Kind: "info", Item: "timezone", Purpose: "logs"}, ""},
	} {
		err := checkWorkspaceRequest(c.r)
		if (c.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), c.want)) {
			t.Fatalf("%+v: %v, want %q", c.r, err, c.want)
		}
	}
}

func TestWorkspaceReply(t *testing.T) {
	n := &Machine{}
	reqs := map[string]*sessionv1.WorkspaceRequest{
		"wr_a": {RequestId: "wr_a", Kind: "tool", Item: "rg", Status: "pending", From: "agent", Coordinator: "c_coord", Provisioner: "c_prov"},
	}
	n.wsReqs.sessions = map[string]map[string]*sessionv1.WorkspaceRequest{"s_1": reqs}
	reply := func(from, status, text string) (*sessionv1.WorkspaceRequest, string, error) {
		return n.workspaceReply("s_1", from, "Name", &inboxv1.Message{MessageId: "msg_1", Text: text,
			WorkspaceRequest: &sessionv1.WorkspaceRequest{RequestId: "wr_a", Status: status}})
	}
	code := func(err error) connect.Code { return connect.CodeOf(err) }
	for _, c := range []struct {
		from, status, text string
		want               connect.Code
	}{
		{"c_other", "approved", "ok", connect.CodePermissionDenied},
		{"c_prov", "approved", "ok", connect.CodePermissionDenied},        // a provisioner only provisions
		{"c_coord", "confirmed", "ok", connect.CodePermissionDenied},      // only the coding agent confirms
		{"c_coord", "provisioned", "ok", connect.CodeFailedPrecondition},  // not approved yet
		{"c_coord", "rejected", "  ", connect.CodeInvalidArgument},        // a rejection says why
		{"c_coord", "approved", "low risk", connect.CodeUnknown /* ok */}, // see below
	} {
		_, _, err := reply(c.from, c.status, c.text)
		if c.want == connect.CodeUnknown {
			if err != nil {
				t.Fatalf("%+v: %v", c, err)
			}
			continue
		}
		if code(err) != c.want {
			t.Fatalf("%+v: %v", c, err)
		}
	}
	if _, _, err := n.workspaceReply("s_1", "c_coord", "", &inboxv1.Message{WorkspaceRequest: &sessionv1.WorkspaceRequest{RequestId: "wr_x", Status: "approved"}}); code(err) != connect.CodeNotFound {
		t.Fatalf("an unknown request: %v", err)
	}
	reqs["wr_a"].Status = "approved"
	w, text, err := reply("c_prov", "provisioned", "at _tools/rg, 14.1.0")
	if err != nil || w.Status != "provisioned" || w.From != "c_prov" || w.FromName != "Name" || w.Note != "at _tools/rg, 14.1.0" || w.MessageId != "msg_1" ||
		w.Item != "rg" || w.Coordinator != "c_coord" {
		t.Fatalf("provisioned by its provisioner: %+v %v", w, err)
	}
	for _, want := range []string{"[Aide]", "wr_a", "「已就位」(provisioned)", "Aide 已核验批复者身份", "workspace_confirm", "at _tools/rg, 14.1.0"} {
		if !strings.Contains(text, want) {
			t.Fatalf("what the agent is told lacks %q: %s", want, text)
		}
	}
	if reqs["wr_a"].Status != "approved" {
		t.Fatal("a checked reply changes nothing until it is delivered")
	}
}
