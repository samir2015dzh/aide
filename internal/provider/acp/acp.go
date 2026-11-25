// Package acp drives coding agents that speak the Agent Client Protocol
// (JSON-RPC 2.0 over stdio, one message per line): Gemini CLI, Cursor,
// Copilot, OpenCode, Qwen Code, Goose and others (see Agents).
//
// The gate sees (and audits) every tool call, and answers the agent's
// permission requests.
package acp

import (
	"bytes"
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/provider"
	"github.com/samir2015dzh/aide/internal/shellenv"
)

// Agent describes how to start one ACP agent.
type Agent struct {
	Name    string   // provider name ("gemini")
	Command string   // binary looked up on PATH
	Args    []string // arguments that serve ACP on stdio
	// Paths are extra install locations; relative ones are under $HOME.
	Paths []string
	// Names begin the names its Windows installers list it by, for finding
	// Command where Windows says it is installed (provider.WindowsProgram),
	// beside a name that contains Command, which every agent has.
	Names []string
	// Apps are desktop apps whose own copy of the CLI is used when there is
	// no other.
	Apps []App
	// DesktopApps are the vendor's desktop apps that don't bring this CLI:
	// installed without it, the agent says the CLI is what is missing.
	DesktopApps []string
	// Auth lists `authenticate` method ids in order of preference. When
	// set, the first one the agent offers is sent before any session call.
	Auth []string
	// DSHPresets: the agent's modes are DeepSeek Harness's permission
	// presets, chosen as a session's process starts (dshpresets.go).
	DSHPresets bool
	// Custom: an agent the owner added on the machine. Its Command may be
	// a path (relative to Dir); it need not take --version, so Discover
	// does the ACP handshake instead.
	Custom bool
	// Env is added to its environment ("NAME=value"); Dir, when set, is
	// the folder its process starts in. The session's folder is the cwd
	// it is given over ACP either way.
	Env []string
	Dir string
}

// App is a desktop app (Electron, macOS or Windows) that keeps the agent's
// CLI as a script its executable runs as Node; paths are inside the app's
// folder, looked for in the user's Applications (a Mac's) or Programs
// (Windows), then the system's, then (Windows) wherever Windows says it
// is installed (Windows: its uninstall entry, App Paths, the program
// running now), with any of Windows.Exes as its executable.
type App struct {
	Bundle, Exec, Script string
	Windows              provider.WindowsApp
}

// windowsAppDirs is provider.WindowsAppDirs; tests stand in for Windows.
var windowsAppDirs = provider.WindowsAppDirs

type Provider struct {
	Agent
	// PathEnv is the PATH used to find binaries ("": the login shell's).
	PathEnv string
	// Binary overrides discovery (tests use a fake agent).
	Binary   string
	modelsMu sync.Mutex
	catalogs map[string]cachedCatalog
}

func (p *Provider) Name() string { return p.Agent.Name }

