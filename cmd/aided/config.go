package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"

	"github.com/samir2015dzh/aide/internal/brandenv"
	"github.com/samir2015dzh/aide/internal/machine"
)

// configHeader opens an exported file.
const configHeader = `# This machine's Aide configuration (aided config export).
# Restore it, here or on another machine: aided config apply -f <this file>
# No secret is in it: a custom agent's secret variable is named, not valued.
`

// configCommand exports this machine's configuration as YAML and applies
// one (docs/machine-ops.md §6), from the machine's own shell, as
// `aided project` and `aided roots`: through the running service's control
// socket, or directly when it isn't running. Its JSON output follows
// docs/cli-output.md.
func configCommand(dataDir *string, openMachine func() (*machine.Machine, error)) *cobra.Command {
	machineDir := func() string { return filepath.Join(*dataDir, "machine") }
	var output string
	config := &cobra.Command{Use: "config", Short: "This machine's configuration as a file: projects, workspace roots, agent defaults, custom agents"}
	config.PersistentFlags().StringVarP(&output, "output", "o", cmpOr(brandenv.Get("OUTPUT"), "text"), "text or json")
	checkOutput := func() error {
		if output != "text" && output != "json" {
			return fmt.Errorf("-o is text or json, not %q", output)
		}
		return nil
	}

	export := &cobra.Command{
		Use:   "export",
		Short: "Print this machine's configuration as YAML, for a version control repository (no secrets)",
		Example: `  aided config export > aide-machine.yaml
  aided config apply -f aide-machine.yaml --dry-run`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := checkOutput(); err != nil {
				return err
			}
			var c machine.ConfigFile
			if out, ok, err := machine.ControlRequest(cmd.Context(), machineDir(), "GET", "/config", nil); ok {
				if err == nil {
					err = json.Unmarshal(out, &c)
				}
				if err != nil {
					return err
				}
			} else {
				n, err := openMachine()
				if err != nil {
					return err
				}
				c, err = n.ExportConfig()
				n.Close()
				if err != nil {
					return err
				}
			}
			if output == "json" {
				writeConfigDoc(cmd.OutOrStdout(), nil, "config", c)
				return nil
			}
			b, err := yaml.Marshal(c)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), configHeader+string(b))
			return nil
		},
	}

	var file string
	var dryRun bool
	apply := &cobra.Command{
		Use:   "apply -f <file>",
		Short: "Make this machine's projects and workspace roots what the file says (again and again: the same result)",
		Long: `Make this machine's projects and workspace roots what the file says: a
project added or updated, the roots set. Projects not in the file are kept;
a section left out is left as it is. A file with a wrong value, or a project
id at another path here, changes nothing. Agent defaults and custom agents
are set from the owner's devices only: those that differ are named, and the
command fails with kind denied after applying the rest.`,
		Example: `  aided config apply -f aide-machine.yaml --dry-run
  aided config apply -f aide-machine.yaml`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := checkOutput(); err != nil {
				return err
			}
			res := applyConfig(cmd, machineDir(), openMachine, file, dryRun)
			out := cmd.OutOrStdout()
			code := configExitCode(res.Error)
			if output == "json" {
				writeConfigDoc(out, res.Error, "config_apply", struct {
					DryRun  bool                   `json:"dry_run"`
					Changes []machine.ConfigChange `json:"changes"`
				}{res.DryRun, res.Changes})
			} else {
				for _, ch := range res.Changes {
					fmt.Fprintln(out, changeLine(ch, res.DryRun))
				}
				switch {
				case res.Error != nil:
				case len(res.Changes) == 0:
					fmt.Fprintln(out, "✓ nothing to change")
				case res.DryRun:
					fmt.Fprintln(out, "dry run: nothing was changed")
				default:
					fmt.Fprintln(out, "✓ applied")
				}
				if res.Error != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Error: %s\n", res.Error.Message)
					if res.Error.Hint != "" {
						fmt.Fprintf(cmd.ErrOrStderr(), "Hint: %s\n", res.Error.Hint)
					}
				}
			}
			if code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}
	apply.Flags().StringVarP(&file, "file", "f", "", "the YAML file (- for stdin)")
	apply.Flags().BoolVar(&dryRun, "dry-run", false, "show what would change, and change nothing")
	apply.MarkFlagRequired("file")
	config.AddCommand(export, apply)
	return config
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// applyConfig reads the file and has the machine apply it.
func applyConfig(cmd *cobra.Command, machineDir string, openMachine func() (*machine.Machine, error), file string, dryRun bool) machine.ConfigResult {
	failed := func(kind, hint string, err error) machine.ConfigResult {
		return machine.ConfigResult{DryRun: dryRun, Changes: []machine.ConfigChange{}, Error: &machine.ConfigError{Kind: kind, Message: err.Error(), Hint: hint}}
	}
	var b []byte
	var err error
	if file == "-" {
		b, err = io.ReadAll(cmd.InOrStdin())
	} else {
		b, err = os.ReadFile(file)
	}
	if err != nil {
		return failed("not_found", "Give the file exported with aided config export", err)
	}
	var c machine.ConfigFile
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
		return failed("usage", "Check the file against aided config export's", fmt.Errorf("%s: %w", file, err))
	}
	body, _ := json.Marshal(c)
	path := "/config"
	if dryRun {
		path += "?dry_run=1"
	}
	if out, ok, err := machine.ControlRequest(cmd.Context(), machineDir, "POST", path, body); ok {
		var res machine.ConfigResult
		if err == nil {
			err = json.Unmarshal(out, &res)
		}
		if err != nil {
			return failed("internal", "", err)
		}
		return res
	}
	n, err := openMachine()
	if err != nil {
		return failed("internal", "", err)
	}
	defer n.Close()
	if !n.Paired() {
		return failed("not_paired", "Log this machine in first: aided login <server>", errors.New("this machine is not logged in yet"))
	}
	return n.ApplyConfig(cmd.Context(), c, dryRun)
}

