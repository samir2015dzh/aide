package e2e

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
)

// TestProjectName (工单 #50): the owner gives a project with a default
// folder name ("project") a name of their own, from the app; every device
// lists it by that name at once, its id, folder and sessions as they were;
// a name too long is refused, and an empty one shows it by its id again.
// A new project is named by its folder (工单 #88): one in a folder named in
// Chinese, under the placeholder id the app makes of it, is listed by the
// folder's name; one whose id was chosen is shown by its id.
// The coding agents a machine reports say when each was checked (工单 #49).
func TestProjectName(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	w := newOwnerMac(t, []string{"AIDE_FAKE_PROVIDERS=1"}, &localv1.GrantSpec{})
	ctx := context.Background()
	ms, err := w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	mid := ms.Machines[0].MachineId
	repo := gitRepo(t, map[string]string{"README.md": "# a\n"})
	if _, err := w.app.client.AddProject(ctx, &localv1.AddProjectRequest{MachineId: mid, ProjectId: "project", Path: repo}); err != nil {
		t.Fatal(err)
	}
	nameOf := func(id string) (name, path string) {
		ps, err := w.app.client.ListProjects(ctx, &localv1.ListProjectsRequest{})
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range ps.Projects {
			if p.ProjectId == id {
				return p.Name, p.Path
			}
		}
		return "?", ""
	}
	named := func() (name, path string) { return nameOf("project") }
	// Its id was not made of its folder: the owner chose it, and it is shown by it.
	if n, _ := named(); n != "" {
		t.Fatalf("a new project with an id of its own has a name: %q", n)
	}
	biz := filepath.Join(t.TempDir(), "报表")
	if err := os.Mkdir(biz, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := w.app.client.AddProject(ctx, &localv1.AddProjectRequest{MachineId: mid, ProjectId: "project-2", Path: biz, Workspace: identityv1.Workspace_WORKSPACE_IN_PLACE}); err != nil {
		t.Fatal(err)
	}
	if n, _ := nameOf("project-2"); n != "报表" {
		t.Fatalf("a project in 报表: %q", n)
	}
	if _, err := w.app.client.RenameProject(ctx, &localv1.RenameProjectRequest{MachineId: mid, ProjectId: "project", Name: "  支付后端  "}); err != nil {
		t.Fatal(err)
	}
	eventually(t, 10*time.Second, "listed by its new name, its folder the same", func() bool { n, p := named(); return n == "支付后端" && p != "" })

	// Adding it again (changing what it is for) keeps the name.
	if _, err := w.app.client.AddProject(ctx, &localv1.AddProjectRequest{MachineId: mid, ProjectId: "project", Path: repo, Description: "payments"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := named(); n != "支付后端" {
		t.Fatalf("name lost when the project was added again: %q", n)
	}
	if _, err := w.app.client.RenameProject(ctx, &localv1.RenameProjectRequest{MachineId: mid, ProjectId: "project", Name: strings.Repeat("长", 65)}); err == nil {
		t.Fatal("a name of 65 characters was taken")
	}
	if _, err := w.app.client.RenameProject(ctx, &localv1.RenameProjectRequest{MachineId: mid, ProjectId: "nope", Name: "x"}); err == nil {
		t.Fatal("a project that isn't there was named")
	}
	if _, err := w.app.client.RenameProject(ctx, &localv1.RenameProjectRequest{MachineId: mid, ProjectId: "project"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, 10*time.Second, "shown by its id again", func() bool { n, _ := named(); return n == "" })

	// When each agent was checked is said with what was found.
	if _, err := w.app.client.CheckAgents(ctx, &localv1.CheckAgentsRequest{}); err != nil {
		t.Fatal(err)
	}
	eventually(t, 30*time.Second, "agents with when they were checked", func() bool {
		ms, err := w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
		if err != nil || len(ms.Machines) == 0 {
			return false
		}
		for _, a := range ms.Machines[0].Status.GetAgents() {
			if a.Available && a.CheckedAt == nil {
				return false
			}
		}
		return len(ms.Machines[0].Status.GetAgents()) > 0
	})
}