func (p *Provider) binary() string {
	if p.Binary != "" {
		return p.Binary
	}
	if p.Custom && strings.ContainsAny(p.Command, `/\`) {
		c := p.Command
		if !filepath.IsAbs(c) && p.Dir != "" {
			c = filepath.Join(p.Dir, c)
		}
		return provider.LookPath("", "", c)
	}
	home, _ := os.UserHomeDir()
	extra := []string{"/opt/homebrew/bin/" + p.Command, "/usr/local/bin/" + p.Command, filepath.Join(home, ".local/bin", p.Command)}
	for _, x := range p.Paths {
		if !filepath.IsAbs(x) {
			x = filepath.Join(home, x)
		}
		extra = append(extra, x)
	}
	if bin := provider.LookPath(p.Command, provider.SearchPath(p.PathEnv), extra...); bin != "" {
		return bin
	}
	return provider.WindowsProgram(p.Command, p.Names...)
}

// command is how to start the CLI: its binary, the arguments ahead of the
// agent's own and extra environment (bin is "" when there is none).
func (p *Provider) command() (bin string, pre, env []string) {
	if bin := p.binary(); bin != "" {
		return bin, nil, nil
	}
	home, _ := os.UserHomeDir()
	dirs := []string{filepath.Join(home, "Applications"), "/Applications"}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		dirs = append(dirs, filepath.Join(local, "Programs")) // the user's Windows installs
	}
	if pf := os.Getenv("ProgramFiles"); pf != "" {
		dirs = append(dirs, pf) // the machine's Windows ones
	}
	for _, a := range p.Apps {
		for _, dir := range dirs {
			app := filepath.Join(dir, a.Bundle)
			exe := filepath.Join(app, a.Exec)
			if script := appScript(app, a.Script); script != "" && provider.LookPath("", "", exe) != "" {
				return exe, []string{script}, []string{"ELECTRON_RUN_AS_NODE=1"}
			}
		}
	}
	for _, a := range p.Apps {
		if len(a.Windows.Exes) == 0 || p.Binary != "" {
			continue
		}
		for _, app := range windowsAppDirs(a.Windows) {
			script := appScript(app, a.Script)
			if script == "" {
				continue
			}
			for _, name := range a.Windows.Exes {
				if exe := provider.LookPath("", "", filepath.Join(app, name)); exe != "" {
					return exe, []string{script}, []string{"ELECTRON_RUN_AS_NODE=1"}
				}
			}
		}
	}
	return "", nil, nil
}

// windowsAppWithoutCLI names an app of the agent's that Windows says is
// installed, though its CLI is not found in it ("DeepSeek Harness (D:\DSH)"):
// the guide then says the app is there and what is missing, not that
// nothing was found. "" for none.
func (p *Provider) windowsAppWithoutCLI() string {
	if p.Binary != "" {
		return ""
	}
	for _, a := range p.Apps {
		if len(a.Windows.Exes) == 0 {
			continue
		}
		for _, dir := range windowsAppDirs(a.Windows) {
			for _, name := range a.Windows.Exes {
				if provider.LookPath("", "", filepath.Join(dir, name)) != "" {
					return a.Bundle + " (" + dir + ")"
				}
			}
		}
	}
	return ""
}

// appScript is where the app in folder app keeps its CLI's script, script
// being where one of its builds keeps it: the same path inside the app's
// code however the build packs it (resources/app.asar, its .unpacked
// folder, or resources/app unpacked: seen 2026-11-20, DeepSeek Harness in
// a folder of the owner's choosing); else, for a script in node_modules,
// the same package path in a folder at most four down the app's resources.
// "" when none is there.
func appScript(app, script string) string {
	for _, s := range packings(script) {
		if p := filepath.Join(app, s); scriptThere(p) {
			return p
		}
	}
	slash := filepath.ToSlash(script)
	i := strings.LastIndex(slash, "node_modules/")
	if i < 0 {
		return ""
	}
	tail := filepath.FromSlash(slash[i:])
	resources := filepath.Join(app, filepath.FromSlash(slash[:strings.Index(slash+"/", "/")]))
	level := []string{resources}
	for depth := 0; depth <= 4 && len(level) > 0; depth++ {
		var next []string
		for _, d := range level {
			if p := filepath.Join(d, tail); fileThere(p) {
				return p
			}
			entries, _ := os.ReadDir(d)
			for _, e := range entries {
				if e.IsDir() && e.Name() != "node_modules" {
					next = append(next, filepath.Join(d, e.Name()))
				}
			}
		}
		level = next
	}
	return ""
}

// packings are script as each way of packing an Electron app's code puts
// it: inside app.asar, in app.asar.unpacked, or in app; script itself when
// it names none.
func packings(script string) []string {
	parts := strings.Split(filepath.ToSlash(script), "/")
	for i, part := range parts {
		switch part {
		case "app.asar", "app.asar.unpacked", "app":
			var out []string
			for _, as := range []string{part, "app.asar", "app.asar.unpacked", "app"} {
				q := slices.Clone(parts)
				q[i] = as
				if p := filepath.FromSlash(strings.Join(q, "/")); !slices.Contains(out, p) {
					out = append(out, p)
				}
			}
			return out
		}
	}
	return []string{script}
}

func fileThere(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// scriptThere: the script is on disk, or inside an .asar archive, which
// only the app's own Node (ELECTRON_RUN_AS_NODE) reads; running it then
// says when the path inside is wrong.
func scriptThere(script string) bool {
	if _, err := os.Stat(script); err == nil {
		return true
	}
	for dir := filepath.Dir(script); ; dir = filepath.Dir(dir) {
		if strings.HasSuffix(filepath.Base(dir), ".asar") {
			st, err := os.Stat(dir)
			return err == nil && !st.IsDir()
		}
		if dir == filepath.Dir(dir) {
			return false
		}
	}
}

var versionRE = regexp.MustCompile(`\d+\.\d+\.\d+[-\w.]*`)

func (p *Provider) Discover(ctx context.Context) provider.Info {
	info := provider.Info{Name: p.Name()}
	bin, pre, env := p.command()
	if !provider.Exists(bin) {
		info.Problem, info.Detail = provider.ProblemNotFound, p.Command+" not found"
		if app := provider.AppWithoutCLI(p.DesktopApps...); app != "" && p.Binary == "" {
			info.Problem, info.Detail = provider.ProblemAppWithoutCLI, app
		} else if dir := p.windowsAppWithoutCLI(); dir != "" {
			info.Problem, info.Detail = provider.ProblemAppWithoutCLI, dir
		}
		return info
	}
	info.Binary = bin
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	env = append(provider.ProbeEnv(p.PathEnv), env...)
	if p.Custom {
		return p.discoverByHandshake(ctx, info)
	}
	out, problem, detail := provider.Probe(ctx, env, bin, append(pre, "--version")...)
	if problem != provider.ProblemNone {
		info.Problem, info.Detail = problem, detail
		return info
	}
	info.Available = true
	info.Version = versionRE.FindString(string(out))
	// ACP has no way to ask for the login state before a session: not
	// confirmed (工单 #112, #108's rule for every agent), never a guess. A
	// session that runs a turn says it is signed in, one that fails "not
	// logged in" that it is not (the machine keeps what they showed).
	info.LoginUnconfirmed = true
	if p.Name() == "antigravity" {
		// Its settings name how it signs in: that, read, confirms it.
		if antigravityConfigured(env) {
			info.LoggedIn, info.LoginUnconfirmed = true, false
		}
		p.discoverModels(ctx, bin, pre, env)
	}
	if p.DSHPresets { // known before any session
		if ps, _ := readDSHPresets(ctx, env, append([]string{bin}, append(pre, p.Args...)...)); ps != nil {
			info.Modes, info.Own.Mode = ps.modes, ps.own
		}
	}
	return info
}

// withPath sets PATH so node-based CLIs find `node` (version probes run
// outside a session environment).
func withPath(env []string, path string) []string {
	if path == "" {
		return env
	}
	return append(shellenv.Without(env, "PATH"), "PATH="+path)
}

func (p *Provider) Start(ctx context.Context, spec provider.StartSpec) (provider.Session, error) {
	bin, pre, env := p.command()
	if bin == "" {
		return nil, errors.New(p.Command + " not found")
	}
	if env != nil || p.Env != nil {
		if spec.Env == nil {
			spec.Env = os.Environ()
		}
		spec.Env = append(append(slices.Clip(spec.Env), env...), p.Env...)
	}
	args := append(slices.Clip(pre), p.Args...)
	// DSH's presets: a new session's is chosen as its process starts.
	var presets *dshPresets
	if p.DSHPresets && spec.ResumeRef == "" {
		var err error
		if presets, err = readDSHPresets(ctx, spec.Env, append([]string{bin}, args...)); err != nil && spec.Mode != "" {
			return nil, provider.Fail(sessionv1.FailureKind_FAILURE_KIND_START_FAILED, fmt.Sprintf("%s: mode %q: %v", p.Name(), spec.Mode, err))
		}
		if presets != nil && spec.Mode != "" {
			more, file, err := dshPresetArgs(presets, spec.Mode, spec.TempDir)
			if err != nil {
				return nil, provider.Fail(sessionv1.FailureKind_FAILURE_KIND_INVALID_REQUEST, fmt.Sprintf("%s: mode %q: %v", p.Name(), spec.Mode, err))
			}
			defer os.Remove(file) // read as the process starts, before it answers
			args = append(args, more...)
			spec.Mode = "" // not set over ACP
		}
	}
	proc, err := provider.StartProc(ctx, bin, args, cmp.Or(p.Dir, spec.WorkDir), spec.Env)
	if err != nil {
		return nil, err
	}
	s := &session{name: p.Name(), proc: proc, events: make(chan provider.Event, 256), pending: map[int64]func(rpcResult){},
		gate: spec.Gate, ctx: ctx, known: map[string]*toolCall{}, calls: map[string]provider.Decision{}, finished: map[string]bool{}}
	if p.Name() == "antigravity" {
		key := modelContext(bin, spec.Env)
		scope := spec.ModelScope
		if scope == "" {
			scope = "workspace:" + modelHash(spec.WorkDir)
		}
		s.catalog = func(data json.RawMessage, initial bool) {
			p.observeModels(key, scope, "acp_session", data, initial)
		}
	}
	go s.read()
	fail := func(err error) (provider.Session, error) {
		proc.Stop(time.Second)
		if p.Name() == "antigravity" && provider.FailureOf(err, sessionv1.FailureKind_FAILURE_KIND_UNKNOWN).Kind == sessionv1.FailureKind_FAILURE_KIND_AUTH {
			err = provider.Fail(sessionv1.FailureKind_FAILURE_KIND_AUTH, err.Error()+"; run `aide agent login antigravity` on this machine to sign in")
		}
		return nil, fmt.Errorf("%s: %w", p.Name(), err)
	}
	// No limit of its own: node-based agents can take a while to boot and
	// load their config, longer on a busy machine. ctx ends it (an
	// interrupt, or the session's start limit).
	res, err := s.call(ctx, "initialize", initializeParams)
	if err != nil {
		return fail(fmt.Errorf("initialize: %w", classified(err, sessionv1.FailureKind_FAILURE_KIND_START_FAILED)))
	}
	var init struct {
		AgentCapabilities struct {
			LoadSession        bool `json:"loadSession"`
			PromptCapabilities struct {
				Image bool `json:"image"`
				Audio bool `json:"audio"`
			} `json:"promptCapabilities"`
			SessionCapabilities struct {
				Resume json.RawMessage `json:"resume"`
				Close  json.RawMessage `json:"close"`
			} `json:"sessionCapabilities"`
		} `json:"agentCapabilities"`
		AuthMethods []struct {
			ID string `json:"id"`
		} `json:"authMethods"`
	}
	json.Unmarshal(res, &init)
	s.images = init.AgentCapabilities.PromptCapabilities.Image
	s.audio = init.AgentCapabilities.PromptCapabilities.Audio
	s.closes = len(init.AgentCapabilities.SessionCapabilities.Close) > 0 && string(init.AgentCapabilities.SessionCapabilities.Close) != "null"
	if m := authMethod(p.Auth, init.AuthMethods); m != "" {
		if _, err := s.call(ctx, "authenticate", map[string]any{"methodId": m}); err != nil {
			return fail(provider.Fail(sessionv1.FailureKind_FAILURE_KIND_AUTH, fmt.Sprintf("authenticate (%s): %v; run `%s` once as this user to log in", m, err, p.Command)))
		}
	}
	// The session/new, /resume or /load result (it says the session's
	// configuration).
	res = nil
	servers := []any{}
	for _, m := range spec.MCPServers {
		env := []any{}
		for k, v := range m.Env {
			env = append(env, map[string]any{"name": k, "value": v})
		}
		servers = append(servers, map[string]any{"name": m.Name, "command": m.Command, "args": m.Args, "env": env})
	}
	params := map[string]any{"cwd": spec.WorkDir, "mcpServers": servers}
	caps := init.AgentCapabilities
	switch {
	case spec.ResumeRef == "":
		res, err = s.call(ctx, "session/new", params)
		if err != nil {
			return fail(fmt.Errorf("session/new: %w", classified(err, sessionv1.FailureKind_FAILURE_KIND_START_FAILED)))
		}
		var r struct {
			SessionID string `json:"sessionId"`
		}
		json.Unmarshal(res, &r)
		if r.SessionID == "" {
			return fail(errors.New("session/new returned no session id"))
		}
		s.sessionID = r.SessionID
	case len(caps.SessionCapabilities.Resume) > 0 && string(caps.SessionCapabilities.Resume) != "null":
		params["sessionId"] = spec.ResumeRef
		if res, err = s.call(ctx, "session/resume", params); err != nil {
			return fail(fmt.Errorf("session/resume: %w", resumeError(err)))
		}
		s.sessionID = spec.ResumeRef
	case caps.LoadSession:
		// session/load replays the conversation as session/update
		// notifications before it answers; they are not new events.
		params["sessionId"] = spec.ResumeRef
		s.replaying.Store(true)
		res, err = s.call(ctx, "session/load", params)
		s.replaying.Store(false)
		if err != nil {
			return fail(fmt.Errorf("session/load: %w", resumeError(err)))
		}
		s.sessionID = spec.ResumeRef
	default:
		return fail(provider.Fail(sessionv1.FailureKind_FAILURE_KIND_RESUME_REJECTED, "the agent cannot resume sessions"))
	}
	if p.Name() == "antigravity" {
		// A saved auto-edit/YOLO mode must not bypass Aide's gate, including
		// when resuming a session whose mode was changed in another client.
		if _, err := s.call(ctx, "session/set_mode", map[string]any{"sessionId": s.sessionID, "modeId": "default"}); err != nil {
			return fail(fmt.Errorf("set permission mode: %w", err))
		}
	}
	var cfg sessionConfig
	json.Unmarshal(res, &cfg)
	var offers *provider.Offers
	if spec.ResumeRef == "" {
		offers = cfg.offers()
		if presets != nil {
			offers.Modes, offers.Own.Mode = presets.modes, presets.own
		}
	} else if p.DSHPresets && cfg.option("mode") == nil && cfg.Modes == nil {
		spec.Mode = "" // a resumed DSH session keeps its own preset
	}
	if err := s.configure(ctx, &cfg, spec); err != nil {
		return fail(err)
	}
	if offers != nil {
		cfg.addEfforts(offers) // those of the model chosen, too
	}
	s.emit(provider.Event{Kind: provider.EventRef, Ref: s.sessionID, Model: cfg.model(), Effort: cfg.effort(), Offers: offers})
	if err := s.Send(ctx, provider.Input{Text: spec.Prompt, Images: spec.Images, Files: spec.Files}); err != nil {
		return fail(err)
	}
	return s, nil
}

// initializeParams are what Aide says of itself in initialize.
var initializeParams = map[string]any{"protocolVersion": 1,
	"clientCapabilities": map[string]any{"fs": map[string]any{"readTextFile": false, "writeTextFile": false}, "terminal": false,
		// Questions as forms (elicitation/create); agents without it
		// turn their ask-the-user tools off.
		"elicitation": map[string]any{"form": map[string]any{}}},
	"clientInfo": map[string]any{"name": "aide", "version": "0.1.0"}}

// sessionConfig is what an ACP session says of its configuration
// (session/new, /resume, /load and session/set_config_option results): its
// config options and, from agents that predate them, its modes and models.
type sessionConfig struct {
	ConfigOptions []configOption `json:"configOptions"`
	Modes         *struct {
		CurrentModeID  string `json:"currentModeId"`
		AvailableModes []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"availableModes"`
	} `json:"modes"`
	Models *struct {
		CurrentModelID  string `json:"currentModelId"`
		AvailableModels []struct {
			ModelID string `json:"modelId"`
			Name    string `json:"name"`
		} `json:"availableModels"`
	} `json:"models"`
}

