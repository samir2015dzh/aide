package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	"github.com/samir2015dzh/aide/internal/peer"
)

// The ops channel (docs/machine-ops.md): one-shot commands on a machine,
// for an agent the owner allowed to run them.

// maxExecTimeout is the longest a machine lets a command run.
const maxExecTimeout = 10 * time.Minute

func (a *app) machineCmd() *cobra.Command {
	cmd := group(&cobra.Command{Use: "machine", Short: "Operate the owner's machines directly"})
	cmd.AddCommand(a.machineExecCmd())
	return cmd
}

func (a *app) machineExecCmd() *cobra.Command {
	var command, workdir string
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "exec <machine>",
		Short: "Run a one-shot shell command on a machine and get its output and exit code",
		Long: `Run one command on a machine, as a new terminal there would, and print its
output and exit code: PowerShell on Windows, sh elsewhere. Give it with
--command, or after the machine and "--" (aide machine exec vps-1 -- git
status); its words are joined with spaces, as ssh does. It runs in --workdir
("~/app" or a full path; default the home folder) and is ended after
--timeout (default 60s, at most 10m). Each of stdout and stderr is cut at 1 MiB.

The owner allows this per agent in Aide (Settings → Access → Run
commands); until then the machine refuses. Every call, refused ones too, is
in the machine's ops log, which the owner sees; the output is kept nowhere.
Exit code 0 when the command exited 0, 5 when it exited otherwise (its own
exit code is in the output), 6 when it ran out of time, 3 when refused.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if command != "" && len(args) > 1 {
				return &exitError{code: exitUsage, kind: "usage", err: errors.New("give the command with --command or after --, not both")}
			}
			if command == "" {
				command = strings.Join(args[1:], " ")
			}
			if strings.TrimSpace(command) == "" {
				return &exitError{code: exitUsage, kind: "usage", err: errors.New("no command"), hint: "aide machine exec <machine> -- <command>"}
			}
			if timeout < 0 || timeout > maxExecTimeout || timeout%time.Second != 0 {
				return &exitError{code: exitUsage, kind: "usage", err: fmt.Errorf("--timeout %s: whole seconds, at most %s", timeout, maxExecTimeout)}
			}
			ctx := cmd.Context()
			c, machines, err := a.connect(ctx, warmTargets(args[0]))
			if err != nil {
				return err
			}
			defer c.Close()
			ref, err := resolveMachine(machines, args[0], "")
			if err != nil {
				return err
			}
			return a.onMachine(ctx, c, ref, func(_ *peer.Client, n *peer.Machine, ref machineRef) error {
				res, err := n.Ops.Exec(ctx, &machinev1.ExecRequest{Command: command, Workdir: workdir, TimeoutSeconds: uint32(timeout / time.Second)})
				if connect.CodeOf(err) == connect.CodePermissionDenied {
					return &exitError{code: exitDenied, kind: "denied", err: err,
						hint: "the owner allows this agent to run commands in Aide: Settings → Access → this agent → Run commands"}
				}
				if err != nil {
					return err
				}
				return a.printExec(cmd, ref, command, res)
			})
		},
	}
	cmd.Flags().StringVar(&command, "command", "", "the command to run (or give it after the machine and --)")
	cmd.Flags().StringVar(&workdir, "workdir", "", `where it runs: "~/app" or a full path (default the home folder)`)
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "end it after this long (default 60s, at most 10m)")
	return cmd
}

// printExec prints what the command did, and fails as it did.
func (a *app) printExec(cmd *cobra.Command, ref machineRef, command string, res *machinev1.ExecResponse) error {
	var err error
	switch {
	case res.TimedOut:
		err = &exitError{code: exitTimeout, kind: "timeout", err: fmt.Errorf("the command ran out of time on %s and was ended", ref.Name)}
	case res.ExitCode != 0:
		err = &exitError{code: exitFailed, kind: "command_failed", err: fmt.Errorf("the command exited with code %d on %s", res.ExitCode, ref.Name)}
	}
	if a.output == "json" {
		return a.report(cmd.OutOrStdout(), err, field{"exec", object{{"machine", ref.Name}, {"machine_id", ref.ID}, {"command", command},
			{"workdir", res.Workdir}, {"shell", res.Shell}, {"exit_code", res.ExitCode}, {"timed_out", res.TimedOut},
			{"stdout", res.Stdout}, {"stderr", res.Stderr}, {"stdout_truncated", res.StdoutTruncated}, {"stderr_truncated", res.StderrTruncated},
			{"started_at", tsJSON(res.StartedAt)}, {"ended_at", tsJSON(res.EndedAt)},
			{"duration_ms", res.EndedAt.AsTime().Sub(res.StartedAt.AsTime()).Milliseconds()}}})
	}
	fmt.Fprint(cmd.OutOrStdout(), res.Stdout)
	fmt.Fprint(cmd.ErrOrStderr(), res.Stderr)
	for _, cut := range []struct {
		what string
		cut  bool
	}{{"stdout", res.StdoutTruncated}, {"stderr", res.StderrTruncated}} {
		if cut.cut {
			fmt.Fprintf(cmd.ErrOrStderr(), "(%s cut at 1 MiB)\n", cut.what)
		}
	}
	return err
}
