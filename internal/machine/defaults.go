package machine

import (
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"

	"connectrpc.com/connect"

	identityv1 "github.com/hermeshai/hermesh/gen/go/hermesh/identity/v1"
	machinev1 "github.com/hermeshai/hermesh/gen/go/hermesh/machine/v1"
	"github.com/hermeshai/hermesh/internal/provider"
)

// A coding agent's defaults on this machine are what its sessions run with
// when the client starting one names no model: a setting from the owner's
// devices (AdminCommand.set_agent_defaults). They are passed per session;
// the agent's own configuration is never changed.
//
// An ACP agent tells what it offers (models, their reasoning efforts,
// modes) and runs with only in a session: the last new one on this machine
// says it for the next ones, and for picking defaults (agent_offers/).

const (
	kAgentDefaults = "agent_defaults/"
	kAgentOffers   = "agent_offers/"
)

var (
	// ACP agents' model ids can be JSON (DSH's ["provider","model"]).
	modelRE  = regexp.MustCompile(`^[A-Za-z0-9._:/\[\]",-]{0,200}$`)
	effortRE = regexp.MustCompile(`^[a-z0-9_-]{0,32}$`)
	modeRE   = regexp.MustCompile(`^[A-Za-z0-9._:/-]{0,64}$`)
)

func (n *Machine) loadDefaults() {
	for a := range n.providers() {
		d := &identityv1.AgentDefaults{}
		if ok, _ := n.st.GetProto(kAgentDefaults+a, d); ok {
			n.defaults[a] = d
		}
		if b, _ := n.st.Get(kAgentOffers + a); b != nil {
			var o provider.Offers
			if json.Unmarshal(b, &o) == nil {
				n.offers[a] = o
			}
		}
	}
}

// offered keeps what a new session of an ACP agent offered. Its agent tells
// the reasoning efforts of the model a session runs only: those it told of
// the others before are kept.
func (n *Machine) offered(agent string, o provider.Offers) {
	if p, ok := n.provider(agent).(provider.ScopedModels); ok && p.ScopedModels() {
		return // its sessions' choices stay theirs (its model catalogs)
	}
	n.mu.Lock()
	for i, m := range o.Models {
		if k := slices.IndexFunc(n.offers[agent].Models, func(x provider.Model) bool { return x.ID == m.ID }); m.Efforts == nil && k >= 0 {
			o.Models[i].Efforts = n.offers[agent].Models[k].Efforts
		}
	}
	n.offers[agent] = o
	if i, ok := n.infos[agent]; ok { // its models and modes are what it offered before
		i.Models, i.Modes, i.Own = nil, nil, provider.Settings{}
		n.infos[agent] = n.withOffers(agent, i)
	}
	n.mu.Unlock()
	if b, err := json.Marshal(o); err == nil {
		if err := n.st.Set(kAgentOffers+agent, b); err != nil {
			n.log.Warn("storing what an agent offers", "agent", agent, "err", err)
		}
	}
	n.kickStatus()
}

// withOffers is i with what the agent's sessions offered, for an agent that
// tells it only there (DSH tells its modes without a session, not its
// models). n.mu is held.
func (n *Machine) withOffers(agent string, i provider.Info) provider.Info {
	o, ok := n.offers[agent]
	if !ok {
		return i
	}
	if len(i.Models) == 0 {
		i.Models, i.Own.Model, i.Own.Effort, i.SomeEfforts = o.Models, o.Own.Model, o.Own.Effort, len(o.Models) > 0
	}
	if len(i.Modes) == 0 {
		i.Modes, i.Own.Mode = o.Modes, o.Own.Mode
	}
	return i
}

func (n *Machine) setAgentDefaults(d *identityv1.AgentDefaults) error {
	if _, ok := identityv1.AgentSpeed_name[int32(d.Speed)]; !ok {
		return errors.New("unknown speed")
	}
	n.mu.Lock()
	known, modes := n.provider(d.Agent) != nil, n.infos[d.Agent].Modes
	n.mu.Unlock()
	switch {
	case !known:
		return errors.New("no such coding agent")
	case !modelRE.MatchString(d.Model):
		return errors.New("not a model name")
	case !effortRE.MatchString(d.Effort):
		return errors.New("not a reasoning effort")
	case d.Speed != identityv1.AgentSpeed_AGENT_SPEED_UNSPECIFIED && d.Agent != "codex":
		return errors.New("only Codex has a speed")
	case !modeRE.MatchString(d.Mode):
		return errors.New("not a mode")
	case d.Mode != "" && !slices.ContainsFunc(modes, func(m provider.Mode) bool { return m.ID == d.Mode }):
		return errors.New("the agent offers no such mode here")
	}
	d = &identityv1.AgentDefaults{Agent: d.Agent, Model: d.Model, Effort: d.Effort, Speed: d.Speed, Mode: d.Mode}
	if err := n.st.SetProto(kAgentDefaults+d.Agent, d); err != nil {
		return err
	}
	n.mu.Lock()
	n.defaults[d.Agent] = d
	n.mu.Unlock()
	return nil
}

