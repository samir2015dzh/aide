package owner

import (
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/hermeshai/hermesh/gen/go/hermesh/identity/v1"
	localv1 "github.com/hermeshai/hermesh/gen/go/hermesh/local/v1"
	"github.com/hermeshai/hermesh/pkg/identity"
)

// A grant issued without a lifetime doesn't expire; one with a lifetime
// still does, at most 90 days on; and an existing grant is made long-lived
// by updating it with no lifetime, keeping what it may use.
func TestGrantLifetime(t *testing.T) {
	base := &identityv1.Grant{OwnerId: "o_x", GrantId: "g_1", Version: 1, ClientId: "c_1"}

	g := grantFromSpec(base, &localv1.GrantSpec{Label: "bot"})
	if !g.ExpiresAt.AsTime().Equal(identity.NoExpiry) {
		t.Fatalf("issued without a lifetime: expires %v, want never", g.ExpiresAt.AsTime())
	}
	if s := specFromGrant(g); s.TtlDays != 0 {
		t.Fatalf("a long-lived grant's spec: %d days", s.TtlDays)
	}

	dated := grantFromSpec(base, &localv1.GrantSpec{Label: "bot", ProjectIds: []string{"app"}, TtlDays: 120})
	if left := time.Until(dated.ExpiresAt.AsTime()); left < 89*24*time.Hour || left > 90*24*time.Hour {
		t.Fatalf("120 days asked: expires in %v, want 90 days", left)
	}
	if s := specFromGrant(dated); s.TtlDays != 90 {
		t.Fatalf("a dated grant's spec: %d days", s.TtlDays)
	}

	spec := specFromGrant(dated)
	spec.TtlDays = 0
	long := grantFromSpec(dated, spec)
	if !long.ExpiresAt.AsTime().Equal(identity.NoExpiry) || long.ProjectIds[0] != "app" || long.ClientLabel != "bot" {
		t.Fatalf("made long-lived: %v", long)
	}
}

// A long-lived grant is never refused for its age; one that ran out is.
func TestLongLivedGrantVerifies(t *testing.T) {
	root, _ := identity.GenerateKey()
	ownerID := identity.OwnerID(identity.MarshalPublicKey(root.Public()))
	rc, err := identity.Sign(&identityv1.RootCert{OwnerId: ownerID, Version: 1, IssuedAt: timestamppb.Now(),
		RootKeys: []*identityv1.RootKey{{PublicKey: identity.MarshalPublicKey(root.Public())}}}, root)
	if err != nil {
		t.Fatal(err)
	}
	tr := identity.NewTrust(ownerID)
	if _, err := tr.AcceptRootChain(&identityv1.RootChain{Certs: []*identityv1.Signed{rc}}); err != nil {
		t.Fatal(err)
	}
	client, _ := identity.GenerateKey()
	spki := identity.MarshalPublicKey(client.Public())
	base := &identityv1.Grant{OwnerId: ownerID, GrantId: "g_1", Version: 1, ClientId: identity.ClientID(spki), ClientPublicKey: spki}
	sign := func(g *identityv1.Grant) *identityv1.Signed {
		s, err := identity.Sign(g, root)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	long := sign(grantFromSpec(base, &localv1.GrantSpec{Label: "bot"}))
	for _, at := range []time.Time{time.Now(), time.Now().AddDate(1, 0, 0), time.Now().AddDate(50, 0, 0)} {
		if _, _, err := tr.VerifyGrant(long, at); err != nil {
			t.Fatalf("long-lived grant at %v: %v", at, err)
		}
	}
	dated := sign(grantFromSpec(base, &localv1.GrantSpec{Label: "bot", TtlDays: 7}))
	if _, _, err := tr.VerifyGrant(dated, time.Now().AddDate(0, 0, 8)); err == nil {
		t.Fatal("a 7-day grant verified 8 days on")
	}
}
