package machine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	"github.com/samir2015dzh/aide/internal/provider"
	"github.com/samir2015dzh/aide/internal/provider/acp"
	"github.com/samir2015dzh/aide/internal/shellenv"
)

// Custom agents: coding agents the owner added on this machine, ACP agents
// started with the command, arguments, environment and folder the owner
// gave, then listed, checked and run as the built-in ones are. They are
// kept in agents.json in the machine's data folder; the values of their
// secret variables (named like a key, a token or a password, or marked
// secret) in its credential store (sealSecret), never in that file. A
// change is in use at once: no restart.

// customAgentsFile and customSecretsFile are where they are kept, in the
// data folder.
const (
	customAgentsFile  = "agents.json"
	customSecretsFile = "keys/agent-secrets.json"
)

// maxCustomAgents bounds what a machine keeps (and checks each time).
const maxCustomAgents = 32

var (
	customNameRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	envNameRE    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
	// secretNameRE: a variable named like this holds a secret, whatever
	// the owner said.
	secretNameRE = regexp.MustCompile(`(?i)KEY|TOKEN|SECRET|PASSW|CREDENTIAL|AUTH|COOKIE|PRIVATE`)
)

// customAgent is an agent as agents.json keeps it; a secret variable has
// no value there.
type customAgent struct {
	Name            string      `json:"name"`
	Command         string      `json:"command"`
	Args            []string    `json:"args,omitempty"`
	Env             []customEnv `json:"env,omitempty"`
	Dir             string      `json:"dir,omitempty"`
	Source          string      `json:"source,omitempty"`
	RegistryID      string      `json:"registry_id,omitempty"`
	RegistryVersion string      `json:"registry_version,omitempty"`
	Distribution    string      `json:"distribution,omitempty"`
}
type customEnv struct {
	Name   string `json:"name"`
	Value  string `json:"value,omitempty"`
	Secret bool   `json:"secret,omitempty"`
}

// customFile is agents.json.
type customFile struct {
	Agents []customAgent `json:"agents"`
}

// secretsFile is the credential store's file: each agent's secret values
// by variable, sealed (sealSecret).
type secretsFile map[string]map[string][]byte

// customAgents are this machine's, with the values of their secrets.
type customAgents struct {
	list    []customAgent
	secrets map[string]map[string]string
}

// looksSecret: a variable's value is a secret by its name.
func looksSecret(name string) bool { return secretNameRE.MatchString(name) }

// loadCustomAgents reads agents.json and the secrets, and makes their
// providers. What can't be read is logged: the machine runs without them.
func (n *Machine) loadCustomAgents() {
	c, err := readCustomAgents(n.cfg.DataDir)
	if err != nil {
		n.log.Warn("custom agents not loaded", "err", err)
		return
	}
	n.customMu.Lock()
	defer n.customMu.Unlock()
	n.customs = c
	n.custom = map[string]provider.Provider{}
	for _, a := range c.list {
		n.custom[a.Name] = customProvider(a, c.secrets[a.Name])
	}
}

func readCustomAgents(dataDir string) (customAgents, error) {
	c := customAgents{secrets: map[string]map[string]string{}}
	b, err := os.ReadFile(filepath.Join(dataDir, customAgentsFile))
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	var f customFile
	if err := json.Unmarshal(b, &f); err != nil {
		return c, fmt.Errorf("%s: %w", customAgentsFile, err)
	}
	c.list = f.Agents
	b, err = os.ReadFile(filepath.Join(dataDir, customSecretsFile))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return c, err
	}
	var sealed secretsFile
	if len(b) > 0 {
		if err := json.Unmarshal(b, &sealed); err != nil {
			return c, fmt.Errorf("%s: %w", customSecretsFile, err)
		}
	}
	for agent, vars := range sealed {
		c.secrets[agent] = map[string]string{}
		for name, v := range vars {
			plain, err := openSecret(v)
			if err != nil {
				return c, fmt.Errorf("opening %s's %s: %w", agent, name, err)
			}
			c.secrets[agent][name] = string(plain)
		}
	}
	return c, nil
}

