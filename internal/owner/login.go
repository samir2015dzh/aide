package owner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	identityv1 "github.com/hermeshai/hermesh/gen/go/hermesh/identity/v1"
	localv1 "github.com/hermeshai/hermesh/gen/go/hermesh/local/v1"
	relayv1 "github.com/hermeshai/hermesh/gen/go/hermesh/relay/v1"
	"github.com/hermeshai/hermesh/internal/connaddr"
	"github.com/hermeshai/hermesh/internal/machine"
	"github.com/hermeshai/hermesh/pkg/identity"
)

// Logging in (docs/accounts.md): this device logs in to the owner's server
// with the account and password, and the server signs its device cert,
// and its own machine's cert if it runs one. Nobody else approves, nothing
// is compared, nothing is confirmed on this device. From then on the server
// signs for this device whatever adding a machine or an agent, or managing
// them, takes (signRoot).

// Setup sets a new server up with the setup code it printed, the account
// and the password it will keep, and logs this device in to it.
func (o *Owner) Setup(ctx context.Context, req *localv1.SetupRequest) (*localv1.SetupResponse, error) {
	relayURL, err := o.loginServer(ctx, req.RelayUrl, req.LocalMachineName)
	if err != nil {
		return nil, err
	}
	j, device, nk, err := o.joiner(req.Label, req.Platform, req.LocalMachineName)
	if err != nil {
		return nil, err
	}
	res, err := o.publicClient(relayURL).SetUp(ctx, &relayv1.SetUpRequest{SetupCode: req.SetupCode, Account: req.Account, Password: req.Password, Joiner: j})
	if err != nil {
		return nil, err
	}
	if err := o.loggedIn(ctx, relayURL, res, j, device, nk, req.LocalMachineName); err != nil {
		return nil, err
	}
	return &localv1.SetupResponse{OwnerId: res.OwnerId}, nil
}

// LogIn logs this device in to the owner's server: as one of the owner's
// devices, or, with machine_only, as a machine only.
func (o *Owner) LogIn(ctx context.Context, req *localv1.LogInRequest) (*localv1.LogInResponse, error) {
	if req.MachineOnly {
		return o.logInAsMachine(ctx, req)
	}
	relayURL, err := o.loginServer(ctx, req.RelayUrl, req.LocalMachineName)
	if err != nil {
		return nil, err
	}
	j, device, nk, err := o.joiner(req.Label, req.Platform, req.LocalMachineName)
	if err != nil {
		return nil, err
	}
	res, err := machine.LogInTo(ctx, o.cfg.HTTPClient, relayURL, req.Account, req.Password, j)
	if err != nil {
		return nil, err
	}
	if err := o.loggedIn(ctx, relayURL, res, j, device, nk, req.LocalMachineName); err != nil {
		return nil, err
	}
	return &localv1.LogInResponse{OwnerId: res.OwnerId}, nil
}

// serverOwner is the owner of the server typed, "" if it can't be told.
func (o *Owner) serverOwner(ctx context.Context, typed string) string {
	server := connaddr.Server(typed)
	if server == "" {
		return ""
	}
	info, err := o.publicClient(normalizeRelayURL(server)).GetInfo(ctx, &relayv1.GetInfoRequest{})
	if err != nil {
		return ""
	}
	return info.OwnerId
}

// loginServer checks that this device can log in, and to which server.
func (o *Owner) loginServer(ctx context.Context, typed, localMachine string) (string, error) {
	if o.Initialized() {
		return "", connect.NewError(connect.CodeAlreadyExists, errors.New("this device already belongs to an owner"))
	}
	if o.cfg.NoLocalMachine && localMachine != "" {
		return "", errNoLocalMachine
	}
	server := connaddr.Server(typed)
	if server == "" {
		return "", connect.NewError(connect.CodeInvalidArgument, errors.New("which server? type its address"))
	}
	relayURL := normalizeRelayURL(server)
	if err := o.checkRelay(ctx, relayURL); err != nil {
		return "", err
	}
	return relayURL, nil
}

