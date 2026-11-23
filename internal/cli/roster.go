package cli

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	relayv1 "github.com/samir2015dzh/aide/gen/go/aide/relay/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/peer"
	"github.com/samir2015dzh/aide/pkg/identity"
)

// The roster (docs/agent-messages.md §15): every endpoint a message can go
// to, as this agent may see it. Nothing new is asked of the relay: the
// agents are the owner's contacts with what the relay knows of their
// connections, and the sessions are what each machine says end to end, as
// session list reads them. What a session is doing is never its own say:
// its title, its current tool or its last words. Its state is
// docs/session-states.md's, as session list gives it.

type endpointJSON struct {
	// Address is what message send --to takes; Name, the readable one it
	// also takes (an agent's name, <machine>/<session name>).
	Address string `json:"address"`
	Name    string `json:"name"`
	// Kind: "cli" or "mcp" (an agent), "aide_session", "dsh_session",
	// "claude_app_session" (a Claude Code conversation the Claude app runs).
	Kind string `json:"kind"`
	// Category: "personal" (an agent of the owner's, following the account)
	// or "local" (a coding agent on a machine, and every session there; then
	// Machine names it when known). Personal agents are addressed by their
	// inbox, a local one by machine and session (工单 #26/#49).
	Category string `json:"category"`
	// Tier is when a message reaches it: "push", "turn" or "pull".
	Tier string `json:"tier"`
	// Mailable: a message can go to it now (a DSH session only once the
	// owner took it over).
	Mailable bool `json:"mailable"`
	// An agent's: connected to the relay now, or when it last was.
	Online   *bool   `json:"online,omitempty"`
	LastSeen *string `json:"last_seen,omitempty"`
	You      bool    `json:"you,omitempty"`
	// A session's.
	Agent          string  `json:"agent,omitempty"`
	Machine        string  `json:"machine,omitempty"`
	MachineID      string  `json:"machine_id,omitempty"`
	State          string  `json:"state,omitempty"`
	Link           string  `json:"link,omitempty"`
	StateText      string  `json:"state_text,omitempty"`
	StalledSeconds int64   `json:"stalled_seconds,omitempty"`
	Doing          string  `json:"doing,omitempty"`
	StartedAt      *string `json:"started_at,omitempty"`
	LastActivity   *string `json:"last_activity,omitempty"`
	CreatedBy      string  `json:"created_by,omitempty"`
	Takeover       *bool   `json:"takeover,omitempty"`
	// AgentPaused: the owner paused the session's coding agent on its
	// machine (SetAgentPaused): it takes no new session, this one runs on.
	AgentPaused bool `json:"agent_paused,omitempty"`
}

// pausedAgentJSON is a coding agent the owner paused on a machine.
type pausedAgentJSON struct {
	Machine   string  `json:"machine"`
	MachineID string  `json:"machine_id"`
	Agent     string  `json:"agent"`
	Since     *string `json:"since"`
	ResumeAt  *string `json:"resume_at"`
}

// roster is what the roster gathered: the endpoints, the coding agents
// paused on the machines, and the machines that did not answer.
type roster struct {
	Endpoints []endpointJSON
	Paused    []pausedAgentJSON
	Offline   []offlineMachine
}

// sessionName is a session's name in addresses (<machine>/<name>): its
// title as names are written (identity.NormalizeName), at most
// identity.MaxNameLen letters and digits; its id when it has no title.
func sessionName(title, id string) string {
	r := []rune(title)
	for len(r) > 0 {
		if n, err := identity.NormalizeName(string(r)); err == nil {
			return n
		}
		r = r[:len(r)-1] // too long (or nothing to keep): shorter
	}
	return id
}

// namedSession is a session to name on its machine.
type namedSession struct {
	id, title string
	created   time.Time
}

// sessionNames names a machine's sessions (sessionName), the older of two
// with one name keeping it and the later ones taking -2, -3, …: a name
// stays the session's while it lives, worked out again each time.
func sessionNames(list []namedSession) map[string]string {
	sorted := slices.Clone(list)
	slices.SortStableFunc(sorted, func(x, y namedSession) int {
		if c := x.created.Compare(y.created); c != 0 {
			return c
		}
		return strings.Compare(x.id, y.id)
	})
	taken := map[string]int{}
	out := map[string]string{}
	for _, s := range sorted {
		n := sessionName(s.title, s.id)
		taken[n]++
		if k := taken[n]; k > 1 {
			n = fmt.Sprintf("%s-%d", n, k)
		}
		out[s.id] = n
	}
	return out
}

