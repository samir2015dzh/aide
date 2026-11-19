package relay

import (
	"time"

	identityv1 "github.com/hermeshai/hermesh/gen/go/hermesh/identity/v1"
)

// Backend persists what the relay knows, per owner: signed objects and
// metadata. The account and the server's key are kept beside them, in the
// Store's meta (account.go).
//
// The relay serves one owner and keeps this in SQLite (Store).
type Backend interface {
	RelayID() (string, error)

	// RootChain is nil for an owner this backend does not know.
	RootChain(ownerID string) (*identityv1.RootChain, error)
	SetRootChain(ownerID string, c *identityv1.RootChain) error
	Revocations(ownerID string) (*identityv1.RevocationList, error)
	SetRevocations(ownerID string, r *identityv1.RevocationList) error

	PutMachineCert(ownerID, machineID string, cert *identityv1.Signed) error
	DeleteMachineCert(ownerID, machineID string) error
	MachineCerts(ownerID string) (map[string]*identityv1.Signed, error)

	// PutGrant stores g only if its version is newer than the stored one.
	PutGrant(ownerID string, g *identityv1.Grant, signed *identityv1.Signed) error
	// DeleteGrant forgets a revoked grant (the revocation list still refuses it).
	DeleteGrant(ownerID, grantID string) error
	Grants(ownerID string) ([]*identityv1.Signed, error)
	GrantForClient(ownerID, clientID string) (*identityv1.Signed, error)

	LogConnection(ownerID string, role int, subjectID, remote, event string)
	Close() error
}

// EraseRetention is how long a relay keeps an erased owner's order: while
// the erase runs, for those online to erase themselves (and say so). A
// device that connects later is refused, and offered a reset
// (docs/erase-everything.md).
const EraseRetention = 10 * time.Minute

// EraseStore keeps erase orders: a Backend that has it lets owners erase
// everything.
type EraseStore interface {
	// EraseOwner keeps the owner's signed erase order, with its root chain
	// as it is, and forgets the owner and everything under it.
	EraseOwner(ownerID, eraseID string, order *identityv1.Signed, chain *identityv1.RootChain, at time.Time) error
	// EraseOrder is the owner's erase order, its root chain and the key ids
	// that acked it; nil once EraseRetention has passed, or for none.
	EraseOrder(ownerID string, now time.Time) (*identityv1.Signed, *identityv1.RootChain, []string, error)
	// AckErase records that a key is erasing; unknown erase ids are ignored.
	AckErase(eraseID, keyID string, at time.Time) error
	// PurgeErases deletes the orders (and their acks) past EraseRetention.
	PurgeErases(now time.Time) error
}
