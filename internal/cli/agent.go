package cli

import (
	"cmp"
	"context"
	"fmt"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	"github.com/samir2015dzh/aide/internal/peer"
	"github.com/samir2015dzh/aide/internal/provider/acp"
)

// machineAgent is a coding agent this client may run on a machine, with
// the models it offers there.
type machineAgent struct {
	Agent     string       `json:"agent"`
	Machine   string       `json:"machine"`
	MachineID string       `json:"machine_id"`
	Version   string       `json:"version"`
	Models    []agentModel `json:"models"`
	// Modes are an ACP agent's session modes (permission presets).
	Modes []agentMode `json:"modes,omitempty"`
	// What a session runs with when the client names none.
	DefaultModel  string              `json:"default_model"`
	DefaultEffort string              `json:"default_effort"`
	DefaultMode   string              `json:"default_mode,omitempty"`
	ModelCatalogs []agentModelCatalog `json:"model_catalogs,omitempty"`
	// Usage is the Claude plan usage read on that machine (Claude Code
	// only), for the scheduler to pace work by.
	Usage *agentUsage `json:"usage,omitempty"`
}

// agentUsage is the machine-readable Claude plan usage: each window's fill
// and reset, when it was read, whether it is a stale fallback, and why it
// is missing. No token is ever here.
type agentUsage struct {
	Windows     []usageWindow `json:"windows"`
	FetchedAt   int64         `json:"fetched_at_unix_ms,omitempty"`
	Stale       bool          `json:"stale,omitempty"`
	Unavailable string        `json:"unavailable,omitempty"`
	Renew       string        `json:"renew,omitempty"`
	// Detail: what each source said on the way (工单 #104), the endpoint's
	// part its HTTP status and a summary of the answer; never a token.
	Detail string `json:"detail,omitempty"`
	// Source: where the reading came from, "claude_code_cache",
	// "claude_code" or "api" (工单 #86, #104).
	Source string `json:"source,omitempty"`
}

type usageWindow struct {
	Key            string  `json:"key"`
	Utilization    float64 `json:"utilization"`
	ResetsAtUnixMs int64   `json:"resets_at_unix_ms,omitempty"`
}

// usageFrom converts the status message to the CLI shape, or nil when the
// agent carries no usage.
func usageFrom(u *machinev1.ClaudeUsage) *agentUsage {
	if u == nil {
		return nil
	}
	out := &agentUsage{Stale: u.Stale, Unavailable: u.Unavailable, Renew: u.Renew, Detail: u.Detail, Source: u.Source, Windows: []usageWindow{}}
	if u.FetchedAt != nil {
		out.FetchedAt = u.FetchedAt.AsTime().UnixMilli()
	}
	for _, w := range u.Windows {
		uw := usageWindow{Key: w.Key, Utilization: w.Utilization}
		if w.ResetsAt != nil {
			uw.ResetsAtUnixMs = w.ResetsAt.AsTime().UnixMilli()
		}
		out.Windows = append(out.Windows, uw)
	}
	return out
}

type agentModelCatalog struct {
	Scope        string       `json:"scope"`
	Source       string       `json:"source"`
	State        string       `json:"state"`
	Models       []agentModel `json:"models"`
	DefaultModel string       `json:"default_model"`
	CurrentModel string       `json:"current_model"`
	ObservedAt   int64        `json:"observed_at_unix_ms"`
	ExpiresAt    int64        `json:"expires_at_unix_ms"`
	Stale        bool         `json:"stale"`
}

type agentModel struct {
	ID      string   `json:"id"`
	Name    string   `json:"name,omitempty"`
	Efforts []string `json:"efforts"`
	// The specific model an alias ID runs on that machine (工单 #99).
	Resolved string `json:"resolved,omitempty"`
	// Where its agent offers it from (DSH's provider lines, 工单 #98 补充).
	Source string `json:"source,omitempty"`
	// Open model metadata enriched on exact match only (工单 #114).
	ContextWindow        uint32   `json:"context_window,omitempty"`
	MaxOutputTokens      uint32   `json:"max_output_tokens,omitempty"`
	Capabilities         []string `json:"capabilities,omitempty"`
	InputCostPerMillion  float64  `json:"input_cost_per_million,omitempty"`
	OutputCostPerMillion float64  `json:"output_cost_per_million,omitempty"`
}

