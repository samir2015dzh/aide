package machine

import (
	"context"
	"encoding/json"

	"github.com/samir2015dzh/aide/internal/provider"
)

// Passive checks (工单 #112): a coding agent's models are known only by
// starting it (Claude Code's initialize, `codex app-server`). Only a check
// the owner asks for on purpose does that, or one for an agent never asked
// before, or for a version of it never asked; what it found is kept
// (agent_models/), and every other check shows that, starting nothing.
const (
	kAgentModels = "agent_models/"
	// kAgentSignIn is what a session of an agent last showed of its sign-in
	// ("in" or "out"): an agent whose check cannot tell (an ACP agent's, 工单
	// #112) shows that rather than a guess.
	kAgentSignIn = "agent_signin/"
)

// keptModels is what the last check that asked an agent for its models
// found, for the version it then was.
type keptModels struct {
	Version   string
	Models    []provider.Model
	Own       provider.Settings
	AllModels bool
}

// discover checks the coding agent name: passively unless onPurpose, then
// with what was kept of its models; asking it (once) when nothing was kept
// for it, or for another version than it now is.
func (n *Machine) discover(name string, p provider.Provider, onPurpose bool) provider.Info {
	k, ok := n.keptFor(name)
	ctx := context.Background()
	if onPurpose || !ok {
		ctx = provider.OnPurpose(ctx)
	}
	// Reading plan usage here, a check that starts the agent asks it that
	// too (工单 #143), taken into the one reading (claudeusage.go).
	if n.usageOn() {
		ctx = provider.AskUsage(ctx)
	}
	info := p.Discover(ctx)
	if info.ModelsSkipped && info.Available && (!ok || k.Version != info.Version) {
		info = p.Discover(provider.OnPurpose(ctx)) // a version never asked: once
	}
	n.noteCheckedUsage(name, info)
	switch {
	case !info.Available:
	case info.ModelsSkipped:
		info.Models, info.Own.Model, info.Own.Effort, info.AllModels = k.Models, k.Own.Model, k.Own.Effort, k.AllModels
	default:
		n.keep(name, keptModels{Version: info.Version, Models: info.Models, Own: info.Own, AllModels: info.AllModels})
	}
	if info.Available && info.LoginUnconfirmed {
		if b, _ := n.st.Get(kAgentSignIn + name); b != nil {
			info.LoggedIn, info.LoginUnconfirmed = string(b) == "in", false
		}
	}
	return info
}

func (n *Machine) keptFor(name string) (keptModels, bool) {
	n.mu.Lock()
	k, ok := n.kept[name]
	n.mu.Unlock()
	if ok {
		return k, true
	}
	b, _ := n.st.Get(kAgentModels + name)
	if b == nil || json.Unmarshal(b, &k) != nil {
		return keptModels{}, false
	}
	n.mu.Lock()
	n.kept[name] = k
	n.mu.Unlock()
	return k, true
}

func (n *Machine) keep(name string, k keptModels) {
	n.mu.Lock()
	n.kept[name] = k
	n.mu.Unlock()
	if b, err := json.Marshal(k); err == nil {
		if err := n.st.Set(kAgentModels+name, b); err != nil {
			n.log.Warn("keeping an agent's models", "agent", name, "err", err)
		}
	}
}

// sawSignIn keeps what a session of the agent showed of its sign-in.
func (n *Machine) sawSignIn(name string, in bool) {
	v := "out"
	if in {
		v = "in"
	}
	if b, _ := n.st.Get(kAgentSignIn + name); string(b) == v {
		return
	}
	if err := n.st.Set(kAgentSignIn+name, []byte(v)); err != nil {
		n.log.Warn("keeping what a session showed of an agent's sign-in", "agent", name, "err", err)
	}
}
