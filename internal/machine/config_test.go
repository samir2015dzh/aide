package machine

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
)

func addLocal(t *testing.T, n *Machine, cmd *identityv1.AdminCommand) {
	t.Helper()
	if _, err := n.ApplyLocal(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
}

// A machine's projects and roots, exported and applied to a clean
// machine, export the same; a dry run changes nothing; applying again
// changes nothing.
func TestConfigRoundTrip(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	root := filepath.Join(base, "root")
	for _, d := range []string{filepath.Join(root, "app"), filepath.Join(root, "notes")} {
		os.MkdirAll(d, 0o755)
	}
	a, b := rootsMachine(t), rootsMachine(t)
	addLocal(t, a, &identityv1.AdminCommand{Command: &identityv1.AdminCommand_SetWorkspaceRoots{SetWorkspaceRoots: &identityv1.SetWorkspaceRoots{Roots: []string{root}}}})
	addLocal(t, a, &identityv1.AdminCommand{Command: &identityv1.AdminCommand_AddProject{AddProject: &identityv1.AddProject{ProjectId: "app", Path: filepath.Join(root, "app"),
		Workspace: identityv1.Workspace_WORKSPACE_IN_PLACE, Description: "the app", Agents: []string{"claude"}}}})
	addLocal(t, a, &identityv1.AdminCommand{Command: &identityv1.AdminCommand_AddProject{AddProject: &identityv1.AddProject{ProjectId: "notes", Path: filepath.Join(root, "notes"),
		Workspace: identityv1.Workspace_WORKSPACE_IN_PLACE}}})

	export := func(n *Machine) string {
		c, err := n.ExportConfig()
		if err != nil {
			t.Fatal(err)
		}
		y, err := yaml.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		return string(y)
	}
	want := export(a)
	var c ConfigFile
	if err := yaml.Unmarshal([]byte(want), &c); err != nil {
		t.Fatal(err)
	}
	clean := export(b)

	res := b.ApplyConfig(ctx, c, true)
	if res.Error != nil || len(res.Changes) != 3 || res.Changes[0].Section != "workspace_roots" || res.Changes[1].Name != "app" || res.Changes[1].Applied {
		t.Fatalf("dry run: %+v", res)
	}
	if got := export(b); got != clean {
		t.Fatalf("the dry run changed something:\n%s", got)
	}
	if res := b.ApplyConfig(ctx, c, false); res.Error != nil || len(res.Changes) != 3 || !res.Changes[2].Applied {
		t.Fatalf("apply: %+v", res)
	}
	if got := export(b); got != want {
		t.Fatalf("exported after apply:\n%s\nwant:\n%s", got, want)
	}
	if res := b.ApplyConfig(ctx, c, false); res.Error != nil || len(res.Changes) != 0 {
		t.Fatalf("applied again: %+v", res)
	}
}

// A project id at another path is a conflict: nothing is changed. A file
// naming a folder that isn't here, or a project outside the roots it sets,
// changes nothing either.
func TestConfigRefusals(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	root, other := filepath.Join(base, "root"), filepath.Join(base, "other")
	os.MkdirAll(filepath.Join(root, "app"), 0o755)
	os.MkdirAll(other, 0o755)
	n := rootsMachine(t)
	addLocal(t, n, &identityv1.AdminCommand{Command: &identityv1.AdminCommand_AddProject{AddProject: &identityv1.AddProject{ProjectId: "app", Path: other, Workspace: identityv1.Workspace_WORKSPACE_IN_PLACE}}})
	roots := []string{root}
	for _, c := range []struct {
		name string
		file ConfigFile
		kind string
	}{
		{"conflict", ConfigFile{Version: 1, WorkspaceRoots: &[]string{}, Projects: []ConfigProject{{ID: "app", Path: filepath.Join(root, "app"), Workspace: "in_place"}}}, "invalid_state"},
		{"missing folder", ConfigFile{Version: 1, Projects: []ConfigProject{{ID: "gone", Path: filepath.Join(base, "gone"), Workspace: "in_place"}}}, "not_found"},
		{"outside", ConfigFile{Version: 1, WorkspaceRoots: &roots, Projects: []ConfigProject{{ID: "out", Path: other, Workspace: "in_place"}}}, "outside_roots"},
		{"version", ConfigFile{Version: 9}, "usage"},
		{"workspace", ConfigFile{Version: 1, Projects: []ConfigProject{{ID: "x", Path: other, Workspace: "cloud"}}}, "usage"},
	} {
		res := n.ApplyConfig(ctx, c.file, false)
		if res.Error == nil || res.Error.Kind != c.kind || res.Error.Hint == "" {
			t.Errorf("%s: %+v %+v", c.name, res, res.Error)
		}
		if len(n.WorkspaceRoots()) != 0 {
			t.Fatalf("%s: the roots changed", c.name)
		}
		if p, _ := n.st.Project("app"); p.Path != other {
			t.Fatalf("%s: the project changed: %+v", c.name, p)
		}
	}
}

// Agent defaults and custom agents are exported, a secret's value never;
// apply names those that differ and changes none (the owner's devices set
// them), applying the rest, refused with kind denied.
func TestConfigOwnerOnly(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	a, b := rootsMachine(t), rootsMachine(t)
	a.defaults["codex"] = &identityv1.AgentDefaults{Agent: "codex", Model: "gpt-5", Speed: identityv1.AgentSpeed_AGENT_SPEED_FAST}
	a.customs.list = []customAgent{{Name: "mine", Command: "my-agent", Args: []string{"--acp"}, Env: []customEnv{{Name: "MODE", Value: "fast"}, {Name: "MY_API_KEY"}, {Name: "OTHER", Secret: true}}}}
	a.customs.secrets = map[string]map[string]string{"mine": {"MY_API_KEY": "sk-very-secret", "OTHER": "hidden-value"}}

	c, err := a.ExportConfig()
	if err != nil {
		t.Fatal(err)
	}
	y, _ := yaml.Marshal(c)
	if strings.Contains(string(y), "sk-very-secret") || strings.Contains(string(y), "hidden-value") || !strings.Contains(string(y), "MY_API_KEY") ||
		!strings.Contains(string(y), "speed: fast") || !strings.Contains(string(y), "value: fast") {
		t.Fatalf("exported:\n%s", y)
	}
	want := []ConfigEnv{{Name: "MODE", Value: "fast"}, {Name: "MY_API_KEY", Secret: true}, {Name: "OTHER", Secret: true}}
	if !reflect.DeepEqual(c.CustomAgents[0].Env, want) {
		t.Fatalf("env: %+v", c.CustomAgents[0].Env)
	}
	// The same machine: nothing differs, nothing refused.
	if res := a.ApplyConfig(ctx, c, false); res.Error != nil || len(res.Changes) != 0 {
		t.Fatalf("applied to itself: %+v", res)
	}

	c.Projects = []ConfigProject{{ID: "here", Path: dir, Workspace: "in_place"}}
	res := b.ApplyConfig(ctx, c, false)
	if res.Error == nil || res.Error.Kind != "denied" || !strings.Contains(res.Error.Message, "agent_defaults codex") || !strings.Contains(res.Error.Message, "custom_agents mine") {
		t.Fatalf("owner-only: %+v %+v", res, res.Error)
	}
	if p, _ := b.st.Project("here"); p == nil {
		t.Fatal("the project was not applied")
	}
	if b.defaults["codex"] != nil || len(b.customs.list) != 0 {
		t.Fatal("owner-only settings changed")
	}
}