type agentMode struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

func (a *app) agentCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "agent", Short: "The coding agents this client may run, and setting one up on this machine"}
	cmd.AddCommand(a.agentListCmd())
	var method string
	login := &cobra.Command{Use: "login antigravity", Short: "Sign in to the official Antigravity ACP server on this machine", Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if args[0] != "antigravity" {
				return fail(exitUsage, "agent login supports antigravity; use other agents' own login commands")
			}
			for _, p := range acp.Providers("") {
				if p.Name() != args[0] {
					continue
				}
				fmt.Fprintln(c.ErrOrStderr(), "Follow the Antigravity sign-in link below. Press Ctrl+C to cancel.")
				if err := p.Login(c.Context(), method, c.ErrOrStderr()); err != nil {
					return err
				}
				if a.output == "json" {
					return a.report(c.OutOrStdout(), nil, field{"agent", p.Name()}, field{"authenticated", true})
				}
				fmt.Fprintln(c.OutOrStdout(), "Antigravity ACP signed in. Aide can now start its sessions.")
				return nil
			}
			return fail(exitUsage, "unknown agent %q", args[0])
		}}
	login.Flags().StringVar(&method, "method", "oauth-personal", "ACP authentication method (oauth-personal, oauth-business, gemini-api-key, agent-platform)")
	cmd.AddCommand(login)
	return cmd
}

