package acp

import (
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// DeepSeek Harness's own providers (工单 #129). Aide starts dsh with
// `--profile acp`, a profile of its own; the DSH desktop app keeps its
// profile, `desktop`, to itself (the CLI refuses to boot it), and the
// providers its owner added there — gateways of their own, routed by the
// llm-pi-ai plugin — are in that profile's patch layer only
// (<DSH home>/profiles/desktop/cordis.patch.yml). Booted without them,
// the acp profile's llm-pi-ai row has no routes, and a session offers
// DeepSeek's own models only. So each dsh process Aide starts gets a
// --patch overlay giving the acp profile's llm-pi-ai row the desktop
// profile's providers as well: those the acp profile has itself stay as
// they are. The credentials they name are DSH's own, shared by its
// profiles (its credentials plugin), so nothing else is needed; nothing
// of the owner's DSH configuration is written.

const dshProvidersPlugin = "@deepseek-ai/dsh-llm-pi-ai"

// dshHome is where DSH keeps its profiles: $DSH_HOME, else ~/.dsh.
func dshHome(env []string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if k, v, ok := strings.Cut(env[i], "="); ok && k == "DSH_HOME" {
			if v != "" {
				return v
			}
			break
		}
	}
	if env == nil {
		if v := os.Getenv("DSH_HOME"); v != "" {
			return v
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".dsh")
}

// dshDesktopProviders is the providers table the desktop profile's patch
// layer gives the llm-pi-ai row, or nil.
func dshDesktopProviders(env []string) *yaml.Node {
	home := dshHome(env)
	if home == "" {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(home, "profiles", "desktop", "cordis.patch.yml"))
	if err != nil {
		return nil
	}
	var doc yaml.Node
	if yaml.Unmarshal(b, &doc) != nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.SequenceNode {
		return nil
	}
	for _, e := range doc.Content[0].Content {
		if !isProvidersRow(e) {
			continue
		}
		if ps := value(value(e, "config"), "providers"); ps != nil && ps.Kind == yaml.MappingNode && len(ps.Content) > 0 {
			return ps
		}
	}
	return nil
}

// isProvidersRow: a patch entry for the llm-pi-ai row (by its id, or its plugin's name).
func isProvidersRow(e *yaml.Node) bool {
	if e.Kind != yaml.MappingNode {
		return false
	}
	id, name := value(e, "id"), value(e, "name")
	return id != nil && id.Value == "llm-pi-ai" || name != nil && name.Value == dshProvidersPlugin
}

// dshProvidersPatch is the overlay giving the llm-pi-ai row of dump (the
// acp profile, as dsh composes it) the providers of desktop it does not
// have, and their names; nil when there are none to add, or the row is
// not there or off. The overlay replaces the row's whole config, so it is
// the row's own, with only providers added to.
func dshProvidersPatch(dump []byte, desktop *yaml.Node) ([]byte, []string, error) {
	if desktop == nil {
		return nil, nil, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(dump, &doc); err != nil || len(doc.Content) == 0 {
		return nil, nil, err
	}
	row := findDSHRow(doc.Content[0], dshProvidersPlugin)
	if row == nil {
		return nil, nil, nil
	}
	id := value(row, "id")
	if d := value(row, "disabled"); id == nil || id.Value == "" || d != nil && d.Tag == "!!bool" && d.Value == "true" {
		return nil, nil, nil
	}
	config := &yaml.Node{Kind: yaml.MappingNode}
	if c := value(row, "config"); c != nil && c.Kind == yaml.MappingNode {
		config = deepCopy(c)
	}
	providers := value(config, "providers")
	if providers == nil || providers.Kind != yaml.MappingNode {
		if providers != nil { // not a table: left as dsh would read it
			return nil, nil, nil
		}
		providers = &yaml.Node{Kind: yaml.MappingNode}
		config.Content = append(config.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "providers"}, providers)
	}
	var added []string
	for i := 0; i+1 < len(desktop.Content); i += 2 {
		name := desktop.Content[i].Value
		if value(providers, name) != nil {
			continue // the acp profile's own
		}
		providers.Content = append(providers.Content, deepCopy(desktop.Content[i]), deepCopy(desktop.Content[i+1]))
		added = append(added, name)
	}
	if len(added) == 0 {
		return nil, nil, nil
	}
	b, err := yaml.Marshal([]any{map[string]any{"id": id.Value, "config": config}})
	return b, added, err
}

// dshProviderArgs are the arguments giving a dsh process the desktop
// profile's providers (dshProvidersPatch), and the overlay's file, in
// dir, to remove once the process has read it; none when there are none
// to add.
func dshProviderArgs(dump []byte, desktop *yaml.Node, dir string) (args []string, file string, err error) {
	b, _, err := dshProvidersPatch(dump, desktop)
	if err != nil || b == nil {
		return nil, "", err
	}
	f, err := os.CreateTemp(dir, "dsh-providers-*.yml")
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
