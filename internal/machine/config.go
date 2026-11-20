package machine

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"connectrpc.com/connect"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
)

// A machine's configuration as one file (`aided config export` and
// `apply`, docs/machine-ops.md §6): its workspace roots, projects, agent
// defaults and custom agents, for a version control repository. No secret
// is in it: a custom agent's secret variable is named, never its value.
//
// Apply goes through ApplyLocal, as `aided project` and `aided roots` do:
// the machine's own user changes the projects and the workspace roots.
// Agent defaults and custom agents are set from the owner's devices only;
// apply compares them and names what differs, and changes none.

// ConfigVersion is the file's format.
const ConfigVersion = 1

// ConfigFile is the file. A section left out (nil) is left as it is; a
// project or agent not in the file is kept.
type ConfigFile struct {
	Version        int                   `yaml:"version" json:"version"`
	WorkspaceRoots *[]string             `yaml:"workspace_roots,omitempty" json:"workspace_roots"`
	Projects       []ConfigProject       `yaml:"projects,omitempty" json:"projects"`
	AgentDefaults  []ConfigAgentDefaults `yaml:"agent_defaults,omitempty" json:"agent_defaults"`
	CustomAgents   []ConfigCustomAgent   `yaml:"custom_agents,omitempty" json:"custom_agents"`
}

type ConfigProject struct {
	ID          string   `yaml:"id" json:"id"`
	Path        string   `yaml:"path" json:"path"`
	Workspace   string   `yaml:"workspace" json:"workspace"` // worktree or in_place
	Base        string   `yaml:"base,omitempty" json:"base"`
	Description string   `yaml:"description,omitempty" json:"description"`
	Agents      []string `yaml:"agents,omitempty" json:"agents"`
}

type ConfigAgentDefaults struct {
	Agent  string `yaml:"agent" json:"agent"`
	Model  string `yaml:"model,omitempty" json:"model"`
	Effort string `yaml:"effort,omitempty" json:"effort"`
	Speed  string `yaml:"speed,omitempty" json:"speed"` // standard or fast (Codex)
	Mode   string `yaml:"mode,omitempty" json:"mode"`
}

type ConfigCustomAgent struct {
	Name    string      `yaml:"name" json:"name"`
	Command string      `yaml:"command" json:"command"`
	Args    []string    `yaml:"args,omitempty" json:"args"`
	Dir     string      `yaml:"dir,omitempty" json:"dir"`
	Env     []ConfigEnv `yaml:"env,omitempty" json:"env"`
}

// ConfigEnv is a custom agent's variable; a secret one has no value.
type ConfigEnv struct {
	Name   string `yaml:"name" json:"name"`
	Value  string `yaml:"value,omitempty" json:"value"`
	Secret bool   `yaml:"secret,omitempty" json:"secret"`
}

// ConfigChange is one thing apply changes, or would.
type ConfigChange struct {
	Section string `json:"section"` // workspace_roots, projects, agent_defaults, custom_agents
	Name    string `json:"name"`    // the project, agent or ""
	Action  string `json:"action"`  // add or update
	// OwnerOnly: set from the owner's devices only; apply leaves it.
	OwnerOnly bool `json:"owner_only"`
	Applied   bool `json:"applied"`
}

// ConfigError is why apply refused, in the CLI's terms (docs/cli-output.md §2).
type ConfigError struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Hint    string `json:"hint"`
}

func (e *ConfigError) Error() string { return e.Message }

// ConfigResult is what apply did, or would do.
type ConfigResult struct {
	DryRun  bool           `json:"dry_run"`
	Changes []ConfigChange `json:"changes"`
	Error   *ConfigError   `json:"error,omitempty"`
}

var speedNames = map[identityv1.AgentSpeed]string{identityv1.AgentSpeed_AGENT_SPEED_STANDARD: "standard", identityv1.AgentSpeed_AGENT_SPEED_FAST: "fast"}

func workspaceName(w identityv1.Workspace) string {
	if w == identityv1.Workspace_WORKSPACE_IN_PLACE {
		return "in_place"
	}
	return "worktree"
}