// agentDefaults is what agent's sessions run with when the client names no
// model.
func (n *Machine) agentDefaults(agent string) provider.Settings {
	n.mu.Lock()
	defer n.mu.Unlock()
	d := n.defaults[agent]
	return provider.Settings{Model: d.GetModel(), Effort: d.GetEffort(), Speed: d.GetSpeed(), Mode: d.GetMode()}
}

// checkMode refuses a session mode the agent does not offer here (an ACP
// agent's, as its last new session here offered them), naming the ones it
// does.
func checkMode(agent string, i provider.Info, mode string) error {
	if mode == "" || slices.ContainsFunc(i.Modes, func(m provider.Mode) bool { return m.ID == mode }) {
		return nil
	}
	if len(i.Modes) == 0 {
		return cerr(connect.CodeInvalidArgument, "%s on this machine offers no session modes (an ACP agent's show once a session of it has started here)", agent)
	}
	var ids []string
	for _, m := range i.Modes {
		ids = append(ids, m.ID)
	}
	return cerr(connect.CodeInvalidArgument, "%s on this machine has no mode %q (one of: %s)", agent, mode, strings.Join(ids, ", "))
}

// checkSettings refuses a model or reasoning effort the agent does not
// offer here, naming the ones it does. Only an agent that lists every model
// it takes (Codex) has its model checked; an effort must be one the model
// the session runs offers (runs: "" or unlisted, one any listed model does),
// unless that is not known yet (SomeEfforts).
func checkSettings(agent string, i provider.Info, model, effort, runs string) error {
	if model != "" && i.AllModels && !slices.ContainsFunc(i.Models, func(m provider.Model) bool { return m.ID == model }) {
		var ids []string
		for _, m := range i.Models {
			ids = append(ids, m.ID)
		}
		return cerr(connect.CodeInvalidArgument, "%s on this machine has no model %q (one of: %s)", agent, model, strings.Join(ids, ", "))
	}
	if effort == "" {
		return nil
	}
	var efforts []string
	if k := slices.IndexFunc(i.Models, func(m provider.Model) bool { return m.ID == runs }); k >= 0 {
		efforts = i.Models[k].Efforts
	} else {
		for _, m := range i.Models {
			for _, e := range m.Efforts {
				if !slices.Contains(efforts, e) {
					efforts = append(efforts, e)
				}
			}
		}
	}
	switch {
	case len(efforts) == 0 && i.SomeEfforts: // not known yet: the agent checks it
		return nil
	case len(efforts) == 0:
		return cerr(connect.CodeInvalidArgument, "%s on this machine takes no reasoning effort", agent)
	case !slices.Contains(efforts, effort) && runs != "":
		return cerr(connect.CodeInvalidArgument, "%s's model %s has no reasoning effort %q (one of: %s)", agent, runs, effort, strings.Join(efforts, ", "))
	case !slices.Contains(efforts, effort):
		return cerr(connect.CodeInvalidArgument, "%s has no reasoning effort %q (one of: %s)", agent, effort, strings.Join(efforts, ", "))
	}
	return nil
}

// agentModels fills in what a's status says of models: the ones it offers,
// this machine's defaults and the agent's own configuration. n.mu is held.
func (n *Machine) agentModels(a *machinev1.AgentStatus, i provider.Info) {
	for _, m := range i.Models {
		a.Models = append(a.Models, &machinev1.AgentModel{Id: m.ID, Name: m.Name, Efforts: m.Efforts})
	}
	a.Defaults = n.defaults[a.Name]
	for _, m := range i.Modes {
		a.Modes = append(a.Modes, &machinev1.AgentMode{Id: m.ID, Name: m.Name})
	}
	if i.Own != (provider.Settings{}) {
		a.Own = &identityv1.AgentDefaults{Agent: a.Name, Model: i.Own.Model, Effort: i.Own.Effort, Speed: i.Own.Speed, Mode: i.Own.Mode}
	}
	if p, ok := n.provider(a.Name).(provider.ModelCatalogProvider); ok {
		for _, c := range p.ModelCatalogs() {
			v := &machinev1.AgentModelCatalog{Scope: c.Scope, Source: c.Source, State: c.State,
				DefaultModel: c.DefaultModel, CurrentModel: c.CurrentModel}
			if !c.ObservedAt.IsZero() {
				v.ObservedAtUnixMs, v.ExpiresAtUnixMs = c.ObservedAt.UnixMilli(), c.ExpiresAt.UnixMilli()
			}
			for _, m := range c.Models {
				v.Models = append(v.Models, &machinev1.AgentModel{Id: m.ID, Name: m.Name, Efforts: m.Efforts})
			}
			a.ModelCatalogs = append(a.ModelCatalogs, v)
		}
	}
}
