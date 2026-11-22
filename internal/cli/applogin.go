package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
	"github.com/samir2015dzh/aide/gen/go/aide/local/v1/localv1connect"
	"github.com/samir2015dzh/aide/internal/brandenv"
	"github.com/samir2015dzh/aide/internal/localipc"
	"github.com/samir2015dzh/aide/internal/machine"
	"github.com/samir2015dzh/aide/internal/paths"
	"github.com/samir2015dzh/aide/pkg/identity"
)

// Logging in through the Aide app on this computer (work order #39,
// docs/accounts.md §3.1): the app is logged in already, so this agent asks
// it, over its local socket (this user's alone), for a grant for its own
// key; no account or password again. It is still an agent of its own, with
// its own key, revocable on its own.

// appSocket is the app's local API on this computer: $AIDE_APP_SOCKET (set
// when the app runs this command), else where the app's aided serves it.
func appSocket() string {
	if s := brandenv.Get("APP_SOCKET"); s != "" {
		return s
	}
	return paths.LocalAPI(paths.DataDir())
}

// appClient is a client of the app's local API, or nil when nothing
// answers there.
func appClient(ctx context.Context) localv1connect.LocalServiceClient {
	sock := appSocket()
	if _, err := os.Stat(sock); err != nil {
		return nil
	}
	hc := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return localipc.Dial(ctx, sock)
	}}}
	return localv1connect.NewLocalServiceClient(hc, "http://local")
}

// appLoggedIn is the app's status when it answers and is logged in to an
// owner; nil otherwise.
func appLoggedIn(ctx context.Context) (localv1connect.LocalServiceClient, *localv1.GetStatusResponse) {
	c := appClient(ctx)
	if c == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	st, err := c.GetStatus(ctx, &localv1.GetStatusRequest{})
	if err != nil || st.OwnerId == "" || st.RelayUrl == "" || st.SignedOut || st.Removed {
		return nil, nil
	}
	return c, st
}

// appLogin logs this agent in through the app (--from-app, or login with
// no server while the app here is logged in).
func (a *app) appLogin(cmd *cobra.Command, name string) error {
	ctx := cmd.Context()
	c, st := appLoggedIn(ctx)
	if c == nil {
		return &exitError{code: exitUnavailable, kind: "unavailable", err: errors.New("the Aide app on this computer is not running, or not logged in"),
			hint: "open the Aide app and log in, or log in with the account: `aide auth login <your server> --account <account>`"}
	}
	if len(name) > maxLabel {
		return fail(exitUsage, "--name is %d bytes long; at most %d", len(name), maxLabel)
	}
	progress := cmd.OutOrStdout()
	if a.output == "json" {
		progress = cmd.ErrOrStderr()
	}
	done, previous, err := a.alreadyPaired(ctx, progress, st.RelayUrl, st.OwnerId)
	if err != nil {
		return err
	}
	if done {
		return a.loggedIn(cmd)
	}
	if err := a.loadKey(); err != nil {
		return err
	}
	if name == "" {
		name = firstNonEmpty(previous, machine.HostLabel("client@"))
	}
	res, err := c.IssueLocalClientGrant(ctx, &localv1.IssueLocalClientGrantRequest{PublicKey: identity.MarshalPublicKey(a.key.Public()), Label: name})
	if err != nil {
		return fail(exitUnavailable, "logging in through the Aide app: %v", connectMessage(err))
	}
	a.st = state{RelayURL: res.RelayUrl, OwnerID: res.OwnerId}
	a.trust = identity.NewTrust(res.OwnerId)
	if _, err := a.trust.AcceptRootChain(res.RootChain); err != nil {
		return fmt.Errorf("owner root chain: %w", err)
	}
	if err := a.adopt(res.RootChain, res.Grant); err != nil {
		return err
	}
	if rc, err := a.dial(ctx, false); err == nil {
		if err := a.registerInbox(ctx, rc); err != nil {
			fmt.Fprintln(os.Stderr, "warning: registering this agent's inbox:", err)
		}
		rc.Close()
	}
	fmt.Fprintf(progress, "Logged in to %s through the Aide app on this computer, as %q: no password needed. Run `aide project list` to see where it can work.\n", res.Account, name)
	return a.loggedIn(cmd)
}

// revokeThroughApp has the app here revoke this agent's grant, when the
// app answers and is the same owner's: logging out ends the agent, not
// only its files here. It says whether it did.
func (a *app) revokeThroughApp(ctx context.Context) (bool, error) {
	c, st := appLoggedIn(ctx)
	if c == nil || a.g == nil || st.OwnerId != a.st.OwnerID {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := c.RevokeClient(ctx, &localv1.RevokeClientRequest{GrantId: a.g.GrantId}); err != nil {
		return false, connectMessage(err)
	}
	return true, nil
}

func connectMessage(err error) error {
	if ce := (*connect.Error)(nil); errors.As(err, &ce) {
		return errors.New(ce.Message())
	}
	return err
}