// write saves c: the secrets first, then the agents that name them; each
// file whole under another name, then renamed over the old one.
func (c customAgents) write(dataDir string) error {
	sealed := secretsFile{}
	for agent, vars := range c.secrets {
		if len(vars) == 0 {
			continue
		}
		sealed[agent] = map[string][]byte{}
		for name, v := range vars {
			b, err := sealSecret([]byte(v))
			if err != nil {
				return fmt.Errorf("sealing %s's %s (%s): %w", agent, name, secretStore, err)
			}
			sealed[agent][name] = b
		}
	}
	b, _ := json.MarshalIndent(sealed, "", "  ")
	if err := writeFileAtomic(filepath.Join(dataDir, customSecretsFile), b); err != nil {
		return err
	}
	b, _ = json.MarshalIndent(customFile{Agents: c.list}, "", "  ")
	return writeFileAtomic(filepath.Join(dataDir, customAgentsFile), b)
}

func writeFileAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// customProvider is how an agent runs: as an ACP agent, with its variables.
func customProvider(a customAgent, secrets map[string]string) *acp.Provider {
	var env []string
	for _, e := range a.Env {
		v := e.Value
		if e.Secret {
			v = secrets[e.Name]
		}
		env = append(env, e.Name+"="+v)
	}
	return &acp.Provider{Agent: acp.Agent{Name: a.Name, Command: a.Command, Args: a.Args, Names: []string{a.Name}, Custom: true, Env: env, Dir: a.Dir}}
}

// provider is the coding agent by that name, built in or custom; nil when
// there is none.
func (n *Machine) provider(name string) provider.Provider {
	if p := n.cfg.Providers[name]; p != nil {
		return p
	}
	n.customMu.RLock()
	defer n.customMu.RUnlock()
	return n.custom[name]
}

// providers are all the coding agents, built in and custom.
func (n *Machine) providers() map[string]provider.Provider {
	n.customMu.RLock()
	defer n.customMu.RUnlock()
	ps := make(map[string]provider.Provider, len(n.cfg.Providers)+len(n.custom))
	for name, p := range n.custom {
		ps[name] = p
	}
	for name, p := range n.cfg.Providers {
		ps[name] = p
	}
	return ps
}

// customNames are the custom agents' names, in the order they were added.
func (n *Machine) customNames() []string {
	n.customMu.RLock()
	defer n.customMu.RUnlock()
	var names []string
	for _, a := range n.customs.list {
		names = append(names, a.Name)
	}
	return names
}

// customMeta returns the saved origin metadata for a custom agent (defaults to "local").
func (n *Machine) customMeta(name string) (source, registryID, registryVersion string) {
	n.customMu.RLock()
	defer n.customMu.RUnlock()
	for _, a := range n.customs.list {
		if a.Name == name {
			src := a.Source
			if src == "" {
				src = "local"
			}
			return src, a.RegistryID, a.RegistryVersion
		}
	}
	return "local", "", ""
}

// knownAgent: a coding agent of this machine has that name.
func (n *Machine) knownAgent(name string) bool {
	return slices.Contains(AgentNames(), name) || n.provider(name) != nil
}

