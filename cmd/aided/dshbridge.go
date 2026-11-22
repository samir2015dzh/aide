package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/samir2015dzh/aide/internal/provider/acp/dshnative"
	"github.com/samir2015dzh/aide/internal/shellenv"
)

// dshBridgeCommand is `aided dsh-bridge`: the Aide plugin in DSH
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
installed packages) into <DSH home>/aide-bridge/backups/<time>, and
writes the plugin's package into <DSH home>/aide-bridge. It changes
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
	// The report DSH makes after installing the plugin for the owner (工单 #59): aided
	// reads it, checks the plugin again and shows the outcome on the Connect page.
	var ok, failed bool
	var reason string
	report := &cobra.Command{
		Use:   "report",
		Short: "Tell Aide how installing the plugin went: aided checks it again and shows it on the Connect page",
		Long: `For DSH, at the end of the install Aide handed to it:

  aided dsh-bridge report --ok                        installed, and status says the plugin runs
  aided dsh-bridge report --ok --reason "<what is left>"   installed, not in effect yet
  aided dsh-bridge report --failed --reason "<why>"      not installed: the real cause, in words for the owner

The reason is shown as written on the Connect page.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if ok == failed {
				return errors.New("say --ok or --failed")
			}
			h, err := home()
			if err != nil {
				return err
			}
			if err := dshnative.Report(h, ok, reason, time.Now()); err != nil {
				return err
			}
			fmt.Println("✓ Reported to Aide; it checks the plugin again within a few seconds.")
			return nil
		},
	}
	report.Flags().BoolVar(&ok, "ok", false, "the plugin is installed")
	report.Flags().BoolVar(&failed, "failed", false, "the plugin could not be installed")
	report.Flags().StringVar(&reason, "reason", "", "why it failed, or what is left to do, in words for the owner")
	cmd.AddCommand(report)
	return cmd
}