type configOption struct {
	ID           string          `json:"id"`
	Category     string          `json:"category"`
	CurrentValue json.RawMessage `json:"currentValue"`
	Options      []choice        `json:"options"`
}

type choice struct {
	Value   string   `json:"value"`
	Name    string   `json:"name"`
	GroupID string   `json:"group"`
	Options []choice `json:"options"` // a group of choices; Name (or GroupID) is the group's
	Group   string   `json:"-"`       // the group it is in (choices)
}

// option is the session's config option of a category ("model",
// "thought_level", "mode"), or with that id; nil when it has none.
func (c *sessionConfig) option(category string) *configOption {
	for i, o := range c.ConfigOptions {
		if o.Category == category || o.ID == category {
			return &c.ConfigOptions[i]
		}
	}
	return nil
}

// modelOptions are the session's config options of the model category: an
// agent may offer its models in more than one (工单 #98 补充: all of them
// are its models, not only the first's).
func (c *sessionConfig) modelOptions() []*configOption {
	var out []*configOption
	for i, o := range c.ConfigOptions {
		if o.Category == "model" || o.ID == "model" || o.ID == "llm" || o.ID == "custom_llm" {
			out = append(out, &c.ConfigOptions[i])
		}
	}
	return out
}

// choices are the option's values, each with the group it is in.
func (o *configOption) choices() []choice {
	var out []choice
	var walk func(cs []choice, group string)
	walk = func(cs []choice, group string) {
		for _, c := range cs {
			if len(c.Options) > 0 {
				walk(c.Options, firstNonEmpty(c.Name, c.GroupID))
				continue
			}
			out = append(out, choice{Value: c.Value, Name: firstNonEmpty(c.Name, c.Value), Group: group})
		}
	}
	walk(o.Options, "")
	return out
}

