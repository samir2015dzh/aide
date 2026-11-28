package machine

import (
	"context"
	"encoding/json"
	"time"

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
	// Kept as a SignInSeen (JSON); the bare "in"/"out" earlier versions kept
	// says no more than that (工单 #180: an "out" with no when or why no
	// longer decides, see signInKept).
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
		if seen, ok := n.signInKept(name); ok {
			info.LoggedIn, info.LoginUnconfirmed, info.SignInSeen = seen.In, false, &seen
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

// signInKept is what the last run of the agent here showed of its sign-in.
// A bare "out" kept by an earlier version is no evidence (工单 #180): it
// says neither when nor why, and kept the agent signed out — no test run
// offered — long after whatever showed it, so it is left out (the next run
// says again); a bare "in" stands, as the next failed run corrects it.
func (n *Machine) signInKept(name string) (provider.SignInSeen, bool) {
	b, _ := n.st.Get(kAgentSignIn + name)
	switch string(b) {
	case "":
		return provider.SignInSeen{}, false
	case "in":
		return provider.SignInSeen{In: true}, true
	case "out":
		return provider.SignInSeen{}, false
	}
	var seen provider.SignInSeen
	if json.Unmarshal(b, &seen) != nil {
		return provider.SignInSeen{}, false
	}
	return seen, true
}

// sawSignIn keeps what a session of the agent showed of its sign-in, when,
// and what it said failing "not signed in" (why).
func (n *Machine) sawSignIn(name string, in bool, why string) provider.SignInSeen {
	if old, ok := n.signInKept(name); ok && in && old.In && !old.At.IsZero() {
		return old // signed in since then: no write per turn
	}
	seen := provider.SignInSeen{In: in, At: time.Now().UTC().Truncate(time.Second), Why: why}
	b, _ := json.Marshal(seen)
	if err := n.st.Set(kAgentSignIn+name, b); err != nil {
		n.log.Warn("keeping what a session showed of an agent's sign-in", "agent", name, "err", err)
	}
	return seen
}