func (a *app) agentListCmd() *cobra.Command {
	var machineFlag, agentFlag string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the coding agents this client may run on each machine, with the models and reasoning efforts they offer",
		Long: `List the coding agents this client may run: installed and logged in on
the machine, and in the grant. Each shows the models it offers there, with
the reasoning efforts each model takes, and the model and effort a session
runs with when session start names none (the owner's default for the agent
on that machine, else the agent's own; empty when Aide can't tell).
Pass a model's id to session start --model and one of its efforts to
--effort. An ACP agent (deepseek, gemini, ...) also lists its session modes
(permission presets) for --mode; it tells its models, efforts and modes
only in a session, so they show once one of its sessions has started on
that machine. Deepseek's modes are DeepSeek Harness's permission presets
(read-only, workspace-write, danger-full-access, ...), listed before any
session.

Antigravity reports scoped model_catalogs instead of a machine-wide model
list. Each snapshot includes scope, source, state, observation/expiry times,
and stale. A catalog's default_model is the initial selection in that scope;
current_model may change. An unknown or session_required catalog is not an
empty entitlement list. Only state=known confirms the reported choices
(including an empty list); expired snapshots are historical. The session's
own config options remain authoritative. Listing never creates a session
or sends a prompt. It queries session/options only when advertised.

A machine that is offline is listed after the agents as machine/*: its
agents cannot be listed until it is back. Being offline is not an error.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			c, machines, err := a.connect(ctx, warmTargets(machineFlag))
			if err != nil {
				return err
			}
			defer c.Close()
			machines, err = targetMachines(machines, machineFlag)
			if err != nil {
				return err
			}
			agents, errs := a.listAgents(ctx, c, machines, agentFlag)
			offline := offlineOf(machines, errs)
			w := cmd.OutOrStdout()
			if a.output == "json" {
				return a.report(w, nil, field{"agents", agents}, field{"offline", offline})
			}
			if len(agents) == 0 && len(offline) == 0 {
				fmt.Fprintln(w, "no coding agent this client may run")
				return nil
			}
			tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
			fmt.Fprintln(tw, "AGENT\tMACHINE\tDEFAULT\tMODELS")
			for _, ag := range agents {
				var ids []string
				for _, m := range ag.Models {
					ids = append(ids, m.ID)
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", ag.Agent, ag.Machine, cmp.Or(strings.Join(strings.Fields(ag.DefaultModel+" "+ag.DefaultEffort+" "+ag.DefaultMode), " "), "-"), cmp.Or(strings.Join(ids, ", "), "-"))
				for _, c := range ag.ModelCatalogs {
					ids := []string{}
					for _, m := range c.Models {
						ids = append(ids, m.ID)
					}
					fmt.Fprintf(tw, "  %s\t%s\t%s\t%s (%s; %s; observed=%d; stale=%t)\n", c.Scope, ag.Machine,
						cmp.Or(c.DefaultModel, "unknown"), cmp.Or(strings.Join(ids, ", "), "-"), c.State, c.Source, c.ObservedAt, c.Stale)
				}
			}
			for _, m := range offline {
				fmt.Fprintf(tw, "%s/*\t-\t-\toffline: %s\n", m.Machine, offlineText(m))
			}
			tw.Flush()
			return nil
		},
	}
	cmd.Flags().StringVar(&machineFlag, "machine", "", "only this machine (name or id)")
	cmd.Flags().StringVar(&agentFlag, "agent", "", "only this agent (e.g. codex)")
	return cmd
}

// listAgents lists the coding agents this client may run on the online
// machines (only agent, when set); errs holds the machines that could not
// be asked.
func (a *app) listAgents(ctx context.Context, c *peer.Client, machines []machineRef, agent string) ([]machineAgent, map[string]error) {
	names := map[string]string{}
	status := map[string]*machinev1.MachineStatus{}
	var mu sync.Mutex
	errs := c.FanOut(ctx, ids(machines), func(ctx context.Context, n *peer.Machine) error {
		info, err := n.Info.GetInfo(ctx, &machinev1.GetInfoRequest{})
		if err != nil {
			return err
		}
		mu.Lock()
		names[n.MachineID], status[n.MachineID] = n.Name, info.Status
		mu.Unlock()
		return nil
	})
	out := []machineAgent{}
	for _, m := range machines {
		for _, ag := range status[m.ID].GetAgents() {
			if !a.mayRun(ag) || (agent != "" && ag.Name != agent) {
				continue
			}
			e := machineAgent{Agent: ag.Name, Machine: cmp.Or(names[m.ID], m.Name), MachineID: m.ID, Version: ag.Version, Models: []agentModel{},
				DefaultModel: cmp.Or(ag.Defaults.GetModel(), ag.Own.GetModel()), DefaultEffort: cmp.Or(ag.Defaults.GetEffort(), ag.Own.GetEffort()),
				DefaultMode: cmp.Or(ag.Defaults.GetMode(), ag.Own.GetMode()), Usage: usageFrom(ag.GetUsage())}
			for _, x := range ag.Models {
				e.Models = append(e.Models, agentModel{
					ID: x.Id, Name: x.Name, Efforts: append([]string{}, x.Efforts...), Resolved: x.Resolved, Source: x.Source,
					ContextWindow: x.ContextWindow, MaxOutputTokens: x.MaxOutputTokens,
					Capabilities: append([]string{}, x.Capabilities...),
					InputCostPerMillion: x.InputCostPerMillion, OutputCostPerMillion: x.OutputCostPerMillion,
				})
			}
			for _, x := range ag.Modes {
				e.Modes = append(e.Modes, agentMode{ID: x.Id, Name: x.Name})
			}
			for _, c := range ag.ModelCatalogs {
				v := agentModelCatalog{Scope: c.Scope, Source: c.Source, State: c.State,
					Models: []agentModel{}, DefaultModel: c.DefaultModel, CurrentModel: c.CurrentModel,
					ObservedAt: c.ObservedAtUnixMs, ExpiresAt: c.ExpiresAtUnixMs,
					Stale: c.ExpiresAtUnixMs != 0 && time.Now().UnixMilli() >= c.ExpiresAtUnixMs}
				for _, m := range c.Models {
					v.Models = append(v.Models, agentModel{
						ID: m.Id, Name: m.Name, Efforts: append([]string{}, m.Efforts...), Resolved: m.Resolved, Source: m.Source,
						ContextWindow: m.ContextWindow, MaxOutputTokens: m.MaxOutputTokens,
						Capabilities: append([]string{}, m.Capabilities...),
						InputCostPerMillion: m.InputCostPerMillion, OutputCostPerMillion: m.OutputCostPerMillion,
					})
				}
				e.ModelCatalogs = append(e.ModelCatalogs, v)
			}
			out = append(out, e)
		}
	}
	return out, errs
}
