package acp

import (
	"cmp"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"go.yaml.in/yaml/v3"

	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/provider"
)

// dshDump is part of what `dsh --profile acp --dump-config` prints
// (0.2.2-rc.3): its permission row, and others with !!js values.
const dshDump = `# == @deepseek-ai/dsh-base
- id: sandbox-policy
  name: '@deepseek-ai/dsh-sandbox-policy'
  config:
    mode: !!js process.env.DSH_PERMISSION_MODE ?? 'workspace-write'
- id: pwsh-sandbox
  name: '@deepseek-ai/dsh-pwsh-sandbox'
  disabled: !!js process.platform !== 'win32'
- id: permission
  name: '@deepseek-ai/dsh-permission-presets'
  config:
    presets:
      read-only:
        sandbox: read-only
        approval: ask
      workspace-write:
        sandbox: workspace-write
        approval: ask
        name: Workspace
      danger-full-access:
        sandbox: danger-full-access
        approval: never
        description: !!js "'Full ' + 'access'"
- id: shell-env
  name: '@deepseek-ai/dsh-shell-env'
`

func TestDSHPresets(t *testing.T) {
	ps, err := parseDSHPresets([]byte(dshDump))
	if err != nil || ps == nil {
		t.Fatal(ps, err)
	}
	if len(ps.modes) != 3 || ps.modes[0].ID != "read-only" || ps.modes[1].Name != "Workspace" || ps.modes[2].ID != "danger-full-access" || ps.own != "" || ps.id != "permission" {
		t.Fatalf("%+v", ps)
	}
	b, err := ps.patch("read-only")
	if err != nil {
		t.Fatal(err)
	}
	// The row's whole config, as composed (an expression too), with only
	// the default for new sessions changed.
	var got []struct {
		ID     string
		Config yaml.Node
	}
	if err := yaml.Unmarshal(b, &got); err != nil || len(got) != 1 || got[0].ID != "permission" {
		t.Fatalf("%s: %v", b, err)
	}
	if again, _ := parseDSHPresets([]byte("- name: '@deepseek-ai/dsh-permission-presets'\n  id: permission\n  config:\n" + indent(string(mustMarshal(t, &got[0].Config)), "    "))); again == nil ||
		again.own != "read-only" || len(again.modes) != 3 || !strings.Contains(string(b), "description: !!js") {
		t.Fatalf("patch:\n%s", b)
	}
	// A default already configured is replaced, not repeated.
	ps2, _ := parseDSHPresets([]byte(strings.Replace(dshDump, "    presets:\n", "    defaultPreset: danger-full-access\n    presets:\n", 1)))
	if ps2.own != "danger-full-access" {
		t.Fatalf("own: %+v", ps2)
	}
	if b, _ := ps2.patch("workspace-write"); strings.Count(string(b), "defaultPreset") != 1 || !strings.Contains(string(b), "defaultPreset: workspace-write") {
		t.Fatalf("patch:\n%s", b)
	}
	if _, err := ps.patch("yolo"); err == nil || !strings.Contains(err.Error(), "available: read-only, workspace-write, danger-full-access") {
		t.Fatal(err)
	}

	// No config: the plugin's own presets, and the patch sets the default only.
	ps3, _ := parseDSHPresets([]byte("- id: perm\n  name: '@deepseek-ai/dsh-permission-presets'\n"))
	if len(ps3.modes) != 2 || ps3.modes[0].ID != "workspace-write" {
		t.Fatalf("%+v", ps3)
	}
	if b, _ := ps3.patch("danger-full-access"); string(b) != "- config:\n    defaultPreset: danger-full-access\n  id: perm\n" {
		t.Fatalf("patch:\n%s", b)
	}
	// None: a profile without the plugin, or with it disabled.
	for _, dump := range []string{"[]\n", "", "- id: x\n  name: y\n", "- id: p\n  name: '@deepseek-ai/dsh-permission-presets'\n  disabled: true\n"} {
		if ps, err := parseDSHPresets([]byte(dump)); ps != nil || err != nil {
			t.Fatalf("%q: %+v %v", dump, ps, err)
		}
	}
}