func machineNames(ms []machineRef) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Name
	}
	return out
}

// gatherRoster reads the roster: the contacts (with presence) through ms,
// the sessions from each machine.
func (a *app) gatherRoster(ctx context.Context, c *peer.Client, machines []machineRef, ms rosterContacts) (roster, error) {
	res, err := ms.Contacts(ctx, &relayv1.ContactsRequest{})
	if err != nil {
		return roster{}, err
	}
	if res.RootChain != nil {
		a.trust.AcceptRootChain(res.RootChain)
	}
	woken := map[string][]string{}
	for _, w := range res.Woken {
		woken[w.Inbox] = w.Names
	}
	presence := map[string]*relayv1.Presence{}
	for _, p := range res.Presence {
		presence[p.Inbox] = p
	}
	var out roster
	for _, ct := range res.Contacts {
		info, err := a.trust.VerifyContact(ct)
		if err != nil || info.Kind == "machine" {
			continue
		}
		tier := info.Tier
		if tier == "" {
			tier = identity.TierOf(info.TakesMessages)
		}
		if rings(woken[info.Inbox], "") {
			tier = "push" // its doorbell wakes it
		}
		cat, home := agentCategory(info.Name, machineNames(machines))
		// What the owner set wins over the name (工单 #52).
		if info.Category != "" {
			cat, home = info.Category, ""
			for _, m := range machines {
				if m.ID == info.CategoryMachineID {
					home = m.Name
				}
			}
		}
		if !onRoster(info.Name, cat, info.Category != "") {
			continue
		}
		e := endpointJSON{Address: info.Inbox, Name: info.Name, Kind: info.Kind, Category: cat, Machine: home, Tier: tier, Mailable: info.InboxKey != nil, You: info.Inbox == a.g.ClientId}
		if p := presence[info.Inbox]; p != nil {
			online := p.Online
			e.Online, e.LastSeen = &online, tsJSON(p.LastSeen)
		}
		out.Endpoints = append(out.Endpoints, e)
		// Its names with a doorbell: endpoints of their own, woken.
		for _, n := range woken[info.Inbox] {
			if n != "" {
				out.Endpoints = append(out.Endpoints, endpointJSON{Address: info.Inbox + "/" + n, Name: info.Name + "/" + n, Kind: info.Kind, Category: cat, Machine: home, Tier: "push", Mailable: e.Mailable, Online: e.Online, LastSeen: e.LastSeen})
			}
		}
	}

	names := map[string]string{}
	for _, n := range machines {
		names[n.ID] = n.Name
	}
	type machineRead struct {
		sessions []*sessionv1.Session
		dsh      []*machinev1.DSHSession
		claude   []*machinev1.ClaudeSession
		status   *machinev1.MachineStatus
	}
	read := map[string]*machineRead{}
	var mu sync.Mutex
	errs := c.FanOut(ctx, ids(machines), func(ctx context.Context, n *peer.Machine) error {
		r := &machineRead{}
		ss, err := n.Session.ListSessions(ctx, &sessionv1.ListSessionsRequest{})
		if err != nil {
			return err
		}
		r.sessions = ss.Sessions
		if info, err := n.Info.GetInfo(ctx, &machinev1.GetInfoRequest{}); err == nil {
			r.status = info.Status
		}
		d, err := n.Info.ListDSHSessions(ctx, &machinev1.ListDSHSessionsRequest{})
		switch {
		case err == nil:
			r.dsh = d.Sessions
		case connect.CodeOf(err) == connect.CodeUnimplemented, connect.CodeOf(err) == connect.CodePermissionDenied:
			// From before agents read them, or a grant that sees no
			// sessions it did not start: none listed.
		default:
			return err
		}
		cs, err := n.Info.ListClaudeSessions(ctx, &machinev1.ListClaudeSessionsRequest{})
		switch {
		case err == nil:
			r.claude = cs.Sessions
		case connect.CodeOf(err) == connect.CodeUnimplemented, connect.CodeOf(err) == connect.CodePermissionDenied:
			// From before the roster listed them: none.
		default:
			return err
		}
		mu.Lock()
		read[n.MachineID] = r
		names[n.MachineID] = n.Name // as the machine itself says
		mu.Unlock()
		return nil
	})
	out.Offline = offlineOf(machines, errs)
	for _, m := range machines {
		r := read[m.ID]
		if r == nil {
			continue
		}
		paused := map[string]bool{}
		for _, ag := range r.status.GetAgents() {
			if p := ag.GetPause(); p != nil {
				paused[ag.Name] = true
				out.Paused = append(out.Paused, pausedAgentJSON{Machine: names[m.ID], MachineID: m.ID, Agent: ag.Name, Since: tsJSON(p.Since), ResumeAt: tsJSON(p.ResumeAt)})
			}
		}
		var toName []namedSession
		for _, s := range r.sessions {
			if s.Status != sessionv1.SessionStatus_SESSION_STATUS_ARCHIVED {
				toName = append(toName, namedSession{s.SessionId, s.Title, s.CreatedAt.AsTime()})
			}
		}
		for _, s := range r.dsh {
			if !s.Archived {
				toName = append(toName, namedSession{nativeID(m.ID, s.SessionId), s.Title, s.CreatedAt.AsTime()})
			}
		}
		for _, s := range r.claude {
			if !s.Archived {
				toName = append(toName, namedSession{claudeAppID(m.ID, s.SessionId), s.Title, s.CreatedAt.AsTime()})
			}
		}
		named := sessionNames(toName)
		for _, s := range r.sessions {
			if s.Status == sessionv1.SessionStatus_SESSION_STATUS_ARCHIVED {
				continue
			}
			v := sessionView(s, names[m.ID])
			out.Endpoints = append(out.Endpoints, endpointJSON{Address: m.ID + "/" + s.SessionId, Name: names[m.ID] + "/" + named[s.SessionId], Kind: "aide_session", Category: categoryLocal,
				Tier: "push", Mailable: true, Agent: s.Agent, Machine: names[m.ID], MachineID: m.ID, State: v.State, Link: v.Link, StateText: v.StateText,
				StalledSeconds: v.StalledFor, Doing: oneLine(firstNonEmpty(v.CurrentTool, v.LastMessage, v.Title), 120), StartedAt: v.CreatedAt,
				LastActivity: firstNonNil(v.LastEventAt, v.UpdatedAt), CreatedBy: s.CreatedBy, AgentPaused: paused[s.Agent]})
		}
		for _, s := range r.dsh {
			if s.Archived {
				continue
			}
			id := nativeID(m.ID, s.SessionId)
			takeover := s.Takeover
			out.Endpoints = append(out.Endpoints, endpointJSON{Address: id, Name: names[m.ID] + "/" + named[id], Kind: "dsh_session", Category: categoryLocal, Tier: "push", Mailable: takeover,
				Agent: "deepseek", Machine: names[m.ID], MachineID: m.ID, State: nativeState(s.State), Doing: oneLine(s.Title, 120),
				StartedAt: tsJSON(s.CreatedAt), LastActivity: tsJSON(s.LastActivityAt), Takeover: &takeover})
		}
		// The Claude app's sessions (docs/agent-messages.md §15.4): mailable
		// at the address its CLI agent's hooks reported for the conversation,
		// a conversation of that agent; one the hooks never saw has only the
		// app's id here, and takes no message.
		for _, s := range r.claude {
			if s.Archived {
				continue
			}
			id := claudeAppID(m.ID, s.SessionId)
			addr := firstNonEmpty(s.Address, id)
			out.Endpoints = append(out.Endpoints, endpointJSON{Address: addr, Name: names[m.ID] + "/" + named[id], Kind: "claude_app_session", Category: categoryLocal, Tier: "turn",
				Mailable: s.Address != "", Agent: "claude", Machine: names[m.ID], MachineID: m.ID, State: firstNonEmpty(s.Status, "unknown"),
				Doing: oneLine(s.Title, 120), StartedAt: tsJSON(s.CreatedAt), LastActivity: tsJSON(s.LastActivityAt)})
		}
	}
	return out, nil
}

