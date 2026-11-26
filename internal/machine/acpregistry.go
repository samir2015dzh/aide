package machine

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"runtime"
	"slices"
	"strings"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	"github.com/samir2015dzh/aide/internal/acpregistry"
	"github.com/samir2015dzh/aide/internal/provider"
	"github.com/samir2015dzh/aide/internal/provider/acp"
)

func (n *Machine) acpRegistry() *acpregistry.Registry {
	n.acpRegMu.Lock()
	defer n.acpRegMu.Unlock()
	if n.acpReg == nil {
		n.acpReg = acpregistry.New(acpregistry.Options{
			DataDir: n.cfg.DataDir,
		})
	}
	return n.acpReg
}

// SetACPRegistry overrides the ACP registry client (used by tests to point at a local server).
func (n *Machine) SetACPRegistry(reg *acpregistry.Registry) {
	n.acpRegMu.Lock()
	n.acpReg = reg
	n.acpRegMu.Unlock()
}

// ListACPRegistry returns the official ACP Registry catalog annotated with this
// machine's platform support and installation status (工单 #110).
func (n *Machine) ListACPRegistry(ctx context.Context, refresh bool) *machinev1.ListACPRegistryResponse {
	res := n.acpRegistry().List(ctx, refresh)

	// Index installed custom agents by registry_id or name.
	installedByRegID := map[string]string{}
	n.customMu.RLock()
	for _, a := range n.customs.list {
		if a.RegistryID != "" {
			installedByRegID[a.RegistryID] = a.Name
		} else {
			installedByRegID[a.Name] = a.Name
		}
	}
	n.customMu.RUnlock()

	// Check which built-in providers are currently detected as available on this machine.
	builtinAvail := map[string]bool{}
	n.mu.Lock()
	for name, info := range n.infos {
		if info.Available {
			builtinAvail[name] = true
		}
	}
	n.mu.Unlock()

	out := &machinev1.ListACPRegistryResponse{
		Source:    res.Source,
		LastError: res.LastError,
		Platform:  res.Platform,
	}
	if !res.SyncedAt.IsZero() {
		out.SyncedAt = timestamppb.New(res.SyncedAt)
	}
	for _, e := range res.Entries {
		// sha256Verified: a binary is installed only when the Registry
		// declares its SHA-256, and checked against it.
		dist, sha256Verified, reason := acpregistry.BestDistribution(e, res.Platform)
		installedAs := installedByRegID[e.ID]
		if dist != "" {
			reason = "" // why its binary is not used, when a runner is
		}
		builtin := cmp.Or(acpregistry.DedicatedBuiltin(e), e.ID)
		if installedAs == "" && builtinAvail[builtin] {
			installedAs = builtin
		}
		if b := acpregistry.Builtin(e.ID); b != "" && builtinAvail[b] {
			installedAs = b // the built-in agent's own program (工单 #133)
		}
		out.Entries = append(out.Entries, &machinev1.ACPRegistryEntry{
			Id:                e.ID,
			Name:              e.Name,
			Version:           e.Version,
			Description:       e.Description,
			Repository:        e.Repository,
			License:           e.License,
			Authors:           e.Authors,
			Distribution:      dist,
			Sha256Verified:    sha256Verified,
			Supported:         dist != "",
			UnsupportedReason: reason,
			Installed:         installedAs != "",
			InstalledAs:       installedAs,
		})
	}
	return out
}

// InstallACPRegistryAgent downloads (with SHA-256 verification for binary archives),
// installs into a machine-isolated directory, and registers an official ACP Registry
// agent as a custom ACP agent on this machine (工单 #110).
func (n *Machine) InstallACPRegistryAgent(ctx context.Context, req *machinev1.InstallACPRegistryAgentRequest) (*machinev1.InstallACPRegistryAgentResponse, error) {
	if req == nil || strings.TrimSpace(req.RegistryId) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("registry_id is required"))
	}
	if name := acpregistry.Builtin(req.RegistryId); name != "" {
		return n.installBuiltinProgram(ctx, req.RegistryId, name)
	}
	reserved := func(name string) bool {
		if n.cfg.Providers[name] != nil || slices.Contains(append(AgentNames(), "fake"), name) {
			return true
		}
		n.customMu.RLock()
		defer n.customMu.RUnlock()
		return slices.ContainsFunc(n.customs.list, func(a customAgent) bool {
			return a.Name == name && a.RegistryID != req.RegistryId
		})
	}
	inst, err := n.acpRegistry().Install(ctx, req.RegistryId, req.Name, reserved)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	var env []*machinev1.AgentEnv
	for k, v := range inst.Env {
		env = append(env, &machinev1.AgentEnv{Name: k, Value: v, Secret: looksSecret(k)})
	}
	def := &machinev1.CustomAgent{
		Name:            inst.Name,
		Command:         inst.Command,
		Args:            inst.Args,
		Env:             env,
		Dir:             inst.Dir,
		Source:          "registry",
		RegistryId:      inst.RegistryID,
		RegistryVersion: inst.RegistryVersion,
		Distribution:    inst.Distribution,
	}
	if err := n.SaveCustomAgent(def); err != nil {
		if inst.InstallDir != "" {
			_ = n.acpRegistry().Uninstall(inst.Name)
		}
		return nil, err
	}
	n.audit.Log(AuditRecord{
		Kind:    "agent",
		Subject: inst.Name,
		Action:  "install_registry",
		Detail:  fmt.Sprintf("%s@%s (%s)", inst.RegistryID, inst.RegistryVersion, inst.Distribution),
	})
	return &machinev1.InstallACPRegistryAgentResponse{
		Agent:      def,
		InstallDir: inst.InstallDir,
		Sha256:     inst.SHA256,
	}, nil
}

