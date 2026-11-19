package machine

import (
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/hermeshai/hermesh/gen/go/hermesh/identity/v1"
	"github.com/hermeshai/hermesh/pkg/identity"
)

// A machine never turns a long-lived grant away for its age; one that ran
// out, or was revoked whatever its expiry, it does.
func TestGrantExpiry(t *testing.T) {
	n := &Machine{machineID: "m_1", revocations: &identityv1.RevocationList{}}
	g := func(id string, exp time.Time) *identityv1.Grant {
		return &identityv1.Grant{GrantId: id, Version: 1, MachineIds: []string{"*"}, ExpiresAt: timestamppb.New(exp)}
	}
	if err := n.grantProblemLocked(g("g_long", identity.NoExpiry)); err != nil {
		t.Fatalf("long-lived: %v", err)
	}
	if err := n.grantProblemLocked(g("g_old", time.Now().Add(-time.Hour))); err == nil || err.Error() != "grant expired" {
		t.Fatalf("expired: %v", err)
	}
	n.revocations.RevokedGrantIds = []string{"g_long"}
	if err := n.grantProblemLocked(g("g_long", identity.NoExpiry)); err == nil || err.Error() != "grant revoked" {
		t.Fatalf("long-lived, revoked: %v", err)
	}
}
