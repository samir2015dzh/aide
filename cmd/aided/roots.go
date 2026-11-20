package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	"github.com/samir2015dzh/aide/internal/machine"
)

// rootsCommand shows and sets this machine's workspace roots from its own
// shell (docs/machine-ops.md §5), as projectCommand changes its projects.
func rootsCommand(dataDir *string, openMachine func() (*machine.Machine, error)) *cobra.Command {
	machineDir := func() string { return filepath.Join(*dataDir, "machine") }
	set := func(cmd *cobra.Command, roots []string) error {
		msg, err := applyLocal(cmd.Context(), machineDir(), openMachine,
			&identityv1.AdminCommand{Command: &identityv1.AdminCommand_SetWorkspaceRoots{SetWorkspaceRoots: &identityv1.SetWorkspaceRoots{Roots: roots}}})
		if err != nil {
			return err
		}
		fmt.Println("✓ " + msg)
		return nil
	}
	roots := &cobra.Command{
		Use:   "roots",
		Short: "This machine's workspace roots: the only folders devices may browse, make projects of and run commands in",
		Long: `Show this machine's workspace roots. With roots set, the owner's devices
browse folders and add projects only inside them, and agents' commands
(aide machine exec --workdir) start only inside them. With none, any folder
is, as before roots. Projects added before stay usable; they are named here.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var r machine.Roots
			if out, ok, err := machine.ControlRequest(cmd.Context(), machineDir(), "GET", "/roots", nil); ok {
				if err != nil {
					return err
				}
				if err := json.Unmarshal(out, &r); err != nil {
					return err
				}
			} else {
				n, err := openMachine()
				if err != nil {
					return err
				}
				r = n.Roots()
				n.Close()
			}
			if len(r.Roots) == 0 {
				fmt.Println("No workspace roots: any folder. Set some: aided roots set <folder>…")
			}
			for _, root := range r.Roots {
				fmt.Println(root)
			}
			for _, p := range r.Outside {
				fmt.Printf("! project %s is outside the roots: it stays usable (aided project remove <name> to stop using it)\n", p)
			}
			return nil
		},
	}
	roots.AddCommand(&cobra.Command{
		Use:     "set <folder>…",
		Short:   "Set the workspace roots (replacing those set before)",
		Example: "  aided roots set ~/code /srv/deploy",
		Args:    cobra.MinimumNArgs(1),
		RunE:    func(cmd *cobra.Command, args []string) error { return set(cmd, args) },
	}, &cobra.Command{
		Use:   "clear",
		Short: "Remove the workspace roots: any folder again",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return set(cmd, nil) },
	})
	return roots
}
