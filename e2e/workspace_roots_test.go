package e2e

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
)

// TestWorkspaceRoots walks the workspace roots (borrowed item 15,
// docs/machine-ops.md §5) with real binaries: with none set the machine
// does as before (commands and projects anywhere); the owner sets them on
// server with `aided roots set`; then an agent's command starts only
// inside them, through the relay, refused with kind outside_roots for a
// folder outside, ".." or a link out of a root; the owner's app browses
// and adds projects only inside them; the project added before stays
// usable, and `aided roots` names it; cleared, anywhere again.
func TestWorkspaceRoots(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	env := []string{"AIDE_FAKE_PROVIDERS=1"}
	w := newOwnerMac(t, env, &localv1.GrantSpec{})
	ctx := context.Background()
	cliEnv := append(w.cliEnv, "AIDE_LOCAL=off") // through the relay
	cli := func(args ...string) result { return run(t, cliEnv, time.Minute, filepath.Join(w.bin, "aide"), args...) }
	daemon, data := filepath.Join(w.bin, "aided"), filepath.Join(w.tmp, "vps-1")
	// The owner, in a shell on the server.
	aided := func(args ...string) result {
		return run(t, nil, time.Minute, daemon, append(args, "--data-dir", data)...)
	}
	base := t.TempDir()
	root, out := filepath.Join(base, "root"), filepath.Join(base, "out")
	for _, d := range []string{filepath.Join(root, "app"), filepath.Join(out, "app"), filepath.Join(out, "old")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// A server of the owner's, with a project from before there were roots.
	mid := w.loginMachine("vps-1", data, env)
	if r := aided("project", "add", filepath.Join(out, "old"), "--in-place"); r.code != 0 {
		t.Fatalf("aided project add: %+v", r)
	}
	vps := start(t, "vps-1", env, daemon, "run", "--data-dir", data)
	w.logs = append(w.logs, vps)
	eventually(t, 30*time.Second, "vps-1 online", func() bool {
		res, err := w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
		return err == nil && slices.ContainsFunc(res.Machines, func(m *localv1.Machine) bool { return m.MachineName == "vps-1" && m.Online })
	})

	clients, err := w.app.client.ListClients(ctx, &localv1.ListClientsRequest{})
	if err != nil || len(clients.Clients) != 1 {
		t.Fatalf("clients: %v %v", clients, err)
	}
	spec := proto.CloneOf(clients.Clients[0].Spec)
	spec.MachineExec = true
	if _, err := w.app.client.UpdateClient(ctx, &localv1.UpdateClientRequest{GrantId: clients.Clients[0].GrantId, Spec: spec}); err != nil {
		t.Fatal(err)
	}

	// None set: as before, anywhere.
	if r := aided("roots"); r.code != 0 || !strings.Contains(r.stdout, "No workspace roots") {
		t.Fatalf("aided roots: %+v", r)
	}
	eventually(t, 30*time.Second, "exec allowed", func() bool {
		return cli("machine", "exec", "vps-1", "--workdir", out, "--", "echo hi").code == 0
	})
	if _, err := w.app.client.ListMachineFolders(ctx, &localv1.ListMachineFoldersRequest{MachineId: mid, Path: out}); err != nil {
		t.Fatalf("browsing without roots: %v", err)
	}

	// The owner sets the roots on the server; the project from before,
	// outside them, is named and stays.
	r := aided("roots", "set", root)
	if r.code != 0 || !strings.Contains(r.stdout, "projects outside them stay usable: old") {
		t.Fatalf("aided roots set: %+v", r)
	}
	if r := aided("roots"); r.code != 0 || !strings.Contains(r.stdout, "root") || !strings.Contains(r.stdout, "! project old") {
		t.Fatalf("aided roots after set: %+v", r)
	}

	// Commands: inside run, outside are refused, told why and what to do.
	if r := cli("machine", "exec", "vps-1", "--workdir", filepath.Join(root, "app"), "--", "echo inside"); r.code != 0 || !strings.Contains(r.stdout, "inside") {
		t.Fatalf("exec inside: %+v", r)
	}
	escapes := []string{out, root + string(filepath.Separator) + filepath.Join("..", "out")}
	if err := os.Symlink(out, filepath.Join(root, "link")); err == nil {
		escapes = append(escapes, filepath.Join(root, "link"))
	} else {
		t.Logf("no symbolic links here (%v): that escape is left to the unit test", err)
	}
	for _, dir := range escapes {
		r := cli("machine", "exec", "vps-1", "--workdir", dir, "-o", "json", "--", "echo leaked")
		doc := jsonDoc(t, r)
		e := errOf(doc)
		if r.code != 3 || e["kind"] != "outside_roots" || !strings.Contains(e["hint"].(string), "aided roots") ||
			!strings.Contains(e["message"].(string), "workspace roots") || strings.Contains(r.stdout, "leaked\n") {
			t.Fatalf("exec in %s: %+v", dir, r)
		}
	}
	if r := cli("machine", "exec", "vps-1", "--workdir", out, "--", "echo leaked"); r.code != 3 || !strings.Contains(r.stderr, "aided roots") {
		t.Fatalf("exec outside, text: %+v", r)
	}

	// The owner's app, away from the server: browsing and adding projects
	// inside only.
	if _, err := w.app.client.ListMachineFolders(ctx, &localv1.ListMachineFoldersRequest{MachineId: mid, Path: out}); err == nil ||
		!strings.Contains(err.Error(), "workspace roots") {
		t.Fatalf("browsing outside: %v", err)
	}
	if res, err := w.app.client.ListMachineFolders(ctx, &localv1.ListMachineFoldersRequest{MachineId: mid, Path: root}); err != nil || len(res.Folders) != 1 {
		t.Fatalf("browsing inside: %v %v", res, err)
	}
	if _, err := w.app.client.AddProject(ctx, &localv1.AddProjectRequest{MachineId: mid, ProjectId: "outside", Path: filepath.Join(out, "app")}); err == nil ||
		!strings.Contains(err.Error(), "workspace roots") {
		t.Fatalf("a project outside: %v", err)
	}
	if _, err := w.app.client.AddProject(ctx, &localv1.AddProjectRequest{MachineId: mid, ProjectId: "inside", Path: filepath.Join(root, "app")}); err != nil {
		t.Fatalf("a project inside: %v", err)
	}
	if r := aided("project", "add", filepath.Join(out, "app"), "--in-place"); r.code == 0 || !strings.Contains(r.stderr, "workspace roots") {
		t.Fatalf("aided project add outside: %+v", r)
	}

	// The project from before still works.
	if r := cli("session", "start", "--project", "vps-1/old", "--wait", "--prompt", "say still here", "-o", "json"); r.code != 0 || firstSession(r.stdout).SessionID == "" {
		t.Fatalf("a session in the project from before the roots: %+v", r)
	}

	// Cleared: anywhere again.
	if r := aided("roots", "clear"); r.code != 0 || !strings.Contains(r.stdout, "cleared") {
		t.Fatalf("aided roots clear: %+v", r)
	}
	if r := cli("machine", "exec", "vps-1", "--workdir", out, "--", "echo again"); r.code != 0 || !strings.Contains(r.stdout, "again") {
		t.Fatalf("exec after clearing: %+v", r)
	}
}