// ExportConfig is this machine's configuration. Projects the machine made
// for a repository's sessions (session start --repo) are not in it.
func (n *Machine) ExportConfig() (ConfigFile, error) {
	roots := n.WorkspaceRoots()
	if roots == nil {
		roots = []string{}
	}
	c := ConfigFile{Version: ConfigVersion, WorkspaceRoots: &roots, Projects: []ConfigProject{}, AgentDefaults: []ConfigAgentDefaults{}, CustomAgents: []ConfigCustomAgent{}}
	projects, err := n.st.Projects()
	if err != nil {
		return c, err
	}
	clones := filepath.Join(n.cfg.DataDir, "repos")
	for _, p := range projects {
		if inRoot(clones, p.Path) {
			continue
		}
		agents := p.Agents
		if agents == nil {
			agents = []string{}
		}
		c.Projects = append(c.Projects, ConfigProject{ID: p.ID, Path: p.Path, Workspace: workspaceName(p.Workspace), Base: p.DefaultBaseRef, Description: p.Description, Agents: agents})
	}
	n.mu.Lock()
	for _, a := range slices.Sorted(maps.Keys(n.defaults)) {
		d := n.defaults[a]
		if d.Model+d.Effort+d.Mode == "" && d.Speed == identityv1.AgentSpeed_AGENT_SPEED_UNSPECIFIED {
			continue
		}
		c.AgentDefaults = append(c.AgentDefaults, ConfigAgentDefaults{Agent: a, Model: d.Model, Effort: d.Effort, Speed: speedNames[d.Speed], Mode: d.Mode})
	}
	n.mu.Unlock()
	n.customMu.RLock()
	for _, a := range n.customs.list {
		c.CustomAgents = append(c.CustomAgents, exportCustom(a))
	}
	n.customMu.RUnlock()
	slices.SortFunc(c.CustomAgents, func(a, b ConfigCustomAgent) int { return strings.Compare(a.Name, b.Name) })
	return c, nil
}

// exportCustom is a custom agent without its secrets' values (agents.json
// has none either; a secret is named, never valued).
func exportCustom(a customAgent) ConfigCustomAgent {
	c := ConfigCustomAgent{Name: a.Name, Command: a.Command, Args: a.Args, Dir: a.Dir}
	for _, e := range a.Env {
		c.Env = append(c.Env, ConfigEnv(e))
	}
	return withoutSecrets(c)
}

// withoutSecrets drops the value of each secret variable.
func withoutSecrets(a ConfigCustomAgent) ConfigCustomAgent {
	env := []ConfigEnv{}
	for _, e := range a.Env {
		if e.Secret || looksSecret(e.Name) {
			e = ConfigEnv{Name: e.Name, Secret: true}
		}
		env = append(env, e)
	}
	a.Env = env
	if a.Args == nil {
		a.Args = []string{}
	}
	return a
}