// joiner is what this device logs in as: a new device key, and, for a
// device that runs a machine, that machine's key. A machine-only device
// that logs in as a device keeps its machine (and its key).
func (o *Owner) joiner(label, platform, localMachine string) (*relayv1.Joiner, *identity.SoftwareKey, *machine.Machine, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		label, _ = os.Hostname()
	}
	if platform == "" {
		platform = runtime.GOOS
	}
	device, err := identity.GenerateKey()
	if err != nil {
		return nil, nil, nil, err
	}
	j := &relayv1.Joiner{Kind: relayv1.JoinKind_JOIN_KIND_DEVICE, PublicKey: identity.MarshalPublicKey(device.Public()), Label: label, Platform: platform}
	if localMachine != "" {
		j.MachineName = machine.SanitizeName(localMachine)
	}
	j.Hostname, _ = os.Hostname()
	var n *machine.Machine
	if localMachine != "" || o.machineOnly() {
		o.mu.Lock()
		running := o.localMachine
		o.mu.Unlock()
		if running != nil {
			j.MachinePublicKey = running.PublicKeySPKI()
		} else {
			if n, err = o.openLocalMachine(); err != nil {
				return nil, nil, nil, err
			}
			j.MachinePublicKey = n.PublicKeySPKI()
		}
	}
	return j, device, n, nil
}

// loggedIn keeps what the server signed: this device's identity and, if it
// asked for one, its own machine, which it then starts.
func (o *Owner) loggedIn(ctx context.Context, relayURL string, res *relayv1.LogInResponse, j *relayv1.Joiner, device *identity.SoftwareKey,
	n *machine.Machine, localMachine string) error {
	defer func() {
		if n != nil {
			n.Close()
		}
	}()
	if err := machine.CheckLogIn(res, j); err != nil {
		return err
	}
	wasMachine := o.machineOnly()
	// The device's id among the owner's devices is its device key's.
	if err := o.saveIdentity(res.OwnerId, relayURL, j.PublicKey, res.RootChain, device, res.Credential, res.Account); err != nil {
		return err
	}
	if res.MachineCert == nil {
		return nil
	}
	if wasMachine {
		// It was a machine only: its machine, now the device's own.
		o.st.set(kMachineOnly, nil)
		return o.rebindLocalMachine(ctx, res.RootChain, res.MachineCert)
	}
	if n != nil {
		// Closed before the machine is installed, which opens it again.
		n.Close()
		n = nil
	}
	if res.MachineName != "" {
		localMachine = res.MachineName
	}
	return o.installLocalMachine(ctx, res.MachineCert, localMachine)
}

