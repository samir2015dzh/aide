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

// TestDSHModelChoice (工单 #98): DSH tells its models only in a session.
// A new one names those its configuration has (here two, in a provider's
// group) and the one it runs; one started with a model chosen of them runs
// that one, beside a permission preset, and says so (the model a session
// runs, which the app reads back); one DSH does not offer fails the start.
func TestDSHModelChoice(t *testing.T) {
	f := newFixture(t, "FAKE_ACP_PRESETS=", "FAKE_ACP_CONFIG=1", "FAKE_ACP_MODELS=deepseek/v4-pro,deepseek/v4-flash", "FAKE_ACP_EFFORTS=deepseek/v4-flash=high|max")
	f.p = &Provider{Agent: Agent{Name: "deepseek", Command: "dsh", Args: []string{"--profile", "acp"}, DSHPresets: true}, Binary: fakeBin}
	run := func(model, mode string) (provider.Event, string) {
		t.Helper()
		s, err := f.p.Start(context.Background(), provider.StartSpec{WorkDir: f.dir, TempDir: t.TempDir(), Model: model, Mode: mode, Env: f.env, Gate: f.gate.gate, Prompt: "config"})
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		events, done := collect(t, s)
		return of(events, provider.EventRef)[0], done.Text
	}
	ref, ran := run("", "")
	if ref.Offers == nil || len(ref.Offers.Models) != 2 || ref.Offers.Models[1].ID != "deepseek/v4-flash" || ref.Offers.Own.Model != "deepseek/v4-pro" ||
		ref.Model != "deepseek/v4-pro" || ran != "config: deepseek/v4-pro  workspace-write" {
		t.Fatalf("unchosen: %+v %+v, ran %q", ref, ref.Offers, ran)
	}
	ref, ran = run("deepseek/v4-flash", "read-only")
	if ref.Model != "deepseek/v4-flash" || ran != "config: deepseek/v4-flash  read-only" || !reflect.DeepEqual(ref.Offers.Models[1].Efforts, []string{"high", "max"}) {
		t.Fatalf("chosen: %+v %+v, ran %q", ref, ref.Offers, ran)
	}
	if _, err := f.p.Start(context.Background(), provider.StartSpec{WorkDir: f.dir, TempDir: t.TempDir(), Model: "deepseek/v3", Env: f.env, Prompt: "config"}); err == nil ||
		!strings.Contains(err.Error(), `model "deepseek/v3": not offered by the agent (available: deepseek/v4-pro, deepseek/v4-flash)`) {
		t.Fatalf("not offered: %v", err)
	}
}

// dshProviders is how DSH offers its models (工单 #98 补充): by provider,
// its API and its account lines naming the same model differently, and a
// custom provider (magpie's group route) in a model option of its own.
const dshProviders = `[{"id":"llm","groups":[
 {"name":"DeepSeek","models":[{"value":"[\"deepseek\",\"deepseek-v4-flash\"]","name":"deepseek-v4-flash"},{"value":"[\"deepseek\",\"deepseek-v4-pro\"]","name":"deepseek-v4-pro"}]},
 {"name":"DeepSeek Account","models":[{"value":"[\"deepseek-account\",\"deepseek-v4-flash\"]","name":"DeepSeek-V4.1-Flash"}]}]},
 {"id":"custom_llm","groups":[{"name":"magpie","models":[{"value":"[\"magpie\",\"group/deepseek-v4.1\"]","name":"group/deepseek-v4.1"}]}]}]`

