package owner

import (
	"context"
	"errors"
	"os"
	"strings"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/hermeshai/hermesh/gen/go/hermesh/identity/v1"
	localv1 "github.com/hermeshai/hermesh/gen/go/hermesh/local/v1"
	relayv1 "github.com/hermeshai/hermesh/gen/go/hermesh/relay/v1"
	"github.com/hermeshai/hermesh/internal/connaddr"
	"github.com/hermeshai/hermesh/internal/passlogin"
	"github.com/hermeshai/hermesh/pkg/identity"
)

// Logging in with an account and password (docs/password-login.md): a
// trusted device turns it on with a password key, a root key whose private
// half the relay keeps sealed by the password; a new device that knows the
// password opens it and signs its own join with it, so no other device
// approves and no codes or digits are compared.

// PasswordLogin makes this device one of the owner's trusted devices with
// the owner's account and password, as an approved join would: its root key
// in the next root cert, its device cert and, if asked, its own machine,
// all signed with the password key, so changing the password signs it out.
func (o *Owner) PasswordLogin(ctx context.Context, req *localv1.PasswordLoginRequest) (*localv1.PasswordLoginResponse, error) {
	if o.Initialized() {
		return nil, connect.NewError(connect.CodeAlreadyExists, errors.New("this device already belongs to an owner"))
	}
	if o.machineOnly() {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("this computer is a machine of an owner; leave it first"))
	}
	if o.cfg.NoLocalMachine && req.LocalMachineName != "" {
		return nil, errNoLocalMachine
	}
	server := connaddr.Server(req.RelayUrl)
	if server == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("which server? type its address"))
	}
	relayURL := normalizeRelayURL(server)
	if err := o.checkRelay(ctx, relayURL); err != nil {
		return nil, err
	}
	sess, err := passlogin.Login(ctx, o.publicClient(relayURL), req.Account, req.Password)
	if err != nil {
		return nil, err
	}
	rootPub, err := o.rootKey(ctx, req.RootPublicKey)
	if err != nil {
		return nil, err
	}
	spki := identity.MarshalPublicKey(rootPub)
	label := strings.TrimSpace(req.RootLabel)
	if label == "" {
		label, _ = os.Hostname()
	}
	device, err := identity.GenerateKey()
	if err != nil {
		return nil, err
	}
	msgs := []proto.Message{deviceCert(sess.OwnerID, device, label)}
	if req.LocalMachineName != "" {
		nc, err := o.localMachineCert(sess.OwnerID, rootPub)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, nc)
	}
	signed, err := sess.Sign(msgs...)
	if err != nil {
		return nil, err
	}
	chain, err := sess.Join(ctx, &identityv1.RootKey{PublicKey: spki, Label: label, Platform: req.RootPlatform})
	if err != nil {
		return nil, err
	}
	o.mu.Lock()
	o.rootPub = rootPub
	o.mu.Unlock()
	if err := o.saveIdentity(sess.OwnerID, relayURL, spki, chain, device, signed[0], sess.Account); err != nil {
		return nil, err
	}
	if len(signed) > 1 {
		if err := o.installLocalMachine(ctx, signed[1], req.LocalMachineName); err != nil {
			return nil, err
		}
	}
	return &localv1.PasswordLoginResponse{OwnerId: sess.OwnerID, ConnectAddress: connaddr.Format(relayURL, sess.OwnerID)}, nil
}

// passwordKeys is the key ids of the password keys in the root cert.
func passwordKeys(rc *identityv1.RootCert) map[string]bool {
	out := map[string]bool{}
	for _, rk := range rc.GetRootKeys() {
		if rk.Platform != passlogin.Platform {
			continue
		}
		if pub, err := identity.ParsePublicKey(rk.PublicKey); err == nil {
			out[identity.KeyID(pub)] = true
		}
	}
	return out
}

// loggedInWith is the root keys that root certs signed by one of keys
// added, by key id, with their labels: the devices that logged in with a
// password.
func loggedInWith(chain *identityv1.RootChain, keys map[string]bool) map[string]string {
	out := map[string]string{}
	var prev *identityv1.RootCert
	for _, c := range chain.GetCerts() {
		rc := &identityv1.RootCert{}
		if identity.Unverified(c, rc) != nil {
			return out
		}
		if prev != nil && keys[c.SignerKeyId] {
			for _, rk := range rc.RootKeys {
				if passlogin.Listed(prev, rk.PublicKey) {
					continue
				}
				if pub, err := identity.ParsePublicKey(rk.PublicKey); err == nil {
					out[identity.KeyID(pub)] = rk.Label
				}
			}
		}
		prev = rc
	}
	return out
}

