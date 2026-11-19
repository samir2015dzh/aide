package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/hermeshai/hermesh/internal/provider/acp/dshnative"
	"github.com/hermeshai/hermesh/internal/shellenv"
)

// dshBridgeCommand is `hermeshd dsh-bridge`: the Hermesh plugin in DSH
// Desktop that taking over a DSH session goes through (docs/dsh-takeover.md).
func dshBridgeCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "dsh-bridge", Short: "The Aide plugin in DSH Desktop, through which a DSH session you take over gets your messages"}
	home := func() (string, error) { return dshnative.Home(shellenv.Environ()) }
	cmd.AddCommand(&cobra.Command{
		Use: "status", Short: "Whether the plugin runs in DSH Desktop here", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			h, err := home()
			if err != nil {
				return err
			}
			fmt.Printf("DSH home    %s\n", h)
			ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
			defer cancel()
			b, err := dshnative.DialBridge(ctx, h)
			if err != nil {
				fmt.Printf("plugin      not usable: %v\n", err)
				if errors.Is(err, dshnative.ErrBridgeNotInstalled) {
					fmt.Println("            install it: aided dsh-bridge prepare")
				}
				return nil
			}
			defer b.Close()
			fmt.Printf("plugin      runs, version %s (protocol %d)\n", b.Hello.Version, b.Hello.Protocol)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "prepare",
		Short: "Back up DSH's configuration and write the plugin's package, for DSH Desktop to install",
		Long: `Backs up DSH's configuration files (the DSH home's own and each
profile's: package.json, cordis.patch.yml and the like; not sessions, not
installed packages) into <DSH home>/hermesh-bridge/backups/<time>, and
writes the plugin's package into <DSH home>/hermesh-bridge. It changes
nothing DSH reads. Then install the package in DSH Desktop's Plugins page,
which checks it against your DSH, and turns it off or removes it there.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			h, err := home()
			if err != nil {
				return err
			}
			p, err := dshnative.PrepareBridge(h, time.Now())
			if err != nil {
				return err
			}
			fmt.Printf("✓ Backed up %d configuration files to %s\n", len(p.BackedUp), p.Backup)
			for _, f := range p.BackedUp {
				fmt.Printf("    %s\n", f)
			}
			if !p.DesktopProfile {
				fmt.Printf("⚠ DSH Desktop's profile is not at %s: open DSH Desktop once, or it is kept elsewhere and was not backed up\n",
					strings.TrimSuffix(h, "/")+"/profiles/desktop")
			}
			fmt.Printf("✓ The plugin's package (version %s): %s\n\n", p.Version, p.Package)
			fmt.Printf("Next, in DSH Desktop: Plugins › install from a file or path:\n  %s\nthen restart DSH Desktop. Turn it off or remove it there too.\n", p.Package)
			return nil
		},
	})
	return cmd
}
