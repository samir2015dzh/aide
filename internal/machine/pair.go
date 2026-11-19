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
	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/hermeshai/hermesh/gen/go/hermesh/identity/v1"
	relayv1 "github.com/hermeshai/hermesh/gen/go/hermesh/relay/v1"
	"github.com/hermeshai/hermesh/gen/go/hermesh/relay/v1/relayv1connect"
	"github.com/hermeshai/hermesh/internal/connaddr"
	"github.com/hermeshai/hermesh/pkg/identity"
)

// Pair registers this machine with the owner: it shows a pairing code and
// waits until the owner approves it. address is a connection address, or
// the owner's server: its one owner, which it reports, is pinned then.
func (n *Machine) Pair(ctx context.Context, address, label string, out io.Writer) error {
	relayURL, ownerID, err := connaddr.ParseAny(address)
	if err != nil {
		return err
	}
	if ownerID == "" {
		if relayURL, err = connaddr.Discover(ctx, n.cfg.HTTPClient, relayURL); err != nil {
			return err
		}
		if ownerID, err = ServerOwner(ctx, n.cfg.HTTPClient, relayURL); err != nil {
			return err
		}
	}
	// A machine whose owner its relay no longer serves (the owner erased
	// everything, or its identity was reset) pairs with whichever owner comes
	// next; its projects stay.
	ownerGone := n.Paired() && OwnerGone(ctx, n.cfg.HTTPClient, n.RelayURL(), n.ownerID)
	if ownerGone {
		fmt.Fprintf(out, "The previous owner %s is gone from its server (it erased everything, or was reset); pairing with a new owner.\n", n.ownerID)
	}
	if n.Paired() && !ownerGone && ownerID != "" && n.ownerID != ownerID {
		return fmt.Errorf("this machine is already paired with owner %s", n.ownerID)
	}
	host, _ := os.Hostname()
	if label == "" {
		label = HostLabel("")
	}
	spki := identity.MarshalPublicKey(n.key.Public())
	pr := &identityv1.PairingRequest{Kind: identityv1.PairingKind_PAIRING_KIND_MACHINE, SubjectPublicKey: spki, Label: label,
		Hostname: host, Platform: runtime.GOOS + "/" + runtime.GOARCH, OwnerId: ownerID, CreatedAt: timestamppb.Now()}
	creds, ownerID, err := PairWithRelay(ctx, n.cfg.HTTPClient, relayURL, pr, n.key, out)
	if err != nil {
		return err
	}
	if n.Paired() && !ownerGone && n.ownerID != ownerID {
		return fmt.Errorf("this machine is already paired with owner %s", n.ownerID)
	}
	if err := n.Install(ownerID, relayURL, creds.RootChain, creds.GetMachineCert()); err != nil {
		return err
	}
	// The machine names itself: the name it asked with (its operator's
	// --name or its host name). The owner can rename it later.
	if err := n.SetName(label); err != nil {
		return err
	}
	fmt.Fprintf(out, "✓ Paired as “%s” (%s)\n", n.Name(), n.machineID)
	return nil
}

// OwnerGone reports whether the relay or center at relayURL says it no
// longer serves ownerID: the owner erased everything, or its identity was
// reset (a center), or another owner claimed the relay. An unreachable one
// does not count as gone.
func OwnerGone(ctx context.Context, hc connect.HTTPClient, relayURL, ownerID string) bool {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	info, err := relayv1connect.NewRelayPublicServiceClient(hc, strings.TrimSuffix(relayURL, "/")).GetInfo(ctx, &relayv1.GetInfoRequest{OwnerId: ownerID})
	return err == nil && info.OwnerId != ownerID
}

// HostLabel is a default name from this host's name: prefix plus its first
// DNS label, at most 64 bytes (CI runners and some clouds use host names
// longer than a relay accepts as a label).
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
// certificate (used by pairing and by the owner's own Mac).
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

// ServerOwner is the one owner of the server at relayURL, which it reports:
// a bare server address pins it, as a connection address would.
func ServerOwner(ctx context.Context, hc connect.HTTPClient, relayURL string) (string, error) {
	info, err := relayv1connect.NewRelayPublicServiceClient(hc, relayURL).GetInfo(ctx, &relayv1.GetInfoRequest{})
	if err != nil {
		return "", fmt.Errorf("server unreachable: %w", err)
	}
	if info.OwnerId == "" {
		return "", fmt.Errorf("the server at %s is not set up yet: claim it in the Aide app first, then pair with the connection address it shows", relayURL)
	}
	return info.OwnerId, nil
}

// resubmitRounds bounds how often an expired request is submitted again
// while the requester waits (about an hour with the relay's 10 minutes).
const resubmitRounds = 6

// PairWithRelay submits a self-signed pairing request, which names its
// owner, and waits for the owner to approve it, returning the credentials and
// the owner id. Re-running shows the same code (it is derived from the key),
// and so does a request submitted again once it expired while this waits.
func PairWithRelay(ctx context.Context, hc connect.HTTPClient, relayURL string, pr *identityv1.PairingRequest,
	key identity.Signer, out io.Writer) (*identityv1.Credentials, string, error) {
	if pr.OwnerId == "" {
		return nil, "", errors.New("a pairing request names its owner: use the connection address shown in the Aide app")
	}
	signed, err := identity.Sign(pr, key)
	if err != nil {
		return nil, "", err
	}
	pub := relayv1connect.NewRelayPublicServiceClient(hc, relayURL)
	res, err := pub.SubmitPairing(ctx, &relayv1.SubmitPairingRequest{Request: signed})
	if err != nil {
		return nil, "", fmt.Errorf("submit pairing request: %w", err)
	}
	what := "machine"
	if pr.Kind == identityv1.PairingKind_PAIRING_KIND_CLIENT {
		what = "client"
	}
	owner := pr.OwnerId
	fmt.Fprintf(out, "Pairing code: %s\nEnter it in the Aide app to add this %s (valid until %s).\nWaiting for approval...\n",
		res.Code, what, res.ExpiresAt.AsTime().Local().Format("15:04"))
	rounds := 0
	for {
		w, err := pub.WaitPairing(ctx, &relayv1.WaitPairingRequest{Code: res.Code, KnownOwnerId: owner})
		if err != nil {
			if ctx.Err() != nil {
				return nil, "", ctx.Err()
			}
			time.Sleep(2 * time.Second)
			continue
		}
		if w.Done {
			if identity.ChainOwner(w.GetCredentials().GetRootChain()) != owner {
				return nil, "", errors.New("the credentials are from a different owner than the one shown; pair again")
			}
			return w.Credentials, owner, nil
		}
		if w.Expired {
			if rounds++; rounds > resubmitRounds {
				return nil, "", errors.New("the pairing code expired; run the command again for a new one")
			}
			// The same code again (it comes from the key): it reaches the
			// owner's devices again.
			if res, err = pub.SubmitPairing(ctx, &relayv1.SubmitPairingRequest{Request: signed}); err != nil {
				return nil, "", fmt.Errorf("submit pairing request: %w", err)
			}
		}
	}
}