// ApplyConfig makes this machine's projects and workspace roots what c
// says, through ApplyLocal; dryRun changes nothing. It checks everything
// first: a file with a wrong value or a project in conflict changes
// nothing. Agent defaults and custom agents that differ are named
// (OwnerOnly) and refused with kind denied, after the rest is applied.
func (n *Machine) ApplyConfig(ctx context.Context, c ConfigFile, dryRun bool) ConfigResult {
	res := ConfigResult{DryRun: dryRun, Changes: []ConfigChange{}}
	type step struct {
		i   int
		cmd *identityv1.AdminCommand
	}
	var steps []step
	fail := func(kind, hint string, format string, a ...any) ConfigResult {
		res.Error = &ConfigError{Kind: kind, Message: fmt.Sprintf(format, a...), Hint: hint}
		return res
	}
	if c.Version != ConfigVersion {
		return fail("usage", "Export one from this version: aided config export", "the file's version is %d; this aided reads version %d", c.Version, ConfigVersion)
	}

	roots := n.WorkspaceRoots()
	if c.WorkspaceRoots != nil {
		var want []string
		for _, r := range *c.WorkspaceRoots {
			p, err := realDir(expandHome(strings.TrimSpace(r)))
			if err != nil {
				return fail("not_found", "Make the folder on this machine, or change workspace_roots in the file", "workspace root %q: %v", r, err)
			}
			if !slices.Contains(want, p) {
				want = append(want, p)
			}
		}
		if !slices.Equal(want, roots) && !(len(want) == 0 && len(roots) == 0) {
			res.Changes = append(res.Changes, ConfigChange{Section: "workspace_roots", Action: "update"})
			steps = append(steps, step{len(res.Changes) - 1, &identityv1.AdminCommand{Command: &identityv1.AdminCommand_SetWorkspaceRoots{SetWorkspaceRoots: &identityv1.SetWorkspaceRoots{Roots: want}}}})
		}
		roots = want
	}

	seen := map[string]bool{}
	for _, p := range c.Projects {
		if seen[p.ID] {
			return fail("usage", "Keep one entry per project id in the file", "project %s is in the file twice", p.ID)
		}
		seen[p.ID] = true
		if !projectIDRE.MatchString(p.ID) {
			return fail("usage", "Use lowercase letters, digits, '.', '_' or '-'", "project id %q is not a project id", p.ID)
		}
		var mode identityv1.Workspace
		switch p.Workspace {
		case "worktree":
			mode = identityv1.Workspace_WORKSPACE_WORKTREE
		case "in_place":
			mode = identityv1.Workspace_WORKSPACE_IN_PLACE
		case "":
		default:
			return fail("usage", "workspace is worktree or in_place", "project %s: unknown workspace %q", p.ID, p.Workspace)
		}
		agents, err := projectAgents(p.Agents, n.knownAgent)
		if err != nil {
			return fail("usage", "List coding agents this machine has (aided doctor)", "project %s: %v", p.ID, err)
		}
		mode, path, err := projectWorkspace(ctx, mode, p.Path)
		if err != nil {
			return fail("not_found", "Make the folder on this machine (git clone it for a worktree project), or change its path in the file", "project %s: %v", p.ID, err)
		}
		if err := checkRoots(roots, path); err != nil {
			return fail("outside_roots", "Add a workspace root that holds it to workspace_roots, or move the project", "project %s: %v", p.ID, err)
		}
		base := p.Base
		if mode == identityv1.Workspace_WORKSPACE_IN_PLACE {
			base = ""
		} else if base == "" {
			base = "HEAD"
		}
		want := Project{ID: p.ID, Path: path, DefaultBaseRef: base, Workspace: mode, Description: strings.TrimSpace(p.Description), Agents: agents}
		have, err := n.st.Project(p.ID)
		if err != nil {
			return fail("internal", "", "reading project %s: %v", p.ID, err)
		}
		action := "add"
		if have != nil {
			if have.Path != want.Path {
				return fail("invalid_state", "Remove it first (aided project remove "+p.ID+"), or give it another id in the file",
					"project %s is at %s on this machine, not %s", p.ID, have.Path, want.Path)
			}
			if have.DefaultBaseRef == want.DefaultBaseRef && have.Workspace == want.Workspace && have.Description == want.Description && slices.Equal(have.Agents, want.Agents) {
				continue
			}
			action = "update"
		}
		res.Changes = append(res.Changes, ConfigChange{Section: "projects", Name: p.ID, Action: action})
		steps = append(steps, step{len(res.Changes) - 1, &identityv1.AdminCommand{Command: &identityv1.AdminCommand_AddProject{AddProject: &identityv1.AddProject{
			ProjectId: p.ID, Path: path, Workspace: mode, DefaultBaseRef: base, Description: p.Description, Agents: agents}}}})
	}

	// Set from the owner's devices only: compared, not changed.
	n.mu.Lock()
	for _, d := range c.AgentDefaults {
		have := n.defaults[d.Agent]
		if have.GetModel() == d.Model && have.GetEffort() == d.Effort && speedNames[have.GetSpeed()] == d.Speed && have.GetMode() == d.Mode {
			continue
		}
		res.Changes = append(res.Changes, ConfigChange{Section: "agent_defaults", Name: d.Agent, Action: addOrUpdate(have != nil), OwnerOnly: true})
	}
	n.mu.Unlock()
	n.customMu.RLock()
	for _, a := range c.CustomAgents {
		k := slices.IndexFunc(n.customs.list, func(h customAgent) bool { return h.Name == a.Name })
		if k >= 0 && sameCustom(exportCustom(n.customs.list[k]), withoutSecrets(a)) {
			continue
		}
		res.Changes = append(res.Changes, ConfigChange{Section: "custom_agents", Name: a.Name, Action: addOrUpdate(k >= 0), OwnerOnly: true})
	}
	n.customMu.RUnlock()

	if !dryRun {
		for _, s := range steps {
			if _, err := n.ApplyLocal(ctx, s.cmd); err != nil {
				kind := "internal"
				switch {
				case IsOutsideRoots(err):
					kind = "outside_roots"
				case connect.CodeOf(err) == connect.CodeInvalidArgument:
					kind = "usage"
				}
				return fail(kind, "Fix the file and apply it again: what was applied stays", "%s %s: %v", res.Changes[s.i].Section, res.Changes[s.i].Name, err)
			}
			res.Changes[s.i].Applied = true
		}
	}
	var owner []string
	for _, ch := range res.Changes {
		if ch.OwnerOnly {
			owner = append(owner, ch.Section+" "+ch.Name)
		}
	}
	if len(owner) > 0 {
		verb := "were not changed"
		if dryRun {
			verb = "would not be changed"
		}
		return fail("denied", "Set them in the Aide app (Machines › this machine), or leave them out of the file; this machine's shell changes projects and workspace roots only",
			"%s %s: agent defaults and custom agents are set from the owner's devices", strings.Join(owner, ", "), verb)
	}
	return res
}

func sameCustom(a, b ConfigCustomAgent) bool {
	return a.Command == b.Command && slices.Equal(a.Args, b.Args) && a.Dir == b.Dir && slices.Equal(a.Env, b.Env)
}

func addOrUpdate(exists bool) string {
	if exists {
		return "update"
	}
	return "add"
}
