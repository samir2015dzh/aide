package cli

import (
	"context"
	"fmt"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	"github.com/samir2015dzh/aide/internal/peer"
)

// machineUsage is the Claude plan usage read on one machine, for the
// scheduler to pace work by. The token it is read from never leaves the
// machine; only these windows and when they reset are reported.
type machineUsage struct {
	Machine   string      `json:"machine"`
	MachineID string      `json:"machine_id"`
	Usage     *agentUsage `json:"usage"`
}

func (a *app) usageCmd() *cobra.Command {
	var machineFlag string
	cmd := &cobra.Command{
		Use:   "usage",
		Short: "Show Claude plan usage (5-hour and weekly windows) on each machine, for pacing work",
		Long: `Show how full the Claude plan's rate-limit windows are on each machine —
the five-hour window and the weekly limits, each as a percent used and when
it resets — so the scheduler can hold work before it hits a cap.

The reading is taken on the machine from the signed-in account's token
(Claude Desktop, or the Claude Code credentials file); the token never
leaves the machine, and nothing here carries it, only the windows. When
there is no reading the usage shows as unavailable with the reason
(signed_out, no_token, api_error, parse_error, unsupported). A reading kept after a
failed refresh is marked stale.`,
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
			rows, errs := a.readUsage(ctx, c, machines)
			offline := offlineOf(machines, errs)
			w := cmd.OutOrStdout()
			if a.output == "json" {
				return a.report(w, nil, field{"usage", rows}, field{"offline", offline})
			}
			if len(rows) == 0 && len(offline) == 0 {
				fmt.Fprintln(w, "no machine to read Claude usage on")
				return nil
			}
			tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
			fmt.Fprintln(tw, "MACHINE\tWINDOW\tUSED\tRESETS\tNOTE")
			for _, r := range rows {
				printUsageRow(tw, r)
			}
			for _, m := range offline {
				fmt.Fprintf(tw, "%s\t-\t-\t-\toffline: %s\n", m.Machine, offlineText(m))
			}
			tw.Flush()
			return nil
		},
	}
	cmd.Flags().StringVar(&machineFlag, "machine", "", "only this machine (name or id)")
	return cmd
}

func printUsageRow(tw *tabwriter.Writer, r machineUsage) {
	u := r.Usage
	if u == nil {
		fmt.Fprintf(tw, "%s\t-\t-\t-\tno Claude Code\n", r.Machine)
		return
	}
	note := ""
	if u.Stale {
		note = "stale"
	}
	if u.Renew != "" {
		note = joinNote(note, "renew="+u.Renew)
	}
	if u.Unavailable != "" {
		fmt.Fprintf(tw, "%s\t-\t-\t-\t%s\n", r.Machine, joinNote("read-fail: "+u.Unavailable, note))
		return
	}
	if len(u.Windows) == 0 {
		fmt.Fprintf(tw, "%s\t-\t-\t-\t%s\n", r.Machine, joinNote("no windows", note))
		return
	}
	for i, win := range u.Windows {
		name := r.Machine
		if i > 0 {
			name = "" // group the machine's windows under one name
		}
		fmt.Fprintf(tw, "%s\t%s\t%.0f%%\t%s\t%s\n", name, win.Key, win.Utilization, resetText(win.ResetsAtUnixMs), note)
		note = ""
	}
}

func joinNote(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + "; " + b
}

func resetText(unixMs int64) string {
	if unixMs == 0 {
		return "-"
	}
	d := time.Until(time.UnixMilli(unixMs))
	if d <= 0 {
		return "due"
	}
	if d < time.Hour {
		return fmt.Sprintf("in %dm", int(d.Minutes()))
	}
	return fmt.Sprintf("in %dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

// readUsage asks each online machine for its Claude plan usage.
func (a *app) readUsage(ctx context.Context, c *peer.Client, machines []machineRef) ([]machineUsage, map[string]error) {
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
	out := []machineUsage{}
	for _, m := range machines {
		var usage *agentUsage
		for _, ag := range status[m.ID].GetAgents() {
			if ag.Name == "claude" {
				usage = usageFrom(ag.GetUsage())
				break
			}
		}
		name := m.Name
		if n := names[m.ID]; n != "" {
			name = n
		}
		out = append(out, machineUsage{Machine: name, MachineID: m.ID, Usage: usage})
	}
	return out, errs
}
