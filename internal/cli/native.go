package cli

import (
	"context"
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

// readOnly is the error of anything but reading a DSH session.
func readOnly(id string) error {
	return &exitError{code: exitUsage, kind: "invalid_state",
		err:  fmt.Errorf("%s is a DeepSeek Harness session Aide did not start: agents can only read it; it takes no messages or answers, and can't be interrupted, archived or taken over from here", id),
		hint: fmt.Sprintf("read it with `aide session get %s` and `aide session events %s`; for work of your own, `aide session start`", id, id)}
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
	return &nativeSessionJSON{SessionID: nativeID(machineID, s.SessionId), Source: "dsh_native", ReadOnly: true, DSHSessionID: s.SessionId, Title: s.Title,
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

// getNative is session get of a DSH session.
func (a *app) getNative(cmd *cobra.Command, id string) error {
	return a.readNative(cmd.Context(), id, &machinev1.ReadDSHSessionRequest{Limit: 1}, func(res *machinev1.ReadDSHSessionResponse, ref machineRef) error {
		v := nativeView(res.Session, ref.ID, ref.Name)
		if a.output == "json" {
			return a.report(cmd.OutOrStdout(), nil, field{"native_session", v})
		}
		w := cmd.OutOrStdout()
		fmt.Fprintf(w, "session %s · DSH native, read-only · %s · %s · %s\n", v.SessionID, v.State, v.Machine, firstNonEmpty(v.Cwd, "no folder"))
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