// claudeAppID is a Claude app session's id in the roster while it has no
// address: its machine and the app's id (local_…).
func claudeAppID(machineID, appID string) string { return machineID + "/" + appID }

// rosterContacts is the part of the message service the roster reads.
type rosterContacts interface {
	Contacts(context.Context, *relayv1.ContactsRequest) (*relayv1.ContactsResponse, error)
}

func firstNonNil(ts ...*string) *string {
	for _, t := range ts {
		if t != nil {
			return t
		}
	}
	return nil
}

func (a *app) rosterCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "roster",
		Short: "Everyone and every session a message can go to: the owner's agents, and the sessions on the machines you reach",
		Long: `Everyone and every session a message can go to, in one list (docs/agent-messages.md §15):

- the owner's agents (kind cli or mcp): connected now or when last, and when a message reaches them;
  category personal (follows the account: Muse, Grok Bot) or local (a coding agent on a machine, named in machine)
  (tier: push, it is woken or takes it at once; turn, in its next turn; pull, when it next checks);
- the Aide sessions on the machines you reach (kind aide_session): their state as session list gives
  it, and what they are doing, from their own title, tool or last words, never their say;
- the DeepSeek Harness sessions there (kind dsh_session), mailable once the owner took one over;
- the Claude app's sessions there (kind claude_app_session), mailable once that computer's CLI agent's
  hooks reported the conversation (aide message hook claude --install), at that agent's address.

Each has an address and a name, both of which message send --to takes: an agent's name, or
<machine>/<session name> (a session's title as names are written, -2, -3 for a second and third of
one name on a machine). Sessions this agent's grant can't see are not listed. paused_agents are
the coding agents the owner paused on a machine: they start no new session there.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			c, machines, err := a.connect(ctx, nil)
			if err != nil {
				return err
			}
			defer c.Close()
			ms, err := a.messages(c)
			if err != nil {
				return err
			}
			r, err := a.gatherRoster(ctx, c, machines, ms)
			if err != nil {
				return err
			}
			if a.output == "json" {
				return a.report(cmd.OutOrStdout(), nil, field{"endpoints", orEmpty(r.Endpoints)}, field{"paused_agents", orEmpty(r.Paused)}, field{"offline", r.Offline})
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "NAME\tKIND\tTIER\tSTATE\tADDRESS")
			for _, e := range r.Endpoints {
				state := e.StateText
				if e.Online != nil {
					state = "offline"
					if *e.Online {
						state = "online"
					}
				}
				if !e.Mailable {
					state = firstNonEmpty(state, "-") + " (not mailable)"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", e.Name, e.Kind, e.Tier, firstNonEmpty(state, "-"), e.Address)
			}
			tw.Flush()
			for _, p := range r.Paused {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s is paused on %s\n", p.Agent, p.Machine)
			}
			return nil
		},
	}
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// isBroadcast reports whether to is one of the forms that write to many at
// once: "*", "<machine>/*", "repo:<repository>". Those are the owner's
// alone (docs/agent-messages.md §15), and not yet theirs either: an agent
// writes to one endpoint at a time.
func isBroadcast(to string) bool {
	return to == "*" || strings.HasSuffix(to, "/*") || strings.HasPrefix(to, "repo:")
}

func broadcastRefused(to string) error {
	return &exitError{code: exitUsage, kind: "broadcast_owner_only", err: fmt.Errorf("%q writes to many at once: only the owner may, and agents write to one endpoint at a time", to),
		hint: "write to each one, by its address or name from `aide roster`"}
}

// sessionByName finds the session a readable name stands for, among the
// roster's: <machine>/<session name>, or a session name alone when exactly
// one session on the machines has it. ok is false when none has it; two or
// more are an error naming them.
func sessionByName(to string, endpoints []endpointJSON) (endpointJSON, bool, error) {
	var found []endpointJSON
	for _, e := range endpoints {
		if e.Kind != "aide_session" && e.Kind != "dsh_session" && e.Kind != "claude_app_session" {
			continue
		}
		if e.Name == to {
			if !e.Mailable && e.Kind == "claude_app_session" {
				return endpointJSON{}, false, &exitError{code: exitUsage, kind: "invalid_state", err: fmt.Errorf("%s is a Claude app session that takes no messages yet", to),
					hint: "on its computer, run `aide message hook claude --install`: its next conversation start reports where it takes messages"}
			}
			return e, true, nil // <machine>/<name> is one at most
		}
		if _, name, _ := strings.Cut(e.Name, "/"); name == to && !strings.Contains(to, "/") {
			found = append(found, e)
		}
	}
	switch len(found) {
	case 0:
		return endpointJSON{}, false, nil
	case 1:
		return found[0], true, nil
	}
	var names []string
	for _, e := range found {
		names = append(names, e.Name)
	}
	return endpointJSON{}, false, &exitError{code: exitUsage, kind: "not_found", err: fmt.Errorf("%d sessions are called %q: write one of %s", len(found), to, strings.Join(names, ", ")),
		hint: "name the machine too (<machine>/<session name>), as `aide roster` lists them"}
}
