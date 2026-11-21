package owner

import (
	"slices"
	"testing"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	relayv1 "github.com/samir2015dzh/aide/gen/go/aide/relay/v1"
	"github.com/samir2015dzh/aide/pkg/identity"
)

// A machine has one Her, who holds one grant: Her turned on again while her
// machine held none (its data started over, or the grant never reached it)
// was given another beside the one before, and the owner saw "Her on
// <machine>" twice. Which of the relay's clients go: Her's on that machine,
// standing, of this owner, those kept left out.
func TestHerGrantsIn(t *testing.T) {
	root, _ := identity.GenerateKey()
	oldKey, _ := identity.GenerateKey()
	newKey, _ := identity.GenerateKey()
	entry := func(g *identityv1.Grant, revoked bool) *relayv1.ClientEntry {
		s, err := identity.Sign(g, root)
		if err != nil {
			t.Fatal(err)
		}
		return &relayv1.ClientEntry{Grant: s, Revoked: revoked}
	}
	before := HerGrant("o_1", "m_1", "box", identity.MarshalPublicKey(oldKey.Public()))
	again := HerGrant("o_1", "m_1", "box", identity.MarshalPublicKey(oldKey.Public()))
	now := HerGrant("o_1", "m_1", "box", identity.MarshalPublicKey(newKey.Public()))
	other := HerGrant("o_1", "m_2", "desk", identity.MarshalPublicKey(newKey.Public()))
	stranger := HerGrant("o_2", "m_1", "box", identity.MarshalPublicKey(newKey.Public()))
	agent := &identityv1.Grant{OwnerId: "o_1", GrantId: "g_agent", ClientId: "c_agent", ClientLabel: "Muse"}
	gone := HerGrant("o_1", "m_1", "box", identity.MarshalPublicKey(oldKey.Public()))
	clients := []*relayv1.ClientEntry{entry(before, false), entry(again, false), entry(now, false), entry(other, false),
		entry(stranger, false), entry(agent, false), entry(gone, true), entry(now, false), {}}

	if got, want := herGrantsIn(clients, "o_1", "m_1", nil), []string{before.GrantId, again.GrantId, now.GrantId}; !slices.Equal(got, want) {
		t.Errorf("Her turned off: revokes %v, want %v", got, want)
	}
	// Her's machine took a new grant: the others go, both for the key from before.
	keepNew := func(g *identityv1.Grant) bool { return g.GrantId == now.GrantId }
	if got, want := herGrantsIn(clients, "o_1", "m_1", keepNew), []string{before.GrantId, again.GrantId}; !slices.Equal(got, want) {
		t.Errorf("a new grant: revokes %v, want %v", got, want)
	}
	// She holds a grant for the old key: those for it stay (which one her
	// machine holds is not known), the one for a key it doesn't keep goes.
	oldID := identity.ClientID(identity.MarshalPublicKey(oldKey.Public()))
	if got, want := herGrantsIn(clients, "o_1", "m_1", func(g *identityv1.Grant) bool { return g.ClientId == oldID }), []string{now.GrantId}; !slices.Equal(got, want) {
		t.Errorf("held: revokes %v, want %v", got, want)
	}
	if got := herGrantsIn(clients, "o_1", "m_3", nil); len(got) != 0 {
		t.Errorf("a machine without Her: revokes %v", got)
	}
}