// SetLoginPassword turns logging in with a password on, or changes the
// password, or (both empty) turns it off, in one Touch ID: the next root
// cert has a new password key instead of the old one, and without the
// devices that logged in with the old one; what those devices signed is
// re-issued by this one, their own machines and the agents that logged in
// with the old password are revoked. It returns how many devices left.
func (o *Owner) SetLoginPassword(ctx context.Context, account, password string) (uint32, error) {
	if err := o.requireInit(); err != nil {
		return 0, err
	}
	off := account == "" && password == ""
	if !off {
		if err := passlogin.CheckNew(account, password); err != nil {
			return 0, connect.NewError(connect.CodeInvalidArgument, err)
		}
	}
	ctl, err := o.latestChain(ctx)
	if err != nil {
		return 0, err
	}
	cur := o.trust.Root()
	old := passwordKeys(cur)
	added := loggedInWith(o.trust.RootChain(), old)
	if _, ok := added[o.Status().DeviceKeyId]; ok {
		return 0, connect.NewError(connect.CodeFailedPrecondition, errors.New("this device logged in with the password: change it on a device that did not"))
	}
	if off && len(old) == 0 {
		_, err := ctl.SetPasswordLogin(ctx, &relayv1.SetPasswordLoginRequest{})
		return 0, err
	}
	var keys []*identityv1.RootKey
	for _, rk := range cur.RootKeys {
		pub, err := identity.ParsePublicKey(rk.PublicKey)
		if err != nil {
			continue
		}
		if id := identity.KeyID(pub); old[id] || added[id] != "" {
			continue
		}
		keys = append(keys, rk)
	}
	var key *identity.SoftwareKey
	if !off {
		if key, err = identity.GenerateKey(); err != nil {
			return 0, err
		}
		keys = append(keys, &identityv1.RootKey{PublicKey: identity.MarshalPublicKey(key.Public()), Label: passlogin.Label, Platform: passlogin.Platform})
	}
	gone := func(id string) bool { return added[id] != "" }
	machines, err := ctl.ListMachines(ctx, &relayv1.ListMachinesRequest{})
	if err != nil {
		return 0, err
	}
	clients, err := ctl.ListClients(ctx, &relayv1.ListClientsRequest{})
	if err != nil {
		return 0, err
	}
	var msgs []proto.Message
	var revokeMachines, revokeGrants []string
	for _, e := range machines.Machines {
		nc := &identityv1.MachineCert{}
		if identity.Unverified(e.MachineCert, nc) != nil || nc.OwnerId != o.ownerID {
			continue
		}
		switch signer := e.MachineCert.SignerKeyId; {
		case gone(nc.DeviceKeyId):
			revokeMachines = append(revokeMachines, nc.MachineId)
		case gone(signer) || old[signer]:
			nc.IssuedAt = timestamppb.Now()
			msgs = append(msgs, nc)
		}
	}
	nMachines := len(msgs)
	for _, c := range clients.Clients {
		g := &identityv1.Grant{}
		if c.Revoked || identity.Unverified(c.Grant, g) != nil || g.OwnerId != o.ownerID {
			continue
		}
		switch signer := c.Grant.GetSignerKeyId(); {
		case old[signer]:
			revokeGrants = append(revokeGrants, g.GrantId) // an agent that logged in with the old password
		case gone(signer):
			g.Version++
			msgs = append(msgs, g)
		}
	}
	signed, err := o.extendChain(ctx, ctl, keys, msgs...)
	if err != nil {
		return 0, err
	}
	for i, s := range signed[1:] {
		if i < nMachines {
			_, err = ctl.PublishMachineCert(ctx, &relayv1.PublishMachineCertRequest{MachineCert: s})
		} else {
			_, err = ctl.PublishGrant(ctx, &relayv1.PublishGrantRequest{Grant: s})
			o.st.putGrant(s, false)
		}
		if err != nil {
			return 0, err
		}
	}
	set := &relayv1.SetPasswordLoginRequest{}
	if key != nil {
		if set.Login, err = passlogin.Seal(o.ownerID, account, password, key); err != nil {
			return 0, err
		}
	}
	if _, err := ctl.SetPasswordLogin(ctx, set); err != nil {
		return 0, err
	}
	if len(revokeMachines) > 0 || len(revokeGrants) > 0 {
		if _, err := o.revoke(ctx, &identityv1.RevocationList{OwnerId: o.ownerID, RevokedMachineIds: revokeMachines, RevokedGrantIds: revokeGrants}); err != nil {
			return 0, err
		}
	}
	return uint32(len(added)), nil
}

// GetLoginPassword tells whether logging in with a password is on, with
// which account, the server to type, and the devices that logged in.
func (o *Owner) GetLoginPassword(ctx context.Context) (*localv1.GetLoginPasswordResponse, error) {
	if err := o.requireInit(); err != nil {
		return nil, err
	}
	ctl, err := o.latestChain(ctx)
	if err != nil {
		return nil, err
	}
	res, err := ctl.GetPasswordLogin(ctx, &relayv1.GetPasswordLoginRequest{})
	if err != nil || res.Account == "" {
		return &localv1.GetLoginPasswordResponse{}, err
	}
	pub, err := identity.ParsePublicKey(res.PublicKey)
	if err != nil {
		return nil, err
	}
	out := &localv1.GetLoginPasswordResponse{Account: res.Account}
	o.mu.Lock()
	out.Server = strings.TrimPrefix(o.relayURL, "https://")
	o.mu.Unlock()
	for _, label := range loggedInWith(o.trust.RootChain(), map[string]bool{identity.KeyID(pub): true}) {
		out.Devices = append(out.Devices, label)
	}
	return out, nil
}