// label is a choice as a list names it: "name (group)".
func (x choice) label() string {
	if x.Group == "" {
		return x.Name
	}
	return x.Name + " (" + x.Group + ")"
}

func (o *configOption) current() string {
	var v string
	json.Unmarshal(o.CurrentValue, &v)
	return v
}

// model is the model the session runs ("" when it does not say).
func (c *sessionConfig) model() string {
	if ms := c.modelOptions(); len(ms) > 0 {
		for _, o := range ms {
			if v := o.current(); v != "" {
				return v
			}
		}
		return ""
	}
	if c.Models != nil {
		return c.Models.CurrentModelID
	}
	return ""
}

// effort is the session's reasoning effort ("" when it does not say).
func (c *sessionConfig) effort() string {
	if o := c.option("thought_level"); o != nil {
		return o.current()
	}
	return ""
}

// offers is what the session offers to choose from, and what it runs with
// unless told.
func (c *sessionConfig) offers() *provider.Offers {
	o := &provider.Offers{Own: provider.Settings{Model: c.model(), Effort: c.effort()}}
	if ms := c.modelOptions(); len(ms) > 0 {
		var all []choice
		for _, m := range ms {
			all = append(all, m.choices()...)
		}
		o.Models = sameModelsOnce(all, o.Own.Model)
	} else if c.Models != nil {
		for _, x := range c.Models.AvailableModels {
			o.Models = append(o.Models, provider.Model{ID: x.ModelID, Name: x.Name})
		}
	}
	if m := c.option("mode"); m != nil {
		o.Own.Mode = m.current()
		for _, x := range m.choices() {
			o.Modes = append(o.Modes, provider.Mode{ID: x.Value, Name: x.label()})
		}
	} else if c.Modes != nil {
		o.Own.Mode = c.Modes.CurrentModeID
		for _, x := range c.Modes.AvailableModes {
			o.Modes = append(o.Modes, provider.Mode{ID: x.ID, Name: x.Name})
		}
	}
	c.addEfforts(o)
	return o
}

// addEfforts gives the model the session runs its reasoning efforts: an
// agent offers those of the current model only. An empty value (the
// agent's own default) is no effort to name.
func (c *sessionConfig) addEfforts(o *provider.Offers) {
	t := c.option("thought_level")
	if t == nil {
		return
	}
	var efforts []string
	for _, x := range t.choices() {
		if x.Value != "" {
			efforts = append(efforts, x.Value)
		}
	}
	for i := range o.Models {
		if o.Models[i].ID == c.model() {
			o.Models[i].Efforts = efforts
		}
	}
}

// configure selects the session's model, reasoning effort and mode, each
// through its config option when the session offers one, else (model and
// mode) through session/set_model or session/set_mode. One the agent does
// not offer fails the start: running on another silently is worse.
func (s *session) configure(ctx context.Context, c *sessionConfig, spec provider.StartSpec) error {
	for _, x := range []struct {
		category, name, value, method, param string
		kind                                 sessionv1.FailureKind
	}{
		{"model", "model", spec.Model, "session/set_model", "modelId", sessionv1.FailureKind_FAILURE_KIND_MODEL_UNAVAILABLE},
		{"thought_level", "reasoning effort", spec.Effort, "", "", sessionv1.FailureKind_FAILURE_KIND_INVALID_REQUEST},
		{"mode", "mode", spec.Mode, "session/set_mode", "modeId", sessionv1.FailureKind_FAILURE_KIND_INVALID_REQUEST},
	} {
		if x.value == "" {
			continue
		}
		if err := s.set(ctx, c, x.category, x.value, x.method, x.param); err != nil {
			return provider.Fail(x.kind, fmt.Sprintf("%s %q: %v", x.name, x.value, err))
		}
	}
	return nil
}

