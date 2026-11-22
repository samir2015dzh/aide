package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	"github.com/samir2015dzh/aide/internal/peer"
)

// DeepSeek Harness's own sessions on the machines, those Aide did not
// start (docs/cli.md): an agent whose grant sees sessions it did not start
// lists and reads them; nothing more. Their ids here are dsh_, the
// machine's part of a session id, and DSH's own id, so they are addressed
// as sessions are and never taken for one.

const nativePrefix = "dsh_"

// isNative: id names a DSH session (nativeID).
func isNative(id string) bool { return strings.HasPrefix(id, nativePrefix) }

// nativeID is the id of DSH's session dshID on the machine machineID.
func nativeID(machineID, dshID string) string {
	m := strings.TrimPrefix(machineID, "m_")
	return nativePrefix + m[:min(8, len(m))] + "_" + dshID
}

// dshID is DSH's own id of the session id names.
func dshID(id string) string {
	parts := strings.SplitN(id, "_", 3)
	if len(parts) < 3 {
		return ""
	}
	return parts[2]
}

// nativeMachine is the machine whose DSH session id names, by the part of
// its id the address carries.
func nativeMachine(machines []machineRef, id string) (machineRef, bool) {
	parts := strings.SplitN(id, "_", 3)
	if len(parts) < 3 || parts[1] == "" {
		return machineRef{}, false
	}
	var found []machineRef
	for _, m := range machines {
		if nativeID(m.ID, parts[2]) == id {
			found = append(found, m)
		}
	}
	if len(found) != 1 {
		return machineRef{}, false
	}
	return found[0], true
}

// readOnly is the error of anything but reading a DSH session.
func readOnly(id string) error {
	return &exitError{code: exitUsage, kind: "invalid_state",
		err:  fmt.Errorf("%s is a DeepSeek Harness session Aide did not start: agents read it, and send it messages only once the owner took it over; it can't be interrupted, archived or taken over from here", id),
		hint: fmt.Sprintf("read it with `aide session get %s` and `aide session events %s`; taken over, `aide message send --to %s`; for work of your own, `aide session start`", id, id, id)}
}

// nativeErr says what an older machine's Unimplemented means.
func nativeErr(ref machineRef, err error) error {
	if connect.CodeOf(err) == connect.CodeUnimplemented {
		return &exitError{code: exitUsage, kind: "invalid_state", err: fmt.Errorf("Aide on %s is too old to show agents its DSH sessions", ref.Name), hint: "the owner updates it"}
	}
	return err
}

type nativeSessionJSON struct {
	SessionID      string  `json:"session_id"`
	Source         string  `json:"source"`
	ReadOnly       bool    `json:"read_only"`
	Takeover       bool    `json:"takeover"`
	DSHSessionID   string  `json:"dsh_session_id"`
	Title          string  `json:"title"`
	Machine        string  `json:"machine"`
	MachineID      string  `json:"machine_id"`
	State          string  `json:"state"`
	TurnOpen       bool    `json:"turn_open"`
	Archived       bool    `json:"archived"`
	Cwd            string  `json:"cwd"`
	AgentPreset    string  `json:"agent_preset"`
	Error          string  `json:"error"`
	CreatedAt      *string `json:"created_at"`
	LastActivityAt *string `json:"last_activity_at"`
}

func nativeView(s *machinev1.DSHSession, machineID, machine string) *nativeSessionJSON {
	return &nativeSessionJSON{SessionID: nativeID(machineID, s.SessionId), Source: "dsh_native", ReadOnly: !s.Takeover, Takeover: s.Takeover, DSHSessionID: s.SessionId, Title: s.Title,
		Machine: machine, MachineID: machineID, State: nativeState(s.State), TurnOpen: s.TurnOpen, Archived: s.Archived, Cwd: s.Cwd,
		AgentPreset: s.AgentPreset, Error: s.Error, CreatedAt: tsJSON(s.CreatedAt), LastActivityAt: tsJSON(s.LastActivityAt)}
}

// nativeState is the contract's spelling of a DSH session's state:
// running, idle, ended, or unknown (where the machine can't tell whether
// DSH has it open, or from a newer machine).
func nativeState(st machinev1.DSHSessionState) string {
	if _, ok := machinev1.DSHSessionState_name[int32(st)]; !ok || st == machinev1.DSHSessionState_DSH_SESSION_STATE_UNSPECIFIED {
		return "unknown"
	}
	return enumName(st, "DSH_SESSION_STATE_")
}

