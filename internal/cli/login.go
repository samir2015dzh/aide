package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	relayv1 "github.com/samir2015dzh/aide/gen/go/aide/relay/v1"
	"github.com/samir2015dzh/aide/internal/brandenv"
	"github.com/samir2015dzh/aide/internal/connaddr"
	"github.com/samir2015dzh/aide/internal/machine"
	"github.com/samir2015dzh/aide/pkg/identity"
)

// accountLogin logs this agent in to the owner's server with the account
// and password (docs/accounts.md): the server signs its grant itself, to
// every machine, project and coding agent, its own tasks, until revoked.
func (a *app) accountLogin(cmd *cobra.Command, arg, account, name string, fromStdin bool) error {
	ctx := cmd.Context()
	server := connaddr.Server(arg)
	if server == "" {
		return fail(exitUsage, "%v", connaddr.ErrNoServer)
	}
	relayURL, _, err := connaddr.ParseAny(server)
	if err != nil {
		return fail(exitUsage, "%v", err)
	}
	if len(name) > maxLabel {
		return fail(exitUsage, "--name is %d bytes long; at most %d", len(name), maxLabel)
	}
	progress := cmd.OutOrStdout()
	if a.output == "json" {
		progress = cmd.ErrOrStderr()
	}
	done, previous, err := a.alreadyPaired(ctx, progress, relayURL, "")
	if err != nil {
		return err
	}
	if done {
		return a.loggedIn(cmd)
	}
	if account == "" {
		account = brandenv.Get("ACCOUNT")
	}
	if account == "" {
		if account, err = ask(cmd, "Account: "); err != nil {
			return err
		}
	}
	password, err := readPassword(cmd, fromStdin)
	if err != nil {
		return err
	}
	if err := a.loadKey(); err != nil {
		return err
	}
	host, _ := os.Hostname()
	if name == "" {
		name = firstNonEmpty(previous, machine.HostLabel("client@"))
	}
	res, err := machine.LogInTo(ctx, a.hc, relayURL, account, password, &relayv1.Joiner{Kind: relayv1.JoinKind_JOIN_KIND_AGENT,
		PublicKey: identity.MarshalPublicKey(a.key.Public()), Label: name, Hostname: host, Platform: runtime.GOOS + "/" + runtime.GOARCH})
	if err != nil {
		return fail(exitUnavailable, "logging in: %v", err)
	}
	a.st = state{RelayURL: relayURL, OwnerID: res.OwnerId}
	a.trust = identity.NewTrust(res.OwnerId)
	if _, err := a.trust.AcceptRootChain(res.RootChain); err != nil {
		return fmt.Errorf("owner root chain: %w", err)
	}
	if err := a.adopt(res.RootChain, res.Credential); err != nil {
		return err
	}
	// Its inbox, so that news and messages can reach it from now on.
	if c, err := a.dial(ctx, false); err == nil {
		if err := a.registerInbox(ctx, c); err != nil {
			fmt.Fprintln(os.Stderr, "warning: registering this agent's inbox:", err)
		}
		c.Close()
	}
	fmt.Fprintf(progress, "Logged in to %s as %q. Run `aide project list` to see where it can work.\n", res.Account, name)
	return a.loggedIn(cmd)
}

// ask reads one line from the terminal.
func ask(cmd *cobra.Command, prompt string) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fail(exitUsage, "no terminal to ask for it: use --account or $AIDE_ACCOUNT")
	}
	fmt.Fprint(cmd.ErrOrStderr(), prompt)
	flush(cmd.ErrOrStderr())
	l, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && l == "" {
		return "", err
	}
	return strings.TrimSpace(l), nil
}

// readPassword takes the password from stdin (--password-stdin), else
// $AIDE_PASSWORD, else asks on the terminal without echo.
func readPassword(cmd *cobra.Command, fromStdin bool) (string, error) {
	if fromStdin {
		l, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
		if err != nil && l == "" {
			return "", fail(exitUsage, "reading the password from stdin: %v", err)
		}
		return strings.TrimRight(l, "\r\n"), nil
	}
	if p := brandenv.Get("PASSWORD"); p != "" {
		return p, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fail(exitUsage, "no terminal to ask for the password: use --password-stdin or $AIDE_PASSWORD")
	}
	fmt.Fprint(cmd.ErrOrStderr(), "Password: ")
	flush(cmd.ErrOrStderr())
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(cmd.ErrOrStderr())
	if err != nil {
		return "", err
	}
	if len(b) == 0 {
		return "", errors.New("no password given")
	}
	return string(b), nil
}
