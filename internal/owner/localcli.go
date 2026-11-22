package owner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
	relayv1 "github.com/samir2015dzh/aide/gen/go/aide/relay/v1"
	"github.com/samir2015dzh/aide/internal/brandenv"
	"github.com/samir2015dzh/aide/pkg/identity"
)

// This computer's `aide` CLI, logged in through this device (work order
// #39, docs/accounts.md §3.1). The CLI keeps its own key: it asks over the
// local socket (this user's alone) for a grant for it, and this device,
// logged in as the owner, has the server sign and publish one, as a
// password login would give an agent. Nothing about the account or
// password reaches the CLI, and nothing crosses the network but the grant
// to sign. Whoever can open the socket already acts as the owner (it can
// grant itself as much through UpdateClient), so this adds no power.

// IssueLocalClient signs and publishes an agent grant for pub, named
// label: every machine, project and coding agent, its own tasks, until
// revoked (as a password login's, pkg/relay DefaultGrant).
func (o *Owner) IssueLocalClient(ctx context.Context, pub []byte, label string) (*localv1.IssueLocalClientGrantResponse, error) {
	if err := o.requireInit(); err != nil {
		return nil, err
	}
	if _, err := identity.ParsePublicKey(pub); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("not a public key"))
	}
	label = strings.Join(strings.FieldsFunc(label, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")
	if label == "" || len(label) > 64 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("a name is 1 to 64 bytes"))
	}
	now := time.Now()
	o.mu.Lock()
	ownerID, relayURL, account := o.ownerID, o.relayURL, o.account
	o.mu.Unlock()
	g := &identityv1.Grant{OwnerId: ownerID, GrantId: "g_" + strings.ToLower(identity.RandomCode(26)), Version: 1,
		ClientId: identity.ClientID(pub), ClientPublicKey: pub, ClientLabel: label,
		MachineIds: []string{"*"}, ProjectIds: []string{"*"}, Agents: []string{"*"},
		TaskVisibility: identityv1.TaskVisibility_TASK_VISIBILITY_OWN,
		NotBefore:      timestamppb.New(now.Add(-time.Minute)), ExpiresAt: timestamppb.New(identity.NoExpiry),
		FirstIssuedAt: timestamppb.New(now)}
	signed, err := o.signRoot(ctx, g)
	if err != nil {
		return nil, err
	}
	ctl, _, err := o.control(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := ctl.PublishGrant(ctx, &relayv1.PublishGrantRequest{Grant: signed[0]}); err != nil {
		return nil, err
	}
	o.st.putGrant(signed[0], false)
	go o.refreshCards()
	o.log.Info("this computer's CLI logged in through the app", "client", g.ClientId, "grant", g.GrantId, "label", label)
	return &localv1.IssueLocalClientGrantResponse{RelayUrl: relayURL, OwnerId: ownerID, Account: account, RootChain: o.trust.RootChain(), Grant: signed[0]}, nil
}

// cliProgram is the `aide` CLI installed with this aided (beside it, as
// the app and the installers put them); "" when there is none.
func cliProgram() string {
	if b := brandenv.Get("CLI_BIN"); b != "" {
		return b
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	name := "aide"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	p := filepath.Join(filepath.Dir(exe), name)
	if st, err := os.Stat(p); err != nil || st.IsDir() {
		return ""
	}
	return p
}

// runCLI runs the CLI with args and -o json, pointed at this device's
// local socket, and decodes its document into v. A non-zero exit is not an
// error by itself: the document says what happened.
func (d *Device) runCLI(ctx context.Context, v any, args ...string) error {
	bin := cliProgram()
	if bin == "" {
		return errors.New("no aide CLI beside aided")
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, append(args, "-o", "json")...)
	cmd.Env = append(os.Environ(), brandenv.Both("APP_SOCKET", d.sock)...)
	cmd.Stdin = nil
	out, err := cmd.Output()
	if len(out) == 0 && err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return errors.New(strings.TrimSpace(string(ee.Stderr)))
		}
		return err
	}
	return json.Unmarshal(out, v)
}

// cliDoc is the part of the CLI's documents read here (docs/cli-output.md).
type cliDoc struct {
	OK    bool `json:"ok"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
	Auth *struct {
		Identity *struct {
			Client   string `json:"client"`
			ClientID string `json:"client_id"`
			OwnerID  string `json:"owner_id"`
		} `json:"identity"`
		Home string `json:"home"`
	} `json:"auth"`
}

// LocalCLI is this computer's CLI as `aide auth status` reports it.
func (d *Device) LocalCLI(ctx context.Context) *localv1.LocalCLI {
	out := &localv1.LocalCLI{Found: cliProgram() != ""}
	if !out.Found {
		return out
	}
	var doc cliDoc
	if err := d.runCLI(ctx, &doc, "auth", "status"); err != nil {
		out.Error = err.Error()
		return out
	}
	if a := doc.Auth; a != nil {
		out.Home = a.Home
		if id := a.Identity; id != nil && id.ClientID != "" {
			out.LoggedIn, out.Label, out.ClientId = true, id.Client, id.ClientID
			if o := d.owner(); o != nil {
				o.mu.Lock()
				ownerID := o.ownerID
				o.mu.Unlock()
				out.SameOwner = ownerID != "" && id.OwnerID == ownerID
				if out.SameOwner {
					// Its name and grant as this device lists the owner's agents.
					if list, err := o.ListClients(ctx); err == nil {
						for _, c := range list {
							if c.ClientId == id.ClientID && !c.Revoked {
								out.GrantId = c.GrantId
								if c.Label != "" {
									out.Label = c.Label
								}
							}
						}
					}
				}
			}
		}
	}
	return out
}

// LogInLocalCLI has the CLI log in through this device (aide auth login
// --from-app), and reports it after.
func (d *Device) LogInLocalCLI(ctx context.Context, label string) (*localv1.LocalCLI, error) {
	o := d.owner()
	if o == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("Aide is not set up yet"))
	}
	if err := o.requireInit(); err != nil {
		return nil, err
	}
	if cliProgram() == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("the aide CLI is not installed beside the app"))
	}
	args := []string{"auth", "login", "--from-app"}
	if label != "" {
		args = append(args, "--name", label)
	}
	var doc cliDoc
	if err := d.runCLI(ctx, &doc, args...); err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, err)
	}
	if !doc.OK && doc.Error != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New(doc.Error.Message))
	}
	return d.LocalCLI(ctx), nil
}

// LogOutLocalCLI logs the CLI out: its grant revoked (aide auth logout,
// which asks this device to revoke it), its key and grant forgotten.
func (d *Device) LogOutLocalCLI(ctx context.Context) (*localv1.LocalCLI, error) {
	if cliProgram() == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("the aide CLI is not installed beside the app"))
	}
	var doc struct {
		OK     bool `json:"ok"`
		Logout *struct {
			Revoked bool `json:"revoked"`
		} `json:"logout"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := d.runCLI(ctx, &doc, "auth", "logout"); err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, err)
	}
	if !doc.OK && doc.Error != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New(doc.Error.Message))
	}
	return d.LocalCLI(ctx), nil
}