// installBuiltinProgram installs the program a built-in agent runs —
// Google's Antigravity ACP server, which neither the Antigravity desktop app
// nor agy brings (工单 #133) — from its official registry entry, checked,
// where the agent finds it, and checks the agent again: done only once the
// agent is found there, not when the files are.
func (n *Machine) installBuiltinProgram(ctx context.Context, registryID, name string) (*machinev1.InstallACPRegistryAgentResponse, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	inst, err := n.acpRegistry().InstallTo(ctx, registryID, acp.AntigravityDir(home, runtime.GOOS))
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	// A check under way began before the files were there: it ends first, then
	// the agent is checked again unless what it found already has it.
	<-n.CheckAgents(true, 0)
	info := n.providerInfo(name)
	if !info.Available {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("%s is installed in %s, but it does not start: %s", inst.RegistryID, inst.InstallDir, cmp.Or(info.Detail, "not found")))
	}
	n.audit.Log(AuditRecord{Kind: "agent", Subject: name, Action: "install_program",
		Detail: fmt.Sprintf("%s@%s (binary, sha256 %s) in %s", inst.RegistryID, inst.RegistryVersion, inst.SHA256, inst.InstallDir)})
	return &machinev1.InstallACPRegistryAgentResponse{
		Agent: &machinev1.CustomAgent{Name: name, Command: inst.Command, Args: inst.Args, Source: "builtin",
			RegistryId: inst.RegistryID, RegistryVersion: inst.RegistryVersion, Distribution: inst.Distribution},
		InstallDir: inst.InstallDir,
		Sha256:     inst.SHA256,
	}, nil
}

// capabilitiesProto converts provider.Capabilities to protobuf AgentCapabilities.
func capabilitiesProto(c provider.Capabilities) *machinev1.AgentCapabilities {
	return &machinev1.AgentCapabilities{
		SelectModel:          c.SelectModel,
		SwitchModelInSession: c.SwitchModelInSession,
		SelectEffort:         c.SelectEffort,
		SelectMode:           c.SelectMode,
		LoginInAide:          c.LoginInAide,
		Usage:                c.Usage,
		Fork:                 c.Fork,
		Rollback:             c.Rollback,
		Steer:                c.Steer,
		Resume:               c.Resume,
		Images:               c.Images,
		Audio:                c.Audio,
	}
}

// truthfulCapabilities computes the truthful capability declaration and negotiated
// protocol version for provider name (工单 #110 / #112).
func (n *Machine) truthfulCapabilities(name string, p provider.Provider, info provider.Info) (*machinev1.AgentCapabilities, uint32) {
	var caps provider.Capabilities
	protoVer := info.ProtocolVersion

	switch {
	case info.Capabilities != nil:
		caps = *info.Capabilities
	case p != nil:
		if cp, ok := p.(provider.CapabilitiesProvider); ok {
			if obs, ver := cp.ObservedCapabilities(); obs != nil {
				caps = *obs
				if protoVer == 0 {
					protoVer = ver
				}
				break
			}
		}
		caps = defaultCapabilitiesFor(name, p)
	default:
		caps = defaultCapabilitiesFor(name, nil)
	}

	// An ACP agent's efforts and modes are known once a session (or its
	// presets) showed them: what it offers here, not a fixed claim.
	if _, isACP := p.(*acp.Provider); isACP || !slices.Contains([]string{"claude", "codex", "fake"}, name) {
		caps.SelectMode = caps.SelectMode || len(info.Modes) > 0
		caps.SelectEffort = caps.SelectEffort || slices.ContainsFunc(info.Models, func(m provider.Model) bool { return len(m.Efforts) > 0 })
	}
	// Keep rollback capability strictly aligned with CanRollbackConversation (#113).
	if rc, ok := p.(interface{ CanRollbackConversation() (bool, string) }); ok {
		canRB, _ := rc.CanRollbackConversation()
		caps.Rollback = canRB
	}
	return capabilitiesProto(caps), protoVer
}

func defaultCapabilitiesFor(name string, p provider.Provider) provider.Capabilities {
	switch name {
	case "claude":
		return provider.Capabilities{
			SelectModel:          true,
			SwitchModelInSession: true,
			SelectEffort:         true,
			SelectMode:           false,
			LoginInAide:          true,
			Usage:                true,
			Fork:                 false,
			Rollback:             true,
			Steer:                true,
			Resume:               true,
			Images:               true,
			Audio:                false,
		}
	case "codex":
		return provider.Capabilities{
			SelectModel:          true,
			SwitchModelInSession: true,
			SelectEffort:         true,
			SelectMode:           false,
			LoginInAide:          false,
			Usage:                false,
			Fork:                 false,
			Rollback:             true,
			Steer:                true,
			Resume:               true,
			Images:               true,
			Audio:                false,
		}
	case "fake":
		return provider.Capabilities{
			SelectModel:          true,
			SwitchModelInSession: true,
			SelectEffort:         true,
			SelectMode:           false,
			LoginInAide:          false,
			Usage:                false,
			Fork:                 false,
			Rollback:             true,
			Steer:                true,
			Resume:               true,
			Images:               true,
			Audio:                false,
		}
	default:
		// Generic ACP (and built-in ACP providers before live initialize
		// observation): efforts and modes as the agent shows them
		// (truthfulCapabilities), no fork/steer/in-aide-login/usage; rollback
		// as CanRollbackConversation says (Aide's worktree rollback).
		return provider.Capabilities{
			SelectModel:          true,
			SwitchModelInSession: true,
			SelectEffort:         false,
			SelectMode:           false,
			LoginInAide:          false,
			Usage:                false,
			Fork:                 false,
			Rollback:             p != nil,
			Steer:                false,
			Resume:               true,
			Images:               false,
			Audio:                false,
		}
	}
}
