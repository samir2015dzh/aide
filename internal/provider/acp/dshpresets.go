package acp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/samir2015dzh/aide/internal/provider"
)

// DeepSeek Harness's permission presets (read-only, workspace-write,
// danger-full-access, and any others its configuration has) are its
// sessions' modes, but its ACP bridge offers no modes: a session gets the
// preset its process's configuration names for new sessions (the
// permission row's defaultPreset, @deepseek-ai/dsh-permission-presets),
// when it is created, and keeps it in its own log (resumed, it has it
// still). Aide starts one dsh process per session, so it starts a new
// session's process with a --patch overlay naming the chosen preset: no
// other session, and none of the owner's DSH configuration, changes.
// The overlay replaces the permission row's whole config, so it is that
// row as `dsh --dump-config` composes it (the owner's own layers too),
// with only defaultPreset changed.
//
// ACP has no such thing for agents in general; this is the one place that
// knows DSH's configuration (tested on 0.2.1-rc.5 and 0.2.2-rc.3).

const dshPresetsPlugin = "@deepseek-ai/dsh-permission-presets"

// dshPresetsDefault is the plugin's preset table when its row has none.
var dshPresetsDefault = []string{"workspace-write", "danger-full-access"}

// dshPresets is the permission row of a dsh profile's configuration.
type dshPresets struct {
	modes  []provider.Mode
	own    string // the configured default ("": DSH infers it)
	id     string
	config *yaml.Node // nil: none (the plugin's defaults)
}

// readDSHPresets reads the presets of the profile cmd starts (cmd is the
// dsh command and its arguments, ending in `--profile <name>`); nil when
// it has none.
func readDSHPresets(ctx context.Context, env []string, cmd []string) (*dshPresets, error) {
	out, err := dumpDSH(ctx, env, cmd)
	if err != nil {
		return nil, err
	}
	return parseDSHPresets(out)
}

// dumpDSH is the configuration of the profile cmd starts, as dsh composes it.
func dumpDSH(ctx context.Context, env []string, cmd []string) ([]byte, error) {
	out, problem, detail := provider.Probe(ctx, env, cmd[0], append(slices.Clone(cmd[1:]), "--dump-config")...)
	if problem != provider.ProblemNone {
		return nil, fmt.Errorf("reading its configuration (--dump-config) failed: %s", strings.TrimSpace(detail))
	}
	return out, nil
}

func parseDSHPresets(dump []byte) (*dshPresets, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(dump, &doc); err != nil {
		return nil, fmt.Errorf("reading its configuration: %w", err)
	}
	if len(doc.Content) == 0 {
		return nil, nil
	}
	row := findDSHRow(doc.Content[0], dshPresetsPlugin)
	if row == nil {
		return nil, nil
	}
	id, config := value(row, "id"), value(row, "config")
	if d := value(row, "disabled"); id == nil || id.Value == "" || d != nil && d.Tag == "!!bool" && d.Value == "true" {
		return nil, nil
	}
	ps := &dshPresets{id: id.Value, config: config}
	names := dshPresetsDefault
	if ps.config != nil {
		if ps.config.Kind != yaml.MappingNode {
			return nil, errors.New("its permission presets are not a table")
		}
		if t := value(ps.config, "presets"); t != nil {
			if t.Kind != yaml.MappingNode {
				return nil, errors.New("its permission presets are not a table")
			}
			names = nil
			for i := 0; i+1 < len(t.Content); i += 2 {
				names = append(names, t.Content[i].Value)
			}
		}
		if d := value(ps.config, "defaultPreset"); d != nil && d.Tag == "!!str" {
			ps.own = d.Value
		}
	}
	for _, n := range names {
		name := n
		if ps.config != nil {
			if spec := value(value(value(ps.config, "presets"), n), "name"); spec != nil && spec.Tag == "!!str" && spec.Value != "" {
				name = spec.Value
			}
		}
		ps.modes = append(ps.modes, provider.Mode{ID: n, Name: name})
	}
	return ps, nil
}

// findDSHRow is plugin's entry in a list of entries, or in a group's.
func findDSHRow(list *yaml.Node, plugin string) *yaml.Node {
	if list.Kind != yaml.SequenceNode {
		return nil
	}
	for _, e := range list.Content {
		if e.Kind != yaml.MappingNode {
			continue
		}
		if n := value(e, "name"); n != nil && n.Value == plugin {
			return e
		}
		if c := value(e, "config"); c != nil {
			if r := findDSHRow(c, plugin); r != nil {
				return r
			}
		}
	}
	return nil
}

// value is a mapping's value for key (nil if none).
func value(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// patch is the --patch overlay that gives new sessions preset mode.
func (ps *dshPresets) patch(mode string) ([]byte, error) {
	var ids []string
	for _, m := range ps.modes {
		ids = append(ids, m.ID)
	}
	if !slices.Contains(ids, mode) {
		return nil, fmt.Errorf("not offered by the agent (available: %s)", strings.Join(ids, ", "))
	}
	config := &yaml.Node{Kind: yaml.MappingNode}
	if ps.config != nil {
		config = deepCopy(ps.config)
	}
	preset := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: mode}
	if d := value(config, "defaultPreset"); d != nil {
		*d = *preset
	} else {
		config.Content = append(config.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "defaultPreset"}, preset)
	}
	return yaml.Marshal([]any{map[string]any{"id": ps.id, "config": config}})
}

func deepCopy(n *yaml.Node) *yaml.Node {
	c := *n
	c.Content = nil
	for _, x := range n.Content {
		c.Content = append(c.Content, deepCopy(x))
	}
	return &c
}

// dshPresetArgs are the arguments that start a new session with preset
// mode, and the overlay's file, in dir, to remove once the process has
// read it.
func dshPresetArgs(ps *dshPresets, mode, dir string) (args []string, file string, err error) {
	b, err := ps.patch(mode)
	if err != nil {
		return nil, "", err
	}
	f, err := os.CreateTemp(dir, "dsh-permission-*.yml")
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	if _, err := f.Write(b); err != nil {
		os.Remove(f.Name())
		return nil, "", err
	}
	return []string{"--patch", f.Name()}, f.Name(), nil
}
