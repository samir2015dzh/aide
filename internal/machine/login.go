package machine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	relayv1 "github.com/samir2015dzh/aide/gen/go/aide/relay/v1"
	"github.com/samir2015dzh/aide/gen/go/aide/relay/v1/relayv1connect"
	"github.com/samir2015dzh/aide/internal/connaddr"
	"github.com/samir2015dzh/aide/pkg/identity"
)

// LogIn makes this machine one of the owner's (docs/accounts.md): it logs
// in to the owner's server with the account and password, and the server
// signs its machine cert itself. server is the server's address.
func (n *Machine) LogIn(ctx context.Context, server, account, password, label string, out io.Writer) error {
	relayURL := connaddr.Server(server)
	if relayURL == "" {
		return connaddr.ErrNoServer
	}
	// A machine whose owner its server no longer serves (the owner erased
	// everything) logs in to whichever owner comes next; its projects stay.
	ownerGone := n.Paired() && OwnerGone(ctx, n.cfg.HTTPClient, n.RelayURL(), n.ownerID)
	if ownerGone {
		fmt.Fprintf(out, "The previous owner %s is gone from its server (it erased everything); logging in to the new one.\n", n.ownerID)
	}
	if label == "" {
		label = HostLabel("")
	}
	label = SanitizeName(label)
	host, _ := os.Hostname()
	res, err := LogInTo(ctx, n.cfg.HTTPClient, relayURL, account, password, &relayv1.Joiner{Kind: relayv1.JoinKind_JOIN_KIND_MACHINE,
		PublicKey: identity.MarshalPublicKey(n.key.Public()), Label: label, Hostname: host, Platform: runtime.GOOS + "/" + runtime.GOARCH})
	if err != nil {
		return err
	}
	if n.Paired() && !ownerGone && n.ownerID != res.OwnerId {
		return fmt.Errorf("this machine already belongs to owner %s", n.ownerID)
	}
	if err := n.Install(res.OwnerId, relayURL, res.RootChain, res.Credential); err != nil {
		return err
	}
	// The machine names itself: its operator's --name or its host name, or
	// the free name the server gave for it. The owner can rename it later.
	if res.MachineName != "" {
		label = res.MachineName
	}
	if err := n.SetName(label); err != nil {
		return err
	}
	fmt.Fprintf(out, "✓ Logged in as “%s” (%s)\n", n.Name(), n.machineID)
	return nil
}

// LogInTo logs j in to the server at relayURL with the account and
// password, and checks what the server signed: a root chain naming the
// owner it says, and a credential for j's key that it verifies.
func LogInTo(ctx context.Context, hc connect.HTTPClient, relayURL, account, password string, j *relayv1.Joiner) (*relayv1.LogInResponse, error) {
	if strings.TrimSpace(account) == "" || password == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("the account and the password are needed"))
	}
	res, err := relayv1connect.NewRelayPublicServiceClient(hc, strings.TrimSuffix(relayURL, "/")).
		LogIn(ctx, &relayv1.LogInRequest{Account: account, Password: password, Joiner: j})
	if err != nil {
		return nil, err
	}
	return res, CheckLogIn(res, j)
}

// CheckLogIn verifies what a server answered a login (or a setup) with.
func CheckLogIn(res *relayv1.LogInResponse, j *relayv1.Joiner) error {
	trust := identity.NewTrust(res.OwnerId)
	if _, err := trust.AcceptRootChain(res.RootChain); err != nil {
		return fmt.Errorf("the server's root chain does not check out: %w", err)
	}
	creds := &identityv1.Credentials{RootChain: res.RootChain}
	switch j.Kind {
	case relayv1.JoinKind_JOIN_KIND_DEVICE:
		creds.Subject = &identityv1.Credentials_DeviceCert{DeviceCert: res.Credential}
	case relayv1.JoinKind_JOIN_KIND_MACHINE:
		creds.Subject = &identityv1.Credentials_MachineCert{MachineCert: res.Credential}
	case relayv1.JoinKind_JOIN_KIND_AGENT:
		creds.Subject = &identityv1.Credentials_Grant{Grant: res.Credential}
	}
	sub, err := trust.VerifyCredentials(creds, time.Now())
	if err != nil {
		return fmt.Errorf("the server's answer does not check out: %w", err)
	}
	if !sub.PublicKey.Equal(mustKey(j.PublicKey)) {
		return errors.New("the server signed another key")
	}
	if len(j.MachinePublicKey) > 0 {
		_, pub, err := trust.VerifyMachineCert(res.MachineCert)
		if err != nil {
			return err
		}
		if !pub.Equal(mustKey(j.MachinePublicKey)) {
			return errors.New("the server signed another machine's key")
		}
	}
	return nil
}

func mustKey(spki []byte) any {
	pub, _ := identity.ParsePublicKey(spki)
	return pub
}

// OwnerGone reports whether the server at relayURL says it no longer
// serves ownerID: the owner erased everything, or another owner set it up.
// An unreachable one does not count as gone.
func OwnerGone(ctx context.Context, hc connect.HTTPClient, relayURL, ownerID string) bool {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	info, err := relayv1connect.NewRelayPublicServiceClient(hc, strings.TrimSuffix(relayURL, "/")).GetInfo(ctx, &relayv1.GetInfoRequest{OwnerId: ownerID})
	return err == nil && info.OwnerId != ownerID
}

// HostLabel is a default name from this host's name: prefix plus its first
// DNS label, at most 64 bytes (CI runners and some clouds use host names
// longer than a server accepts as a label).
func HostLabel(prefix string) string {
	host, _ := os.Hostname()
	return hostLabel(prefix, host)
}

func hostLabel(prefix, host string) string {
	if i := strings.IndexByte(host, '.'); i > 0 {
		host = host[:i]
	}
	s := prefix + host
	for len(s) > 64 {
		_, n := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-n]
	}
	return s
}

// Install verifies and stores the owner trust anchor and this machine's
// certificate (logging in, and a device's own machine).
func (n *Machine) Install(ownerID, relayURL string, rootChain *identityv1.RootChain, machineCert *identityv1.Signed) error {
	trust := identity.NewTrust(ownerID)
	if _, err := trust.AcceptRootChain(rootChain); err != nil {
		return fmt.Errorf("owner root chain: %w", err)
	}
	_, pub, err := trust.VerifyMachineCert(machineCert)
	if err != nil {
		return err
	}
	if !pub.Equal(n.key.Public()) {
		return errors.New("received a certificate for a different key")
	}
	for k, v := range map[string]string{kOwnerID: ownerID, kRelayURL: relayURL} {
		if err := n.st.Set(k, []byte(v)); err != nil {
			return err
		}
	}
	n.st.SetProto(kRootChain, rootChain)
	n.st.SetProto(kMachineCert, machineCert)
	n.mu.Lock()
	n.ownerID, n.relayURL, n.trust = ownerID, relayURL, trust
	n.machineCert = machineCert
	n.mu.Unlock()
	n.audit.Log(AuditRecord{Kind: "paired", Subject: ownerID})
	return nil
}

// PublicKeySPKI returns this machine's public key (PKIX DER).
func (n *Machine) PublicKeySPKI() []byte { return identity.MarshalPublicKey(n.key.Public()) }