// TestDSHModelsByProvider (工单 #98 补充): each model DSH offers is listed
// once — the same model by its API and its account lines is one, named by
// its display name, both lines said — and every provider's, a custom one in
// a model option of its own too, is listed and runs when chosen; the line
// not listed can still be named.
func TestDSHModelsByProvider(t *testing.T) {
	f := newFixture(t, "FAKE_ACP_PRESETS=", "FAKE_ACP_CONFIG=1", "FAKE_ACP_PROVIDERS="+dshProviders)
	f.p = &Provider{Agent: Agent{Name: "deepseek", Command: "dsh", Args: []string{"--profile", "acp"}, DSHPresets: true}, Binary: fakeBin}
	run := func(model string) (provider.Event, string) {
		t.Helper()
		s, err := f.p.Start(context.Background(), provider.StartSpec{WorkDir: f.dir, TempDir: t.TempDir(), Model: model, Env: f.env, Gate: f.gate.gate, Prompt: "config"})
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		events, done := collect(t, s)
		return of(events, provider.EventRef)[0], done.Text
	}
	ref, _ := run("")
	// Named one way (工单 #117): deepseek-v4-pro has no display name, so each by its specific model ID.
	want := []provider.Model{
		{ID: `["deepseek","deepseek-v4-flash"]`, Name: "deepseek-v4-flash", Source: "DeepSeek · DeepSeek Account"},
		{ID: `["deepseek","deepseek-v4-pro"]`, Name: "deepseek-v4-pro", Source: "DeepSeek"},
		{ID: `["magpie","group/deepseek-v4.1"]`, Name: "group/deepseek-v4.1", Source: "magpie"},
	}
	if !reflect.DeepEqual(ref.Offers.Models, want) || ref.Offers.Own.Model != `["deepseek","deepseek-v4-flash"]` {
		t.Fatalf("offered: %+v", ref.Offers)
	}
	for _, m := range []string{`["magpie","group/deepseek-v4.1"]`, `["deepseek-account","deepseek-v4-flash"]`} {
		if ref, ran := run(m); ref.Model != m || ran != "config: "+m+"  workspace-write" {
			t.Fatalf("%s: ran %q, reported %q", m, ran, ref.Model)
		}
	}
	if _, err := f.p.Start(context.Background(), provider.StartSpec{WorkDir: f.dir, TempDir: t.TempDir(), Model: "magpie/none", Env: f.env, Prompt: "config"}); err == nil ||
		!strings.Contains(err.Error(), `["magpie","group/deepseek-v4.1"]`) {
		t.Fatalf("not offered: %v", err)
	}
}

// TestSameModelsOnce: one provider's lines, "account" aside, by its ID or its
// group; never by display name; other providers apart; one way of naming.
func TestSameModelsOnce(t *testing.T) {
	got := sameModelsOnce([]choice{
		{Value: "deepseek/deepseek-v4-flash", Name: "deepseek-v4-flash", Group: "DeepSeek"},
		{Value: "acct/DeepSeek_V4_Flash", Name: "DeepSeek-V4.1-Flash", Group: "DeepSeek Account"}, // same by its group
		{Value: "deepseek-account/deepseek-v4.1-flash", Name: "DeepSeek-V4.1-Flash"},              // another version
		{Value: "magpie/deepseek-v4-flash", Name: "deepseek-v4-flash", Group: "magpie"},           // another provider
	}, "acct/DeepSeek_V4_Flash")
	want := []provider.Model{
		{ID: "acct/DeepSeek_V4_Flash", Name: "deepseek-v4-flash", Source: "DeepSeek · DeepSeek Account"},
		{ID: "deepseek-account/deepseek-v4.1-flash", Name: "deepseek-v4.1-flash"},
		{ID: "magpie/deepseek-v4-flash", Name: "deepseek-v4-flash", Source: "magpie"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	// Every one with a display name: named by them.
	got = sameModelsOnce([]choice{
		{Value: "g/gemini-3-pro", Name: "Gemini 3 Pro", Group: "Google"},
		{Value: "g/gemini-3-flash", Name: "Gemini 3 Flash", Group: "Google"},
	}, "")
	if got[0].Name != "Gemini 3 Pro" || got[1].Name != "Gemini 3 Flash" {
		t.Fatalf("display names: %+v", got)
	}
}

// TestNormalizeModels (工单 #117): models kept by an earlier Aide, one per
// line and named "name (group)" (what the owner's list showed), are listed
// as a session's are now: the same model by DSH's API and account lines
// once, named one way, both lines said; reasoning efforts kept; and again,
// it changes nothing.
func TestNormalizeModels(t *testing.T) {
	kept := []provider.Model{
		{ID: `["deepseek","deepseek-v4-flash"]`, Name: "deepseek-v4-flash (DeepSeek)", Efforts: []string{"high", "max"}},
		{ID: `["deepseek","deepseek-v4-pro"]`, Name: "deepseek-v4-pro (DeepSeek)"},
		{ID: `["deepseek-account","deepseek-v4-flash"]`, Name: "DeepSeek-V4.1-Flash (DeepSeek Account)"},
	}
	want := []provider.Model{
		{ID: `["deepseek","deepseek-v4-flash"]`, Name: "deepseek-v4-flash", Source: "DeepSeek · DeepSeek Account", Efforts: []string{"high", "max"}},
		{ID: `["deepseek","deepseek-v4-pro"]`, Name: "deepseek-v4-pro", Source: "DeepSeek"},
	}
	got := NormalizeModels(kept, `["deepseek","deepseek-v4-flash"]`)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	if again := NormalizeModels(got, `["deepseek","deepseek-v4-flash"]`); !reflect.DeepEqual(again, want) {
		t.Fatalf("again %+v", again)
	}
}