// nativeShown: --status, --archived and --since keep s (state for status,
// last activity for since).
func nativeShown(s *machinev1.DSHSession, status string, since *time.Time) bool {
	switch {
	case status == "archived":
		return s.Archived
	case s.Archived, status != "" && nativeState(s.State) != status:
		return false
	}
	return since == nil || !s.LastActivityAt.AsTime().Before(*since)
}

// writeNative prints the DSH sessions as session list does.
func writeNative(w io.Writer, list []*nativeSessionJSON) {
	if len(list) == 0 {
		fmt.Fprintln(w, "no DSH native sessions")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "DSH NATIVE SESSION (READ-ONLY)\tTITLE\tSTATE\tLAST ACTIVITY\tMACHINE\tFOLDER")
	for _, v := range list {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", v.SessionID, oneLine(v.Title, 40), v.State, agoOf(v.LastActivityAt), v.Machine, v.Cwd)
	}
	tw.Flush()
}

// agoOf is how long ago a contract time was.
func agoOf(ts *string) string {
	if ts == nil {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, *ts)
	if err != nil {
		return *ts
	}
	return approx(time.Since(t)) + " ago"
}

// readNative reads DSH's session id on its machine: the session and a
// page of its events.
func (a *app) readNative(ctx context.Context, id string, req *machinev1.ReadDSHSessionRequest, fn func(res *machinev1.ReadDSHSessionResponse, ref machineRef) error) error {
	return a.sessionMachine(ctx, id, func(_ *peer.Client, n *peer.Machine, ref machineRef) error {
		req.SessionId = dshID(id)
		res, err := n.Info.ReadDSHSession(ctx, req)
		if connect.CodeOf(err) == connect.CodeNotFound {
			return &exitError{code: exitUsage, kind: "not_found", err: fmt.Errorf("no DSH session %s this agent may read on %s", id, ref.Name),
				hint: "see `aide session list --native`"}
		}
		if err != nil {
			return nativeErr(ref, err)
		}
		return fn(res, ref)
	})
}

// nativeOptionsJSON is what a taken-over DSH session may be set to
// (message send --model, --permission), and what it has now.
type nativeOptionsJSON struct {
	Models            []nativeModelJSON `json:"models"`
	Permissions       []string          `json:"permissions"`
	CurrentModel      string            `json:"current_model"`
	CurrentPermission string            `json:"current_permission"`
}

type nativeModelJSON struct {
	Model   string   `json:"model"` // provider/model, as --model takes it
	Name    string   `json:"name"`
	Efforts []string `json:"efforts"`
}

func modelArg(m *machinev1.DSHModel) string {
	if m == nil || m.Model == "" {
		return ""
	}
	s := m.Provider + "/" + m.Model
	if m.Effort != "" {
		s += "/" + m.Effort
	}
	return s
}

// parseModelArg reads --model: provider/model[/effort].
func parseModelArg(s string) (*machinev1.DSHModel, error) {
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, "/")
	if len(parts) < 2 || len(parts) > 3 || parts[0] == "" || parts[1] == "" {
		return nil, &exitError{code: exitUsage, kind: "usage", err: fmt.Errorf("--model %q: give provider/model or provider/model/effort", s)}
	}
	m := &machinev1.DSHModel{Provider: parts[0], Model: parts[1]}
	if len(parts) == 3 {
		m.Effort = parts[2]
	}
	return m, nil
}

