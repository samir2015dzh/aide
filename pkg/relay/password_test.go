package relay

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/hermeshai/hermesh/gen/go/hermesh/identity/v1"
	relayv1 "github.com/hermeshai/hermesh/gen/go/hermesh/relay/v1"
	"github.com/hermeshai/hermesh/gen/go/hermesh/relay/v1/relayv1connect"
	"github.com/hermeshai/hermesh/internal/passlogin"
	"github.com/hermeshai/hermesh/pkg/identity"
)

// The relay's side of logging in with a password: it hides which accounts
// exist, hands the sealed key only for the right password and never the
// verifier, locks the account after wrong passwords in a row, and takes
// from a client that logged in only a chain that adds a device, signed by
// the password key.
func TestPasswordLoginOnTheRelay(t *testing.T) {
	s, err := New(Config{DataDir: t.TempDir(), Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	ctx := context.Background()
	pub := relayv1connect.NewRelayPublicServiceClient(srv.Client(), srv.URL)

	// The owner claims the relay with device A, then adds the password key.
	a, _ := identity.GenerateKey()
	aKey := &identityv1.RootKey{PublicKey: identity.MarshalPublicKey(a.Public()), Label: "mac"}
	owner := identity.OwnerID(aKey.PublicKey)
	sign := func(m proto.Message, k *identity.SoftwareKey) *identityv1.Signed {
		sg, err := identity.Sign(m, k)
		if err != nil {
			t.Fatal(err)
		}
		return sg
	}
	device, _ := identity.GenerateKey()
	chain := &identityv1.RootChain{Certs: []*identityv1.Signed{sign(&identityv1.RootCert{OwnerId: owner, RootKeys: []*identityv1.RootKey{aKey}, Version: 1, IssuedAt: timestamppb.Now()}, a)}}
	code, _ := s.ClaimCode()
	if _, err := pub.Claim(ctx, &relayv1.ClaimRequest{ClaimCode: code, RootChain: chain,
		DeviceCert: sign(&identityv1.OwnerDeviceCert{OwnerId: owner, DevicePublicKey: identity.MarshalPublicKey(device.Public()), IssuedAt: timestamppb.Now()}, a)}); err != nil {
		t.Fatal(err)
	}
	p, _ := identity.GenerateKey()
	pKey := &identityv1.RootKey{PublicKey: identity.MarshalPublicKey(p.Public()), Label: passlogin.Label, Platform: passlogin.Platform}
	chain.Certs = append(chain.Certs, sign(&identityv1.RootCert{OwnerId: owner, RootKeys: []*identityv1.RootKey{aKey, pKey}, Version: 2, IssuedAt: timestamppb.Now()}, a))
	ten, err := s.tenantFor(owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.acceptRootChain(ten, chain); err != nil {
		t.Fatal(err)
	}
	login, err := passlogin.Seal(owner, "samir", "correct horse battery", p)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.sqlite.SetPasswordLogin(login); err != nil {
		t.Fatal(err)
	}

	// Which accounts exist does not show.
	fake1, err := pub.StartLogin(ctx, &relayv1.StartLoginRequest{Account: "nobody"})
	if err != nil {
		t.Fatal(err)
	}
	fake2, _ := pub.StartLogin(ctx, &relayv1.StartLoginRequest{Account: "nobody"})
	own, _ := pub.StartLogin(ctx, &relayv1.StartLoginRequest{Account: "Samir"})
	if !proto.Equal(fake1.Kdf, fake2.Kdf) || proto.Equal(fake1.Kdf, own.Kdf) || !proto.Equal(own.Kdf, login.Kdf) {
		t.Fatalf("made-up parameters: %v %v %v", fake1.Kdf, fake2.Kdf, own.Kdf)
	}

	right, _, _ := passlogin.Derive("correct horse battery", login.Kdf)
	wrong, _, _ := passlogin.Derive("wrong", login.Kdf)
	res, err := pub.Login(ctx, &relayv1.LoginRequest{Account: "samir", AuthKey: right})
	if err != nil {
		t.Fatal(err)
	}
	if res.OwnerId != owner || len(res.Login.Verifier) != 0 || len(res.Login.SealedKey) == 0 {
		t.Fatalf("login: %v", res)
	}

	// Lockout: five wrong in a row lock even the right password out.
	for i := range lockAfter {
		if _, err := pub.Login(ctx, &relayv1.LoginRequest{Account: "samir", AuthKey: wrong}); connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Fatalf("wrong password %d: %v", i, err)
		}
	}
	if _, err := pub.Login(ctx, &relayv1.LoginRequest{Account: "samir", AuthKey: right}); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("after %d wrong passwords: %v", lockAfter, err)
	}
	s.logins.succeeded() // the lock ran out

	// What a client that logged in may publish.
	b, _ := identity.GenerateKey()
	bKey := &identityv1.RootKey{PublicKey: identity.MarshalPublicKey(b.Public()), Label: "phone", Platform: "android"}
	try := func(keys []*identityv1.RootKey, by *identity.SoftwareKey) error {
		c := proto.Clone(chain).(*identityv1.RootChain)
		c.Certs = append(c.Certs, sign(&identityv1.RootCert{OwnerId: owner, RootKeys: keys, Version: 3, IssuedAt: timestamppb.Now()}, by))
		_, err := pub.Login(ctx, &relayv1.LoginRequest{Account: "samir", AuthKey: right, RootChain: c})
		return err
	}
	if err := try([]*identityv1.RootKey{pKey, bKey}, p); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("a chain that removes the Mac: %v", err)
	}
	if err := try([]*identityv1.RootKey{aKey, pKey, bKey}, b); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("a chain not signed with the password key: %v", err)
	}
	if err := try([]*identityv1.RootKey{aKey, pKey, bKey}, p); err != nil {
		t.Fatalf("a chain that adds the phone: %v", err)
	}
	if ten.trust.RootVersion() != 3 {
		t.Fatalf("root version %d", ten.trust.RootVersion())
	}

	// Without the password key in the root cert, logging in is off.
	if err := s.acceptRootChain(ten, &identityv1.RootChain{Certs: append(proto.Clone(ten.trust.RootChain()).(*identityv1.RootChain).Certs,
		sign(&identityv1.RootCert{OwnerId: owner, RootKeys: []*identityv1.RootKey{aKey, bKey}, Version: 4, IssuedAt: timestamppb.Now()}, a))}); err != nil {
		t.Fatal(err)
	}
	if _, err := pub.Login(ctx, &relayv1.LoginRequest{Account: "samir", AuthKey: right}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("with the password key gone: %v", err)
	}
}