// logInAsMachine makes this computer a machine of the owner's only: it
// runs the owner's tasks, but is not one of the owner's devices.
func (o *Owner) logInAsMachine(ctx context.Context, req *localv1.LogInRequest) (*localv1.LogInResponse, error) {
	if o.cfg.NoLocalMachine {
		return nil, errNoLocalMachine
	}
	if o.machineOnly() {
		return nil, connect.NewError(connect.CodeAlreadyExists, errors.New("this computer is already a machine of an owner"))
	}
	relayURL, err := o.loginServer(ctx, req.RelayUrl, "")
	if err != nil {
		return nil, err
	}
	n, err := o.openLocalMachine()
	if err != nil {
		return nil, err
	}
	label := req.Label
	err = n.LogIn(ctx, relayURL, req.Account, req.Password, label, discard{})
	owner := n.OwnerID()
	n.Close()
	if err != nil {
		return nil, err
	}
	if err := o.st.set(kMachineOnly, []byte("1")); err != nil {
		return nil, err
	}
	o.st.set(kAccount, []byte(strings.TrimSpace(req.Account)))
	o.mu.Lock()
	o.account = strings.TrimSpace(req.Account)
	o.mu.Unlock()
	if err := o.startLocalMachine(); err != nil {
		return nil, err
	}
	return &localv1.LogInResponse{OwnerId: owner}, nil
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

// signRoot has the server sign msgs for this device (OwnerControl.Sign):
// what adding a machine or an agent, or managing one, takes.
func (o *Owner) signRoot(ctx context.Context, msgs ...proto.Message) ([]*identityv1.Signed, error) {
	if o.signHook != nil {
		return o.signHook(ctx, msgs)
	}
	ctl, _, err := o.control(ctx)
	if err != nil {
		return nil, err
	}
	req := &relayv1.SignRequest{}
	for _, m := range msgs {
		body, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
		if err != nil {
			return nil, err
		}
		req.Messages = append(req.Messages, &relayv1.Unsigned{Type: identity.MessageType(m), Body: body})
	}
	res, err := ctl.Sign(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(res.Signed) != len(msgs) {
		return nil, fmt.Errorf("the server signed %d of %d", len(res.Signed), len(msgs))
	}
	for i, sg := range res.Signed {
		out := msgs[i].ProtoReflect().New().Interface()
		o.mu.Lock()
		trust := o.trust
		o.mu.Unlock()
		if trust.VerifyRootSigned(sg, out) != nil {
			// The server's key may be newer than the chain held.
			o.latestChain(ctx)
			if err := trust.VerifyRootSigned(sg, out); err != nil {
				return nil, fmt.Errorf("what the server signed does not check out: %w", err)
			}
		}
		if !proto.Equal(out, msgs[i]) {
			return nil, errors.New("the server signed something else")
		}
	}
	return res.Signed, nil
}

// GetLogin is the account, as the server keeps it.
func (o *Owner) GetLogin(ctx context.Context) (*localv1.GetLoginResponse, error) {
	if err := o.requireInit(); err != nil {
		return nil, err
	}
	ctl, _, err := o.control(ctx)
	if err != nil {
		return nil, err
	}
	res, err := ctl.GetLogin(ctx, &relayv1.GetLoginRequest{})
	if err != nil {
		return nil, err
	}
	o.mu.Lock()
	server := strings.TrimPrefix(o.relayURL, "https://")
	o.mu.Unlock()
	if res.Account != "" && !res.Convert {
		o.mu.Lock()
		changed := o.account != res.Account
		o.account = res.Account
		o.mu.Unlock()
		if changed {
			o.st.set(kAccount, []byte(res.Account))
		}
	}
	return &localv1.GetLoginResponse{Account: res.Account, Server: server, Convert: res.Convert, Off: res.Off}, nil
}

// SetLogin changes the account's name or password, given the current
// password; a new password signs out every other device and agent that
// logged in. It also converts a password login from before accounts.
func (o *Owner) SetLogin(ctx context.Context, req *localv1.SetLoginRequest) (*localv1.SetLoginResponse, error) {
	if err := o.requireInit(); err != nil {
		return nil, err
	}
	if req.CurrentPassword == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("type the current password"))
	}
	ctl, _, err := o.control(ctx)
	if err != nil {
		return nil, err
	}
	o.mu.Lock()
	keep := ""
	if o.device != nil {
		keep = identity.KeyID(o.device.Public())
	}
	o.mu.Unlock()
	res, err := ctl.SetLogin(ctx, &relayv1.SetLoginRequest{CurrentPassword: req.CurrentPassword, Account: req.Account, NewPassword: req.NewPassword, Off: req.Off, DeviceKey: keep})
	if err != nil {
		return nil, err
	}
	o.mu.Lock()
	o.account = res.Account
	o.mu.Unlock()
	o.st.set(kAccount, []byte(res.Account))
	// The server's key may be new (a conversion): the longer chain.
	o.latestChain(ctx)
	return &localv1.SetLoginResponse{SignedOut: res.SignedOut}, nil
}