// DSH's permission presets are its modes, known before a session; each
// session's process starts with the one chosen, so two sessions at once
// with different presets each have their own: the read-only one cannot
// write, the other can. Unchosen, a session has DSH's own default. A
// resumed session keeps its preset; one DSH does not have fails before
// its process starts, and no overlay is left behind.
func TestDSHPresetModes(t *testing.T) {
	for _, own := range []string{"", "read-only"} {
		f := newFixture(t, "FAKE_ACP_PRESETS="+own, "FAKE_ACP_CONFIG=1", "FAKE_ACP_MODELS=v4-pro", "FAKE_ACP_RESUME=resume")
		f.p = &Provider{Agent: Agent{Name: "deepseek", Command: "dsh", Args: []string{"--profile", "acp"}, DSHPresets: true}, Binary: fakeBin}
		modes := []provider.Mode{{ID: "read-only", Name: "read-only"}, {ID: "workspace-write", Name: "workspace-write"}, {ID: "danger-full-access", Name: "danger-full-access"}}
		t.Setenv("FAKE_ACP_PRESETS", own) // the machine's environment, which agents are checked in
		if info := f.p.Discover(context.Background()); !info.Available || !reflect.DeepEqual(info.Modes, modes) || info.Own.Mode != own {
			t.Fatalf("discovered: %+v", info)
		}
		type ran struct {
			ref     provider.Event
			text    string
			results []provider.Event
			tmp     string
		}
		run := func(mode, resume, dir string) ran {
			t.Helper()
			tmp := t.TempDir()
			s, err := f.p.Start(context.Background(), provider.StartSpec{WorkDir: dir, TempDir: tmp, Mode: mode, ResumeRef: resume, Env: f.env, Gate: f.gate.gate,
				Prompt: "write out.txt hi\nconfig"})
			if err != nil {
				t.Errorf("%s: %v", mode, err)
				return ran{}
			}
			defer s.Close()
			events, done := collect(t, s)
			return ran{of(events, provider.EventRef)[0], done.Text, of(events, provider.EventToolResult), tmp}
		}
		ro, rw := t.TempDir(), t.TempDir()
		var a, b ran
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); a = run("read-only", "", ro) }()
		go func() { defer wg.Done(); b = run("workspace-write", "", rw) }()
		wg.Wait()
		if a.text != "config: v4-pro  read-only" || len(a.results) != 1 || !a.results[0].IsError || exists(filepath.Join(ro, "out.txt")) {
			t.Fatalf("read-only: %+v", a)
		}
		if b.text != "config: v4-pro  workspace-write" || len(b.results) != 1 || b.results[0].IsError || !exists(filepath.Join(rw, "out.txt")) {
			t.Fatalf("workspace-write: %+v", b)
		}
		for _, r := range []ran{a, b} {
			if o := r.ref.Offers; o == nil || !reflect.DeepEqual(o.Modes, modes) || o.Own.Mode != own || len(o.Models) != 1 {
				t.Fatalf("offers: %+v", r.ref.Offers)
			}
			if left, _ := os.ReadDir(r.tmp); len(left) != 0 {
				t.Fatalf("left behind: %v", left)
			}
		}
		// Unchosen: DSH's own default.
		if c := run("", "", t.TempDir()); c.text != "config: v4-pro  "+cmp.Or(own, "workspace-write") {
			t.Fatalf("unchosen: %+v", c)
		}
		// Resumed (a new process), the session keeps its preset.
		if r := run("read-only", a.ref.Ref, ro); r.text != "config: v4-pro  read-only" || len(r.results) != 1 || !r.results[0].IsError || r.ref.Offers != nil {
			t.Fatalf("resumed: %+v", r)
		}
		_, err := f.p.Start(context.Background(), provider.StartSpec{WorkDir: ro, TempDir: t.TempDir(), Mode: "yolo", Prompt: "config", Env: f.env})
		if err == nil || kind(err) != sessionv1.FailureKind_FAILURE_KIND_INVALID_REQUEST ||
			!strings.Contains(err.Error(), `mode "yolo": not offered by the agent (available: read-only, workspace-write, danger-full-access)`) {
			t.Fatal(err)
		}
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func mustMarshal(t *testing.T, v any) []byte {
	b, err := yaml.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func indent(s, by string) string {
	return by + strings.ReplaceAll(strings.TrimRight(s, "\n"), "\n", "\n"+by) + "\n"
}