// set sets one of the session's settings; c then says what it runs with.
func (s *session) set(ctx context.Context, c *sessionConfig, category, value, method, param string) error {
	o := c.option(category)
	var values []string
	if category == "model" { // the option that offers it, of all its model options
		for _, m := range c.modelOptions() {
			for _, x := range m.choices() {
				if x.Value == value {
					o = m
				}
			}
		}
	}
	if o == nil {
		if method == "" {
			return errors.New("the agent offers no choice of it")
		}
		if _, err := s.call(ctx, method, map[string]any{"sessionId": s.sessionID, param: value}); err != nil {
			return err
		}
		if category == "model" && c.Models != nil {
			c.Models.CurrentModelID = value
		} else if category == "mode" && c.Modes != nil {
			c.Modes.CurrentModeID = value
		}
		return nil
	}
	options := []*configOption{o}
	if category == "model" {
		options = c.modelOptions()
	}
	for _, m := range options {
		for _, x := range m.choices() {
			if x.Value != "" { // the agent's own default
				values = append(values, x.Value)
			}
		}
	}
	if !contains(values, value) {
		return fmt.Errorf("not offered by the agent (available: %s)", strings.Join(values, ", "))
	}
	res, err := s.call(ctx, "session/set_config_option", map[string]any{"sessionId": s.sessionID, "configId": o.ID, "value": value})
	if err != nil {
		return err
	}
	// The result is every option as it now is (one may depend on another).
	var now sessionConfig
	if json.Unmarshal(res, &now) == nil && len(now.ConfigOptions) > 0 {
		c.ConfigOptions = now.ConfigOptions
	} else {
		o.CurrentValue, _ = json.Marshal(value)
	}
	return nil
}

func authMethod(want []string, offered []struct {
	ID string `json:"id"`
}) string {
	for _, w := range want {
		for _, o := range offered {
			if o.ID == w {
				return w
			}
		}
	}
	return ""
}

// rpcError is a JSON-RPC error from the agent.
type rpcError struct {
	Code    int
	Message string
}

func (e *rpcError) Error() string { return fmt.Sprintf("%s (code %d)", e.Message, e.Code) }

// ACP error codes (agentclientprotocol.com/protocol/schema#errorcode).
const (
	codeAuthRequired     = -32000
	codeResourceNotFound = -32002
)

// classified gives an agent error its failure kind: auth_required by code,
// otherwise by text, else fallback.
func classified(err error, fallback sessionv1.FailureKind) error {
	var re *rpcError
	if errors.As(err, &re) && re.Code == codeAuthRequired {
		return provider.Fail(sessionv1.FailureKind_FAILURE_KIND_AUTH, err.Error())
	}
	f := provider.FailureOf(err, fallback)
	return &provider.FailureError{Failure: *f}
}

// resumeError classifies a session/load or session/resume error: only a
// not-found code or wording counts as a refused resume (a timeout, a
// login or an unreachable MCP server does not).
func resumeError(err error) error {
	var re *rpcError
	if errors.As(err, &re) && (re.Code == codeResourceNotFound || provider.IsResumeRejected(re.Message)) {
		return provider.Fail(sessionv1.FailureKind_FAILURE_KIND_RESUME_REJECTED, err.Error())
	}
	return classified(err, sessionv1.FailureKind_FAILURE_KIND_START_FAILED)
}

type rpcResult struct {
	result json.RawMessage
	err    error
}

type session struct {
	name      string
	proc      *provider.Proc
	events    chan provider.Event
	gate      provider.Gate
	ctx       context.Context
	sessionID string
	catalog   func(json.RawMessage, bool)
	// images, audio: the agent takes image or audio content blocks
	// (promptCapabilities); closes, session/close (sessionCapabilities.close).
	images    bool
	audio     bool
	closes    bool
	replaying atomic.Bool
	// closeMu orders emits from Start against the reader closing events.
	closeMu sync.RWMutex
	closed  bool

	mu      sync.Mutex
	nextID  int64
	pending map[int64]func(rpcResult)
	// The current turn: streamed message text not yet emitted, and the
	// last complete message (the turn's result).
	msg  strings.Builder
	last string
	// Tool calls seen this session: what is known so far (agents announce
	// a call before filling in its input), the gate's decision, and
	// whether the result was reported.
	known    map[string]*toolCall
	calls    map[string]provider.Decision
	finished map[string]bool
	// forms are elicitation requests waiting for an answer.
	forms map[string]form
}

// form is a pending elicitation: its request id and fields in order.
type form struct {
	reqID  json.RawMessage
	fields []formField
}

type formField struct {
	name, typ string
	multi     bool
	values    map[string]any // option label -> value
}

