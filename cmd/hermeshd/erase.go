package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	localv1 "github.com/hermeshai/hermesh/gen/go/hermesh/local/v1"
	"github.com/hermeshai/hermesh/internal/machine"
	"github.com/hermeshai/hermesh/internal/service"
)

// Erasing (docs/erase-everything.md): `hermeshd erase` erases this
// computer's machine only; Erase Everything is in the apps.

// eraseCommand is `hermeshd erase`.
func eraseCommand(dataDir *string) *cobra.Command {
	var yes, eraseHer bool
	cmd := &cobra.Command{
		Use:   "erase",
		Short: "Erase Aide on this computer only: its machine, logged out, with its projects list, sessions and the worktrees Aide made",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var ask func(string) (string, error)
			if !yes {
				t, err := newTerminal()
				if err != nil {
					return fmt.Errorf("%w (or pass --yes)", err)
				}
				ask = t.line
			}
			return erase(cmd.Context(), os.Stdout, ask, *dataDir, eraseHer)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "don't ask")
	cmd.Flags().BoolVar(&eraseHer, "erase-her", false, "delete Her's data too, without asking")
	return cmd
}

// erase erases this computer's machine, in dataDir. Her's data, where Her
// was on, goes with eraseHer, or when ask is answered so; else it stays.
// ask, when not nil, asks first.
func erase(ctx context.Context, w io.Writer, ask func(string) (string, error), dataDir string, eraseHer bool) error {
	cfg := machine.Config{DataDir: filepath.Join(dataDir, "machine")}
	if _, err := os.Stat(machine.KeyPath(cfg.DataDir)); err != nil {
		if _, err := os.Stat(filepath.Join(dataDir, "owner")); err == nil {
			return errors.New("this computer is one of your devices, not a machine only: erase Aide on all your devices in the app (Settings › Account)")
		}
		fmt.Fprintln(w, "Aide keeps no machine here: nothing to erase.")
		return nil
	}
	svc := service.InstalledFor(dataDir)
	n, err := machine.Open(cfg)
	if err != nil && !svc {
		return fmt.Errorf("can't open this computer's machine (stop `aided run` here first): %w", err)
	}
	what := "This computer's machine"
	if n != nil {
		if n.Paired() {
			what = fmt.Sprintf("This computer's machine “%s” (owner %s)", n.Name(), n.OwnerID())
		}
		n.Close()
	}
	herHome, herData, _ := machine.HerData(cfg)
	eraseHer = eraseHer && herData
	askHer := herData && !eraseHer && ask != nil
	fmt.Fprintf(w, "%s is logged out; its projects list, sessions and the worktrees Aide made are erased.\n", what)
	if eraseHer {
		fmt.Fprintf(w, "Her's assistants, conversations and settings, in %s, are deleted too.\n", herHome)
	}
	fmt.Fprintln(w, "Stays: your project directories and repositories (branches included), and worktrees with uncommitted changes.")
	if herData && !eraseHer && !askHer {
		fmt.Fprintf(w, herStays, herHome)
	}
	if ask != nil {
		if a, err := ask("Erase? [y/N] "); err != nil || !strings.EqualFold(a, "y") {
			return errors.New("not erased")
		}
	}
	if askHer {
		a, err := ask(fmt.Sprintf(herAsk, herHome))
		if err != nil {
			return errors.New("not erased")
		}
		if eraseHer = strings.EqualFold(a, "y"); !eraseHer {
			fmt.Fprintf(w, herStays, herHome)
		}
	}
	// The service would run it again at once.
	if svc {
		if err := service.Uninstall(); err != nil {
			return fmt.Errorf("stopping the background service: %w", err)
		}
	}
	// Its owner's devices stop listing it, when the server can be reached.
	if n, err := openWhenStopped(cfg); err == nil {
		if n.Paired() {
			ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			if err := n.LeaveOwner(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "warning: the server didn't remove it from its owner (%v); remove it there\n", err)
			}
			cancel()
		}
		n.Close()
	}
	if eraseHer {
		eraseHerData(cfg, herHome)
	}
	kept, err := eraseHere(dataDir, cfg)
	if err != nil {
		return err
	}
	fmt.Fprintln(w, "✓ Aide on this computer is erased. Log it in again with `aided login <server>`.")
	for _, wt := range kept {
		fmt.Fprintf(w, "  Kept, with uncommitted changes: %s\n", wt)
	}
	return nil
}

// openWhenStopped opens the machine once the stopped service has let go of
// it: on Windows its daemon ends a moment after its task does.
func openWhenStopped(cfg machine.Config) (*machine.Machine, error) {
	for deadline := time.Now().Add(10 * time.Second); ; {
		n, err := machine.Open(cfg)
		if err == nil || time.Now().After(deadline) {
			return n, err
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// eraseHere erases the machine in cfg.DataDir (stopped), keeping worktrees
// with uncommitted changes in dataDir/kept-worktrees, and returns them.
func eraseHere(dataDir string, cfg machine.Config) ([]string, error) {
	kept, err := machine.Erase(cfg, filepath.Join(dataDir, "kept-worktrees"))
	if err != nil {
		return kept, fmt.Errorf("erasing this machine: %w", err)
	}
	os.Remove(cfg.DataDir) // empty, unless a worktree could not be moved out
	return kept, nil
}

// printErasePreview says what erasing everything reaches, by name, and
// what stays, as the apps' sheet does.
func printErasePreview(items []*localv1.EraseItem, host string) {
	group := func(kind, title, does, later string) {
		var names []string
		for _, it := range items {
			if it.Kind != kind {
				continue
			}
			switch {
			case it.ThisDevice:
				names = append(names, it.Name+" (this computer)")
			case it.Online:
				names = append(names, it.Name)
			default:
				names = append(names, it.Name+" ("+later+")")
			}
		}
		if len(names) > 0 {
			fmt.Printf("  %s: %s\n    %s\n", title, strings.Join(names, ", "), does)
		}
	}
	fmt.Println("Erase everything in Aide? Aide on all your devices goes back to how it was when first installed.")
	fmt.Println("\nErases:")
	group("device", "Devices", "Signed out; their keys, sessions and settings erased", "offline: offers a reset when next opened")
	group("machine", "Machines", "Logged out; their projects lists, sessions and the worktrees Aide made erased", "offline: to log in again when next online")
	group("agent", "Agents", "Disconnected; their login on their own computers erased", "logs in again when next used")
	fmt.Printf("  Your server · %s\n    Not set up any more, with a new setup code\n", host)
	stays := []string{"Your project directories and repositories, branches included", "Worktrees with uncommitted changes: moved aside and kept, and you're told where",
		"What offline devices hold, until each is reset"}
	fmt.Println("Stays:")
	for _, s := range stays {
		fmt.Printf("  %s\n", s)
	}
}