func changeLine(ch machine.ConfigChange, dryRun bool) string {
	what := ch.Section
	if ch.Name != "" {
		what += " " + ch.Name
	}
	switch {
	case ch.OwnerOnly:
		return "! " + what + " (" + ch.Action + "): set from the owner's devices, not here"
	case ch.Applied:
		return "✓ " + what + " (" + ch.Action + ")"
	case dryRun:
		return "+ " + what + " (would " + ch.Action + ")"
	}
	return "✗ " + what + " (" + ch.Action + ": not applied)"
}

// configExitCode is the exit code of a kind (docs/cli-output.md §2).
func configExitCode(e *machine.ConfigError) int {
	if e == nil {
		return 0
	}
	switch e.Kind {
	case "usage", "not_found", "invalid_state":
		return 2
	case "not_paired", "denied", "outside_roots":
		return 3
	}
	return 1
}

// writeConfigDoc prints the one JSON document of docs/cli-output.md §2.
func writeConfigDoc(w io.Writer, e *machine.ConfigError, key string, data any) {
	type errorJSON struct {
		Kind      string `json:"kind"`
		Message   string `json:"message"`
		Hint      string `json:"hint"`
		Retryable bool   `json:"retryable"`
	}
	var ej *errorJSON
	if e != nil {
		ej = &errorJSON{Kind: e.Kind, Message: e.Message, Hint: e.Hint}
	}
	enc := func(v any) string {
		var b bytes.Buffer
		e := json.NewEncoder(&b)
		e.SetEscapeHTML(false)
		e.Encode(v)
		return string(bytes.TrimRight(b.Bytes(), "\n"))
	}
	k, _ := json.Marshal(key)
	doc := fmt.Sprintf(`{"ok":%v,"exit_code":%d,"error":%s,%s:%s}`, e == nil, configExitCode(e), enc(ej), k, enc(data))
	var out bytes.Buffer
	json.Indent(&out, []byte(doc), "", "  ")
	fmt.Fprintln(w, out.String())
}