// elicit turns a form elicitation into a question (URL mode is not
// offered, so not taken).
func (s *session) elicit(id json.RawMessage, params json.RawMessage) error {
	var p struct {
		Mode            string `json:"mode"`
		Message         string `json:"message"`
		RequestedSchema struct {
			Properties json.RawMessage `json:"properties"`
			Required   []string        `json:"required"`
		} `json:"requestedSchema"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return err
	}
	if p.Mode != "" && p.Mode != "form" {
		return fmt.Errorf("elicitation mode %q is not supported", p.Mode)
	}
	names, props, err := orderedObject(p.RequestedSchema.Properties)
	if err != nil || len(names) == 0 {
		return errors.New("the form has no fields")
	}
	q := &provider.Question{ID: "form:" + string(id)}
	f := form{reqID: id}
	for i, name := range names {
		var sc struct {
			Type        string `json:"type"`
			Title       string `json:"title"`
			Description string `json:"description"`
			Enum        []any  `json:"enum"`
			OneOf       []struct {
				Const any    `json:"const"`
				Title string `json:"title"`
			} `json:"oneOf"`
			Items struct {
				Enum  []any `json:"enum"`
				AnyOf []struct {
					Const any    `json:"const"`
					Title string `json:"title"`
				} `json:"anyOf"`
			} `json:"items"`
		}
		json.Unmarshal(props[name], &sc)
		it := provider.QuestionItem{ID: name, Header: firstNonEmpty(sc.Title, name), Text: firstNonEmpty(sc.Description, sc.Title, name)}
		if i == 0 && p.Message != "" {
			it.Text = strings.TrimSpace(p.Message + "\n" + sc.Description)
		}
		ff := formField{name: name, typ: sc.Type, values: map[string]any{}}
		add := func(label string, v any) {
			if label == "" {
				label = fmt.Sprint(v)
			}
			it.Options = append(it.Options, provider.Option{Label: label})
			ff.values[label] = v
		}
		switch {
		case sc.Type == "array":
			ff.multi, it.MultiSelect = true, true
			for _, e := range sc.Items.Enum {
				add("", e)
			}
			for _, o := range sc.Items.AnyOf {
				add(o.Title, o.Const)
			}
		case sc.Type == "boolean":
			add("Yes", true)
			add("No", false)
		default:
			for _, e := range sc.Enum {
				add("", e)
			}
			for _, o := range sc.OneOf {
				add(o.Title, o.Const)
			}
		}
		it.AllowOther = len(it.Options) == 0
		it.Optional = !slices.Contains(p.RequestedSchema.Required, name)
		q.Items = append(q.Items, it)
		f.fields = append(f.fields, ff)
	}
	s.mu.Lock()
	if s.forms == nil {
		s.forms = map[string]form{}
	}
	s.forms[q.ID] = f
	s.mu.Unlock()
	s.emit(provider.Event{Kind: provider.EventQuestion, Question: q})
	return nil
}

// Answer fills in the form (accept), or declines it.
func (s *session) Answer(ctx context.Context, id string, r provider.Reply) error {
	s.mu.Lock()
	f, ok := s.forms[id]
	delete(s.forms, id)
	s.mu.Unlock()
	if !ok {
		return errors.New("no such question")
	}
	result := map[string]any{"action": "decline"}
	if !r.Declined {
		content := map[string]any{}
		for _, a := range r.Answers {
			for _, ff := range f.fields {
				if ff.name == a.ItemID {
					content[ff.name] = ff.value(a)
				}
			}
		}
		result = map[string]any{"action": "accept", "content": content}
	}
	return s.write(map[string]any{"jsonrpc": "2.0", "id": f.reqID, "result": result})
}

// value is an answer in the field's schema type.
func (ff formField) value(a provider.Answer) any {
	if ff.multi {
		out := []any{}
		for _, l := range a.Selected {
			out = append(out, ff.values[l])
		}
		return out
	}
	if len(a.Selected) > 0 {
		return ff.values[a.Selected[0]]
	}
	t := strings.TrimSpace(a.Text)
	switch ff.typ {
	case "integer":
		if n, err := strconv.ParseInt(t, 10, 64); err == nil {
			return n
		}
	case "number":
		if n, err := strconv.ParseFloat(t, 64); err == nil {
			return n
		}
	}
	return t
}

// orderedObject decodes a JSON object keeping its keys' order.
func orderedObject(raw json.RawMessage) ([]string, map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, nil, errors.New("not an object")
	}
	var names []string
	vals := map[string]json.RawMessage{}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		k, _ := t.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, nil, err
		}
		names = append(names, k)
		vals[k] = v
	}
	return names, vals, nil
}

func (s *session) Events() <-chan provider.Event { return s.events }
func (s *session) Done() <-chan struct{}         { return s.proc.Done() }
func (s *session) Err() error                    { return s.proc.Err() }
func (s *session) Pgid() int                     { return s.proc.Pgid() }
func (s *session) LastActivity() time.Time       { return s.proc.LastOutput() }
func (s *session) Diagnose() provider.Diagnosis  { return s.proc.Diagnose() }
func (s *session) Close() error {
	if s.closes && s.sessionID != "" {
		// session/close cancels the turn in progress and has the agent keep
		// what it keeps of the session (DSH flushes it to disk) before its
		// process goes; an agent that does not answer in time is stopped.
		ctx, cancel := context.WithTimeout(context.Background(), closeWait)
		s.call(ctx, "session/close", map[string]any{"sessionId": s.sessionID})
		cancel()
	}
	s.proc.Stop(3 * time.Second)
	return nil
}

// closeWait bounds how long Close waits for session/close.
const closeWait = 10 * time.Second

func (s *session) emit(e provider.Event) {
	s.closeMu.RLock()
	defer s.closeMu.RUnlock()
	if s.closed {
		return
	}
	select {
	case s.events <- e:
	case <-s.proc.Done():
	}
}

func (s *session) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.proc.WriteLine(b)
}

// request sends a request; done runs on the reader goroutine with the
// response.
func (s *session) request(method string, params any, done func(rpcResult)) error {
	s.mu.Lock()
	s.nextID++
	id := s.nextID
	s.pending[id] = done
	s.mu.Unlock()
	if err := s.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
		return err
	}
	return nil
}

func (s *session) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	ch := make(chan rpcResult, 1)
	if err := s.request(method, params, func(r rpcResult) {
		// Observe responses on the reader, before the next notification. The
		// caller goroutine must not overwrite a newer config_option_update.
		if r.err == nil && s.catalog != nil {
			switch method {
			case "session/new", "session/load", "session/resume", "session/set_config_option":
				s.catalog(r.result, method == "session/new")
			}
		}
		ch <- r
	}); err != nil {
		return nil, err
	}
	select {
	case r := <-ch:
		return r.result, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.proc.Done():
		return nil, fmt.Errorf("exited: %v", s.proc.Err())
	}
}

func (s *session) Send(ctx context.Context, in provider.Input) error {
	s.mu.Lock()
	s.msg.Reset()
	s.last = ""
	s.mu.Unlock()
	return s.request("session/prompt", map[string]any{"sessionId": s.sessionID, "prompt": s.prompt(in)}, s.turnDone)
}

// maxInlineAudio bounds an audio file sent inline (base64, as an image is).
const maxInlineAudio = 3584 << 10

// prompt is a message as ACP content blocks: its text, then each attached
// file. Images go as image blocks and audio as audio blocks to an agent
// that takes them; every other file goes as a resource_link to the saved
// file (every ACP agent takes those), which the text lists by path too.
func (s *session) prompt(in provider.Input) []any {
	text := in.Text
	if !s.images && len(in.Images) > 0 {
		text = strings.ReplaceAll(text, provider.ShownAsImage, "") // it is not shown them
	}
	prompt := []any{map[string]any{"type": "text", "text": text}}
	files := in.Files
	for _, img := range in.Images {
		if !s.images {
			files = append(files, provider.File{Path: img.Path, Name: filepath.Base(img.Path), MediaType: img.MediaType})
			continue
		}
		if b, err := os.ReadFile(img.Path); err == nil {
			prompt = append(prompt, map[string]any{"type": "image", "mimeType": img.MediaType, "data": base64.StdEncoding.EncodeToString(b)})
		}
	}
	for _, f := range files {
		if s.audio && strings.HasPrefix(f.MediaType, "audio/") && f.Size <= maxInlineAudio {
			if b, err := os.ReadFile(f.Path); err == nil {
				prompt = append(prompt, map[string]any{"type": "audio", "mimeType": f.MediaType, "data": base64.StdEncoding.EncodeToString(b)})
				continue
			}
		}
		link := map[string]any{"type": "resource_link", "uri": fileURI(f.Path), "name": f.Name}
		if f.MediaType != "" {
			link["mimeType"] = f.MediaType
		}
		if f.Size > 0 {
			link["size"] = f.Size
		}
		prompt = append(prompt, link)
	}
	return prompt
}

// fileURI is a file:// URI for an absolute path (C:\x is file:///C:/x).
func fileURI(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

// Steer is not part of ACP: a session takes one prompt per turn.
func (s *session) Steer(context.Context, provider.Input) error {
	return errors.New(s.name + " (ACP) takes input only between turns")
}

func (s *session) Interrupt(ctx context.Context) error {
	return s.write(map[string]any{"jsonrpc": "2.0", "method": "session/cancel", "params": map[string]any{"sessionId": s.sessionID}})
}

// turnDone handles the session/prompt response, which ends the turn.
func (s *session) turnDone(r rpcResult) {
	s.flush()
	s.mu.Lock()
	last := s.last
	s.mu.Unlock()
	e := provider.Event{Kind: provider.EventTurnCompleted, Text: last}
	if r.err != nil {
		e.IsError, e.Text = true, r.err.Error()
		e.Failure = provider.FailureOf(classified(r.err, sessionv1.FailureKind_FAILURE_KIND_UNKNOWN), sessionv1.FailureKind_FAILURE_KIND_UNKNOWN)
		s.emit(e)
		return
	}
	var p struct {
		StopReason string `json:"stopReason"`
		Usage      struct {
			InputTokens       uint64 `json:"inputTokens"`
			OutputTokens      uint64 `json:"outputTokens"`
			CachedReadTokens  uint64 `json:"cachedReadTokens"`
			CachedWriteTokens uint64 `json:"cachedWriteTokens"`
		} `json:"usage"`
	}
	json.Unmarshal(r.result, &p)
	e.Usage = provider.Usage{InputTokens: p.Usage.InputTokens + p.Usage.CachedReadTokens + p.Usage.CachedWriteTokens, OutputTokens: p.Usage.OutputTokens}
	switch p.StopReason {
	case "end_turn", "":
	case "cancelled":
		e.IsError, e.Text = true, "interrupted"
	default: // max_tokens, max_turn_requests, refusal
		e.IsError = true
		e.Text = strings.TrimSpace("stopped (" + p.StopReason + ") " + last)
		kind := sessionv1.FailureKind_FAILURE_KIND_LIMIT_REACHED
		if p.StopReason == "refusal" {
			kind = sessionv1.FailureKind_FAILURE_KIND_REFUSED
		}
		e.Failure = &provider.Failure{Kind: kind, Message: e.Text}
	}
	s.emit(e)
}

// flush emits the streamed message text as one agent message.
func (s *session) flush() {
	s.mu.Lock()
	text := s.msg.String()
	s.msg.Reset()
	if strings.TrimSpace(text) != "" {
		s.last = text
	}
	s.mu.Unlock()
	if strings.TrimSpace(text) != "" {
		s.emit(provider.Event{Kind: provider.EventAgentMessage, Text: text})
	}
}

type message struct {
	ID     *json.RawMessage `json:"id"`
	Method string           `json:"method"`
	Params json.RawMessage  `json:"params"`
	Result json.RawMessage  `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (s *session) read() {
	defer func() {
		s.closeMu.Lock()
		s.closed = true
		close(s.events)
		s.closeMu.Unlock()
	}()
	s.proc.Lines(func(b []byte) {
		var m message
		if json.Unmarshal(b, &m) != nil {
			return
		}
		switch {
		case m.ID != nil && m.Method == "": // response
			var id int64
			json.Unmarshal(*m.ID, &id)
			s.mu.Lock()
			done := s.pending[id]
			delete(s.pending, id)
			s.mu.Unlock()
			if done != nil {
				r := rpcResult{result: m.Result}
				if m.Error != nil {
					r.err = &rpcError{Code: m.Error.Code, Message: m.Error.Message}
				}
				done(r)
			}
		case m.ID != nil:
			s.handleRequest(*m.ID, m.Method, m.Params)
		case m.Method == "session/update":
			if !s.replaying.Load() {
				s.handleUpdate(m.Params)
			}
		}
	})
}

// toolCall is an ACP ToolCall (or the fields of a ToolCallUpdate).
type toolCall struct {
	ToolCallID string `json:"toolCallId"`
	Title      string `json:"title"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
	Locations  []struct {
		Path string `json:"path"`
	} `json:"locations"`
	RawInput  json.RawMessage `json:"rawInput"`
	RawOutput json.RawMessage `json:"rawOutput"`
	Content   []struct {
		Type    string `json:"type"`
		Path    string `json:"path"`
		Content struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"content"`
}

func (s *session) handleUpdate(params json.RawMessage) {
	var p struct {
		Update struct {
			SessionUpdate string          `json:"sessionUpdate"`
			Content       json.RawMessage `json:"content"`
			ConfigOptions json.RawMessage `json:"configOptions"`
			toolCall
		} `json:"update"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	u := p.Update
	switch u.SessionUpdate {
	case "config_option_update":
		if s.catalog != nil {
			s.catalog(paramsForOptions(u.ConfigOptions), false)
		}
	case "agent_message_chunk":
		var c struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(u.Content, &c) == nil && c.Type == "text" {
			s.mu.Lock()
			s.msg.WriteString(c.Text)
			s.mu.Unlock()
			s.emit(provider.Event{Kind: provider.EventAgentDraft, Text: c.Text})
		}
	case "tool_call", "tool_call_update":
		// ToolCall.content is an array; agent_message_chunk's is a block.
		json.Unmarshal(u.Content, &u.toolCall.Content)
		s.flush()
		tc := s.merge(u.toolCall)
		// Decide once the call has its input, or at the latest when it runs.
		done := u.Status == "completed" || u.Status == "failed"
		if hasInput(tc.RawInput) || len(tc.Locations) > 0 || u.Status == "in_progress" || done {
			s.decide(tc)
		}
		if done {
			s.result(u.toolCall)
		}
	}
}

// merge folds an update into what is known about its tool call.
func (s *session) merge(u toolCall) toolCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	tc := s.known[u.ToolCallID]
	if tc == nil {
		tc = &toolCall{ToolCallID: u.ToolCallID}
		s.known[u.ToolCallID] = tc
	}
	if u.Title != "" {
		tc.Title = u.Title
	}
	if u.Kind != "" {
		tc.Kind = u.Kind
	}
	if u.Status != "" {
		tc.Status = u.Status
	}
	if len(u.Locations) > 0 {
		tc.Locations = u.Locations
	}
	if hasInput(u.RawInput) {
		tc.RawInput = u.RawInput
	}
	if len(u.Content) > 0 {
		tc.Content = u.Content
	}
	return *tc
}

// hasInput reports whether raw carries a tool input (agents announce calls
// with an empty one).
func hasInput(raw json.RawMessage) bool {
	var m map[string]any
	if json.Unmarshal(raw, &m) == nil {
		return len(m) > 0
	}
	return len(raw) > 0 && string(raw) != "null"
}

// decide runs the gate once per tool call and reports the call.
func (s *session) decide(tc toolCall) provider.Decision {
	s.mu.Lock()
	d, seen := s.calls[tc.ToolCallID]
	s.mu.Unlock()
	if seen {
		return d
	}
	a := Classify(tc)
	d = provider.Decision{Allow: true}
	if s.gate != nil {
		d = s.gate(s.ctx, a)
	}
	s.mu.Lock()
	s.calls[tc.ToolCallID] = d
	s.mu.Unlock()
	s.emit(provider.Event{Kind: provider.EventToolCall, CallID: a.CallID, ToolKind: a.Kind, ToolName: a.Name, Summary: a.Summary, Decision: &d})
	return d
}

func (s *session) result(tc toolCall) {
	s.mu.Lock()
	if s.finished[tc.ToolCallID] {
		s.mu.Unlock()
		return
	}
	s.finished[tc.ToolCallID] = true
	s.mu.Unlock()
	var b strings.Builder
	for _, c := range tc.Content {
		if c.Type == "content" && c.Content.Type == "text" {
			b.WriteString(c.Content.Text)
		} else if c.Type == "diff" {
			b.WriteString("edited " + c.Path + "\n")
		}
	}
	out := b.String()
	if out == "" && len(tc.RawOutput) > 0 && string(tc.RawOutput) != "null" {
		out = rawText(tc.RawOutput)
	}
	if out == "" {
		out = tc.Status
	}
	sum, trunc := provider.KeepOutput(out)
	s.emit(provider.Event{Kind: provider.EventToolResult, CallID: tc.ToolCallID, Summary: sum, IsError: tc.Status == "failed", Truncated: trunc})
}

func rawText(raw json.RawMessage) string {
	var str string
	if json.Unmarshal(raw, &str) == nil {
		return str
	}
	var o struct {
		Output         string `json:"output"`
		Stdout         string `json:"stdout"`
		CombinedOutput string `json:"combinedOutput"` // Antigravity terminal result
	}
	if json.Unmarshal(raw, &o) == nil {
		if o.Output+o.Stdout != "" {
			return o.Output + o.Stdout
		}
		if o.CombinedOutput != "" {
			return o.CombinedOutput
		}
	}
	return string(raw)
}

func (s *session) handleRequest(id json.RawMessage, method string, params json.RawMessage) {
	reply := map[string]any{"jsonrpc": "2.0", "id": id}
	switch method {
	case "session/request_permission":
		var p struct {
			ToolCall toolCall `json:"toolCall"`
			Options  []struct {
				OptionID string `json:"optionId"`
				Kind     string `json:"kind"`
			} `json:"options"`
		}
		json.Unmarshal(params, &p)
		d := s.decide(s.merge(p.ToolCall))
		// Never pick allow_always/reject_always: the agent would remember
		// the choice beyond this session.
		want := "reject_once"
		if d.Allow {
			want = "allow_once"
		}
		for _, o := range p.Options {
			if o.Kind == want {
				reply["result"] = map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": o.OptionID}}
				break
			}
		}
		if reply["result"] == nil {
			reply["error"] = map[string]any{"code": -32603, "message": "Aide: no " + want + " option offered"}
		}
	case "elicitation/create", "session/create_elicitation":
		if err := s.elicit(id, params); err != nil {
			reply["error"] = map[string]any{"code": -32602, "message": "Aide: " + err.Error()}
			break
		}
		return // answered by Answer
	default:
		// fs/*, terminal/* (not advertised) and vendor extensions.
		reply["error"] = map[string]any{"code": -32601, "message": "method not supported by Aide: " + method}
	}
	s.write(reply)
}

// Classify maps an ACP tool call to a normalized action.
func Classify(tc toolCall) provider.ToolAction {
	a := provider.ToolAction{CallID: tc.ToolCallID, Name: tc.Kind, Summary: tc.Title, Raw: tc.RawInput}
	if a.Name == "" {
		a.Name = "other"
	}
	for _, l := range tc.Locations {
		a.Paths = append(a.Paths, l.Path)
	}
	for _, c := range tc.Content {
		if c.Type == "diff" && c.Path != "" && !contains(a.Paths, c.Path) {
			a.Paths = append(a.Paths, c.Path)
		}
	}
	switch tc.Kind {
	case "read":
		a.Kind = provider.ToolRead
	case "edit", "delete", "move":
		a.Kind = provider.ToolEdit
	case "search":
		a.Kind = provider.ToolSearch
	case "execute":
		a.Kind = provider.ToolShell
		a.Command = command(tc.RawInput)
		if a.Command == "" {
			a.Command = tc.Title
		}
	case "fetch":
		a.Kind = provider.ToolNetwork
	default:
		a.Kind = provider.ToolOther
	}
	if a.Summary == "" {
		a.Summary = strings.Join(a.Paths, ", ")
	}
	a.Summary, _ = provider.Truncate(a.Summary, 500)
	return a
}

func command(raw json.RawMessage) string {
	var in struct {
		Command     json.RawMessage `json:"command"`
		CommandLine string          `json:"CommandLine"`  // Antigravity permission request
		ShellLine   string          `json:"command_line"` // Antigravity tool update
	}
	if json.Unmarshal(raw, &in) != nil {
		return ""
	}
	if len(in.Command) == 0 {
		return firstNonEmpty(in.CommandLine, in.ShellLine)
	}
	var str string
	if json.Unmarshal(in.Command, &str) == nil {
		return str
	}
	var parts []string
	if json.Unmarshal(in.Command, &parts) == nil {
		return strings.Join(parts, " ")
	}
	return ""
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