// checkCustomAgent checks what the owner gave for an agent; with secrets,
// the values saved for one of that name, it fills in a secret left empty.
func (n *Machine) checkCustomAgent(def *machinev1.CustomAgent, saved map[string]string) (customAgent, map[string]string, error) {
	bad := func(format string, args ...any) (customAgent, map[string]string, error) {
		return customAgent{}, nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf(format, args...))
	}
	if def == nil {
		return bad("no agent given")
	}
	src := strings.TrimSpace(def.Source)
	if src == "" {
		src = "local"
	}
	dist := strings.TrimSpace(def.Distribution)
	if dist == "" {
		dist = src
	}
	a := customAgent{
		Name:            def.Name,
		Command:         strings.TrimSpace(def.Command),
		Args:            def.Args,
		Dir:             strings.TrimSpace(def.Dir),
		Source:          src,
		RegistryID:      strings.TrimSpace(def.RegistryId),
		RegistryVersion: strings.TrimSpace(def.RegistryVersion),
		Distribution:    dist,
	}
	switch {
	case !customNameRE.MatchString(a.Name):
		return bad("the name %q is not one Aide can use: lower case letters, digits and -, starting with a letter, at most 32", a.Name)
	case n.cfg.Providers[a.Name] != nil || slices.Contains(append(AgentNames(), "fake"), a.Name):
		return bad("%q is the name of an agent Aide has built in: choose another", a.Name)
	case a.Command == "":
		return bad("give the command that starts the agent")
	case strings.ContainsAny(a.Command, "\x00\n\r"):
		return bad("the command must be one line")
	case len(a.Args) > 64:
		return bad("at most 64 arguments")
	}
	for _, arg := range a.Args {
		if len(arg) > 4096 || strings.ContainsRune(arg, 0) {
			return bad("an argument is too long, or holds a NUL")
		}
	}
	if a.Dir != "" {
		if home, err := os.UserHomeDir(); err == nil && (a.Dir == "~" || strings.HasPrefix(a.Dir, "~/") || strings.HasPrefix(a.Dir, `~\`)) {
			a.Dir = filepath.Join(home, a.Dir[1:])
		}
		if st, err := os.Stat(a.Dir); err != nil || !st.IsDir() || !filepath.IsAbs(a.Dir) {
			return bad("the working folder %q is not a folder on this machine", def.Dir)
		}
	}
	secrets := map[string]string{}
	seen := map[string]bool{}
	for _, e := range def.Env {
		name := strings.TrimSpace(e.Name)
		switch {
		case !envNameRE.MatchString(name):
			return bad("%q is not a variable name: letters, digits and _, not starting with a digit", e.Name)
		case seen[strings.ToUpper(name)]:
			return bad("the variable %s is given twice", name)
		case strings.ContainsRune(e.Value, 0):
			return bad("the value of %s holds a NUL", name)
		}
		seen[strings.ToUpper(name)] = true
		if !e.Secret && !looksSecret(name) {
			a.Env = append(a.Env, customEnv{Name: name, Value: e.Value})
			continue
		}
		v := e.Value
		if v == "" {
			old, ok := saved[name]
			if !ok {
				return bad("give a value for %s", name)
			}
			v = old
		}
		secrets[name] = v
		a.Env = append(a.Env, customEnv{Name: name, Secret: true})
	}
	return a, secrets, nil
}

// SaveCustomAgent adds a custom agent, or changes the one by its name, and
// checks it at once.
func (n *Machine) SaveCustomAgent(def *machinev1.CustomAgent) error {
	n.customMu.Lock()
	a, secrets, err := n.checkCustomAgent(def, n.customs.secrets[def.GetName()])
	if err != nil {
		n.customMu.Unlock()
		return err
	}
	c := customAgents{list: slices.Clone(n.customs.list), secrets: map[string]map[string]string{}}
	for k, v := range n.customs.secrets {
		c.secrets[k] = v
	}
	if i := slices.IndexFunc(c.list, func(x customAgent) bool { return x.Name == a.Name }); i >= 0 {
		c.list[i] = a
	} else if len(c.list) >= maxCustomAgents {
		n.customMu.Unlock()
		return connect.NewError(connect.CodeResourceExhausted, fmt.Errorf("at most %d custom agents on a machine", maxCustomAgents))
	} else {
		c.list = append(c.list, a)
	}
	c.secrets[a.Name] = secrets
	if err := c.write(n.cfg.DataDir); err != nil {
		n.customMu.Unlock()
		return err
	}
	p := customProvider(a, secrets)
	n.customs = c
	if n.custom == nil {
		n.custom = map[string]provider.Provider{}
	}
	n.custom[a.Name] = p
	n.customMu.Unlock()
	n.audit.Log(AuditRecord{Kind: "agent", Subject: a.Name, Action: "save", Detail: a.Command})
	n.log.Info("custom agent saved", "agent", a.Name, "command", a.Command, "secrets", len(secrets), "secret_store", secretStore)
	n.recheck(a.Name, p)
	return nil
}

// recheck checks one agent again, in the background, as CheckAgents does.
func (n *Machine) recheck(name string, p provider.Provider) {
	n.mu.Lock()
	delete(n.infos, name)
	if n.pending == nil {
		n.pending = map[string]bool{}
	}
	n.pending[name] = true
	n.mu.Unlock()
	n.kickStatus()
	go func() {
		info := n.discover(name, p, false)
		n.mu.Lock()
		if n.provider(name) == p {
			n.infos[name] = n.withOffers(name, info)
		}
		delete(n.pending, name)
		n.mu.Unlock()
		n.kickStatus()
	}()
}

// RemoveCustomAgent takes a custom agent off this machine. A session of it
// that is running goes on; it can't be started again.
func (n *Machine) RemoveCustomAgent(name string) error {
	n.customMu.Lock()
	i := slices.IndexFunc(n.customs.list, func(x customAgent) bool { return x.Name == name })
	if i < 0 {
		n.customMu.Unlock()
		return connect.NewError(connect.CodeNotFound, fmt.Errorf("no custom agent %q on this machine", name))
	}
	removed := n.customs.list[i]
	c := customAgents{list: slices.Delete(slices.Clone(n.customs.list), i, i+1), secrets: map[string]map[string]string{}}
	for k, v := range n.customs.secrets {
		if k != name {
			c.secrets[k] = v
		}
	}
	if err := c.write(n.cfg.DataDir); err != nil {
		n.customMu.Unlock()
		return err
	}
	n.customs = c
	delete(n.custom, name)
	n.customMu.Unlock()
	if removed.Source == "registry" && removed.Distribution == "binary" {
		if err := n.acpRegistry().Uninstall(name); err != nil {
			n.log.Warn("cleaning up removed registry agent files", "agent", name, "err", err)
		}
	}
	n.mu.Lock()
	delete(n.infos, name)
	delete(n.pending, name)
	n.mu.Unlock()
	n.audit.Log(AuditRecord{Kind: "agent", Subject: name, Action: "remove"})
	n.log.Info("custom agent removed", "agent", name)
	n.kickStatus()
	return nil
}

// CustomAgents lists the custom agents, without their secrets' values.
func (n *Machine) CustomAgents() *machinev1.ListCustomAgentsResponse {
	n.customMu.RLock()
	defer n.customMu.RUnlock()
	res := &machinev1.ListCustomAgentsResponse{}
	for _, a := range n.customs.list {
		src := a.Source
		if src == "" {
			src = "local"
		}
		d := &machinev1.CustomAgent{
			Name:            a.Name,
			Command:         a.Command,
			Args:            a.Args,
			Dir:             a.Dir,
			Source:          src,
			RegistryId:      a.RegistryID,
			RegistryVersion: a.RegistryVersion,
			Distribution:    a.Distribution,
		}
		for _, e := range a.Env {
			d.Env = append(d.Env, &machinev1.AgentEnv{Name: e.Name, Value: e.Value, Secret: e.Secret})
		}
		res.Agents = append(res.Agents, d)
	}
	return res
}

// customTestTimeout bounds a test: an agent run through npx may first
// download itself.
const customTestTimeout = 90 * time.Second

// TestCustomAgent starts an agent as given once, in a scratch folder, and
// does the ACP handshake (initialize, then a new session); then stops it.
// Nothing is kept.
func (n *Machine) TestCustomAgent(ctx context.Context, def *machinev1.CustomAgent) (*machinev1.TestCustomAgentResponse, error) {
	n.customMu.RLock()
	a, secrets, err := n.checkCustomAgent(def, n.customs.secrets[def.GetName()])
	n.customMu.RUnlock()
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "aide-test-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	tmp := filepath.Join(dir, "tmp")
	os.Mkdir(tmp, 0o700)
	work := filepath.Join(dir, "work")
	os.Mkdir(work, 0o700)
	ctx, cancel := context.WithTimeout(ctx, customTestTimeout)
	defer cancel()
	start := time.Now()
	n.audit.Log(AuditRecord{Kind: "test", Subject: a.Name, Detail: a.Command})
	g, err := customProvider(a, secrets).Handshake(ctx, shellenv.Env(tmp), work)
	res := &machinev1.TestCustomAgentResponse{
		Ok:              err == nil && g.Session,
		AgentTitle:      g.Title,
		AgentVersion:    g.Version,
		AuthMethods:     g.Auth,
		Millis:          uint32(time.Since(start).Milliseconds()),
		ProtocolVersion: g.ProtocolVersion,
		Capabilities:    capabilitiesProto(g.Capabilities),
	}
	for _, m := range g.Models {
		res.Models = append(res.Models, n.enrichAgentModel(&machinev1.AgentModel{
			Id:       m.ID,
			Name:     m.Name,
			Efforts:  m.Efforts,
			Resolved: m.Resolved,
			Source:   m.Source,
		}))
	}
	if err != nil {
		res.Reason = err.Error()
	}
	return res, nil
}
