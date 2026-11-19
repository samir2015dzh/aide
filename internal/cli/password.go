package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/hermeshai/hermesh/gen/go/hermesh/identity/v1"
	relayv1 "github.com/hermeshai/hermesh/gen/go/hermesh/relay/v1"
	"github.com/hermeshai/hermesh/gen/go/hermesh/relay/v1/relayv1connect"
	"github.com/hermeshai/hermesh/internal/brandenv"
	"github.com/hermeshai/hermesh/internal/connaddr"
	"github.com/hermeshai/hermesh/internal/machine"
	"github.com/hermeshai/hermesh/internal/passlogin"
	"github.com/hermeshai/hermesh/pkg/identity"
)

// passwordLogin pairs this agent with the owner whose account and password
// it is given (docs/password-login.md): it submits its pairing request as
// any login does, and completes it itself with a grant signed by the
// owner's password key, the one an approval with the defaults would give.
func (a *app) passwordLogin(cmd *cobra.Command, arg, account, name string, fromStdin bool) error {
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
	password, err := readPassword(cmd, fromStdin)
	if err != nil {
		return err
	}
	pub := relayv1connect.NewRelayPublicServiceClient(a.hc, relayURL)
	sess, err := passlogin.Login(ctx, pub, account, password)
	if err != nil {
		return fail(exitUnavailable, "logging in: %v", err)
	}
	if err := a.loadKey(); err != nil {
		return err
	}
	host, _ := os.Hostname()
	if name == "" {
		name = firstNonEmpty(previous, machine.HostLabel("client@"))
	}
	spki := identity.MarshalPublicKey(a.key.Public())
	pr := &identityv1.PairingRequest{Kind: identityv1.PairingKind_PAIRING_KIND_CLIENT, SubjectPublicKey: spki,
		Label: name, Hostname: host, Platform: runtime.GOOS + "/" + runtime.GOARCH, OwnerId: sess.OwnerID, CreatedAt: timestamppb.Now()}
	req, err := identity.Sign(pr, a.key)
	if err != nil {
		return err
	}
	sub, err := pub.SubmitPairing(ctx, &relayv1.SubmitPairingRequest{Request: req})
	if err != nil {
		return fail(exitUnavailable, "submit pairing request: %v", err)
	}
	g := &identityv1.Grant{OwnerId: sess.OwnerID, GrantId: "g_" + strings.ToLower(ulid.Make().String()), Version: 1,
		ClientId: identity.ClientID(spki), ClientPublicKey: spki, ClientLabel: name,
		MachineIds: []string{"*"}, ProjectIds: []string{"*"}, Agents: []string{"*"}, TaskVisibility: identityv1.TaskVisibility_TASK_VISIBILITY_OWN,
		NotBefore: timestamppb.New(time.Now().Add(-time.Minute)), ExpiresAt: timestamppb.New(identity.NoExpiry)}
	signed, err := sess.Sign(g)
	if err != nil {
		return err
	}
	if err := sess.Complete(ctx, sub.Code, signed[0]); err != nil {
		return fail(exitUnavailable, "logging in: %v", err)
	}
	a.st = state{RelayURL: relayURL, OwnerID: sess.OwnerID}
	a.trust = sess.Trust
	if err := a.adopt(sess.Trust.RootChain(), signed[0]); err != nil {
		return err
	}
	if c, err := a.dial(ctx, false); err == nil {
		if err := a.registerInbox(ctx, c); err != nil {
			fmt.Fprintln(os.Stderr, "warning: registering this agent's inbox:", err)
		}
		c.Close()
	}
	fmt.Fprintf(progress, "Logged in to %s as %q. Run `aide project list` to see where it can work.\n", sess.Account, name)
	return a.loggedIn(cmd)
}

// readPassword takes the password from stdin (--password-stdin), else
// $HERMESH_PASSWORD, else asks on the terminal without echo.
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
