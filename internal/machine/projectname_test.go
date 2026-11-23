package machine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/provider"
)

// TestFolderOf: a path's last folder, Windows' separator as POSIX's.
func TestFolderOf(t *testing.T) {
	for path, want := range map[string]string{
		`D:\商务`: "商务", `D:\杂七杂八\`: "杂七杂八", `C:\Users\me\My App`: "My App", "/home/me/app": "app", "/home/me/app/": "app",
		`D:\`: "", "D:": "", "/": "", "": "",
	} {
		if got := folderOf(path); got != want {
			t.Errorf("folderOf(%q) = %q, want %q", path, got, want)
		}
	}
}

// TestProjectFolderName (工单 #88): a new project is named by its folder,
// "-2" on the folder's name when another project is shown by it; one shown
// by its id already gets no name.
func TestProjectFolderName(t *testing.T) {
	others := []Project{{ID: "project", Name: "商务", Path: `D:\商务`}, {ID: "app"}}
	for _, c := range []struct{ id, path, want string }{
		{"project-2", `D:\杂七杂八`, "杂七杂八"},
		{"project-3", `E:\备份\商务`, "商务-2"},
		{"myapp", "/home/me/MyApp", "MyApp"},
		{"app-2", "/srv/app", "app-2"},
		{"web", "/home/me/web", ""},
		{"billing", "/tmp/001", ""},
		{"project", "/home/me/app", ""},
		{"project-4", `D:\`, ""},
	} {
		if got := folderName(c.id, c.path, others); got != c.want {
			t.Errorf("folderName(%q, %q) = %q, want %q", c.id, c.path, got, c.want)
		}
	}
}

// TestProjectNamesSetRight (工单 #88): through the admin command, a new
// project in a folder named in Chinese is named by the folder, its twin
// elsewhere "商务-2"; added again, it keeps the name. As the store opens,
// projects still shown by the placeholder id made of such a folder are
// named by their folders, and those the owner named are not touched.
func TestProjectNamesSetRight(t *testing.T) {
	h := newHarness(t, DefaultLimits())
	ctx := context.Background()
	root := t.TempDir()
	dir := func(parts ...string) string {
		d := filepath.Join(append([]string{root}, parts...)...)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		return d
	}
	add := func(id, path string) {
		t.Helper()
		if _, err := h.n.execAdmin(ctx, &identityv1.AdminCommand{Command: &identityv1.AdminCommand_AddProject{AddProject: &identityv1.AddProject{
			ProjectId: id, Path: path, Workspace: identityv1.Workspace_WORKSPACE_IN_PLACE}}}); err != nil {
			t.Fatal(err)
		}
	}
	name := func(id string) string {
		t.Helper()
		p, err := h.n.st.Project(id)
		if err != nil || p == nil {
			t.Fatal(id, p, err)
		}
		return p.Name
	}
	add("project", dir("商务"))
	add("project-2", dir("备份", "商务"))
	if a, b := name("project"), name("project-2"); a != "商务" || b != "商务-2" {
		t.Fatalf("names: %q, %q", a, b)
	}
	add("project", dir("商务"))
	if n := name("project"); n != "商务" {
		t.Fatalf("added again: %q", n)
	}

	// The projects as an older machine kept them: placeholders, and one the owner named.
	for _, p := range []Project{{ID: "project-5", Path: `D:\杂七杂八`}, {ID: "project-6", Path: dir("合同")}, {ID: "project-7", Path: dir("报价")}, {ID: "project-8", Path: dir("app")}} {
		if err := h.n.st.PutProject(p); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.n.st.SetProjectName("project-7", "我的报价"); err != nil {
		t.Fatal(err)
	}
	if err := h.n.st.nameByFolder(); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"project-5": "杂七杂八", "project-6": "合同", "project-7": "我的报价", "project-8": ""} {
		if got := name(id); got != want {
			t.Errorf("%s: %q, want %q", id, got, want)
		}
	}
}

// TestProjectDSHPreset (工单 #89): a project's DSH permission preset is
// what its DSH sessions start with when none is chosen for the session; one
// chosen goes; another agent's session is not touched; a preset DSH does
// not offer is refused; empty is DSH's own again.
func TestProjectDSHPreset(t *testing.T) {
	dsh := &presets{offering{offers: provider.Offers{Modes: []provider.Mode{{ID: "read-only", Name: "read-only"}, {ID: "workspace-write", Name: "workspace-write"}}}}}
	h := newHarnessWith(t, DefaultLimits(), map[string]provider.Provider{"deepseek": dsh})
	ctx := context.Background()
	<-h.n.CheckAgents(false, AgentsOnce)
	set := func(preset string) error {
		_, err := h.n.execAdmin(ctx, &identityv1.AdminCommand{Command: &identityv1.AdminCommand_SetProjectPermission{SetProjectPermission: &identityv1.SetProjectPermission{ProjectId: "demo", DshPreset: preset}}})
		return err
	}
	if err := set("danger-full-access"); err == nil {
		t.Fatal("a preset DSH doesn't offer was taken")
	}
	if err := set("read-only"); err != nil {
		t.Fatal(err)
	}
	if p, _ := h.n.st.Project("demo"); p.DSHPreset != "read-only" {
		t.Fatalf("kept: %+v", p)
	}
	start := func(agent, mode string) provider.StartSpec {
		t.Helper()
		s, err := h.s.Start(ctx, h.p, &sessionv1.StartSessionRequest{ProjectId: "demo", Agent: agent, Prompt: "say one", Mode: mode})
		if err != nil {
			t.Fatal(err)
		}
		h.wait(s.SessionId, idle)
		if agent == "deepseek" {
			return dsh.spec(-1)
		}
		return h.rec.spec(-1)
	}
	if m := start("deepseek", "").Mode; m != "read-only" {
		t.Fatalf("the project's preset: %q", m)
	}
	if m := start("deepseek", "workspace-write").Mode; m != "workspace-write" {
		t.Fatalf("one chosen for the session: %q", m)
	}
	if m := start("fake", "").Mode; m != "" {
		t.Fatalf("another agent: %q", m)
	}
	if err := set(""); err != nil {
		t.Fatal(err)
	}
	if m := start("deepseek", "").Mode; m != "" {
		t.Fatalf("DSH's own again: %q", m)
	}
}