// sendNative sends text to a DSH session the owner took over, through
// its machine's Aide plugin in DSH Desktop.
func (a *app) sendNative(ctx context.Context, cmd *cobra.Command, id, text, model, permission string) error {
	m, err := parseModelArg(model)
	if err != nil {
		return err
	}
	return a.sessionMachine(ctx, id, func(_ *peer.Client, n *peer.Machine, ref machineRef) error {
		res, err := n.Info.SendDSHMessage(ctx, &machinev1.SendDSHMessageRequest{SessionId: dshID(id), Text: text, Model: m, Permission: permission})
		switch connect.CodeOf(err) {
		case connect.CodeNotFound:
			return &exitError{code: exitUsage, kind: "not_found", err: fmt.Errorf("no DSH session %s this agent may read on %s", id, ref.Name), hint: "see `aide session list --native`"}
		case connect.CodeFailedPrecondition, connect.CodeDeadlineExceeded:
			var ce *connect.Error
			if errors.As(err, &ce) {
				err = errors.New(ce.Message())
			}
			return &exitError{code: exitUsage, kind: "invalid_state", err: err, hint: fmt.Sprintf("read it meanwhile with `aide session events %s`", id)}
		}
		if err != nil {
			return nativeErr(ref, err)
		}
		if a.output == "json" {
			// Handed to DSH, which takes it after the turn running there
			// (docs/agent-messages.md §15).
			return a.report(cmd.OutOrStdout(), nil, field{"sent", map[string]any{"session_id": id, "request_id": res.RequestId,
				"delivery": []deliveryJSON{{To: id, Result: "handed", Tier: "push", Expect: "DSH takes it after the turn running there, or now if none runs"}}}})
		}
		fmt.Fprintf(cmd.OutOrStdout(), "sent to %s on %s (DSH takes it after the turn running there)\n", id, ref.Name)
		return nil
	})
}

// getNative is session get of a DSH session.
func (a *app) getNative(cmd *cobra.Command, id string) error {
	return a.readNative(cmd.Context(), id, &machinev1.ReadDSHSessionRequest{Limit: 1}, func(res *machinev1.ReadDSHSessionResponse, ref machineRef) error {
		v := nativeView(res.Session, ref.ID, ref.Name)
		var opts *nativeOptionsJSON
		if v.Takeover {
			opts = a.nativeOptions(cmd.Context(), id, ref)
		}
		if a.output == "json" {
			return a.report(cmd.OutOrStdout(), nil, field{"native_session", v}, field{"options", opts})
		}
		w := cmd.OutOrStdout()
		mode := "read-only"
		if v.Takeover {
			mode = "taken over: `aide message send --to " + v.SessionID + "`"
		}
		fmt.Fprintf(w, "session %s · DSH native, %s · %s · %s · %s\n", v.SessionID, mode, v.State, v.Machine, firstNonEmpty(v.Cwd, "no folder"))
		if opts != nil {
			fmt.Fprintf(w, "model: %s · permission: %s\n", firstNonEmpty(opts.CurrentModel, "?"), firstNonEmpty(opts.CurrentPermission, "?"))
			for _, m := range opts.Models {
				fmt.Fprintf(w, "  --model %s%s\n", m.Model, map[bool]string{true: " (efforts: " + strings.Join(m.Efforts, ", ") + ")", false: ""}[len(m.Efforts) > 0])
			}
			if len(opts.Permissions) > 0 {
				fmt.Fprintf(w, "  --permission %s\n", strings.Join(opts.Permissions, " | "))
			}
		}
		if v.Title != "" {
			fmt.Fprintf(w, "title: %s\n", v.Title)
		}
		fmt.Fprintf(w, "last activity: %s\n", agoOf(v.LastActivityAt))
		if v.Error != "" {
			fmt.Fprintf(w, "error: %s\n", v.Error)
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "→ its conversation: `aide session events %s`\n", v.SessionID)
		return nil
	})
}

// nativeOptions are what a taken-over DSH session may be set to; nil when
// its machine's plugin can't tell (not running, or older).
func (a *app) nativeOptions(ctx context.Context, id string, ref machineRef) *nativeOptionsJSON {
	var out *nativeOptionsJSON
	a.sessionMachine(ctx, id, func(_ *peer.Client, n *peer.Machine, _ machineRef) error {
		o, err := n.Info.DSHSessionOptions(ctx, &machinev1.DSHSessionOptionsRequest{SessionId: dshID(id)})
		if err != nil {
			return err
		}
		out = &nativeOptionsJSON{Models: []nativeModelJSON{}, Permissions: []string{}, CurrentModel: modelArg(o.CurrentModel), CurrentPermission: o.CurrentPermission}
		for _, m := range o.Models {
			out.Models = append(out.Models, nativeModelJSON{Model: m.Provider + "/" + m.Model, Name: m.ModelName, Efforts: append([]string{}, m.Efforts...)})
		}
		for _, p := range o.Permissions {
			out.Permissions = append(out.Permissions, p.Id)
		}
		return nil
	})
	return out
}
