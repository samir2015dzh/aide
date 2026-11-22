package identity

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/hpke"
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/samir2015dzh/aide/internal/legacy"
	"regexp"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
)

// Messages between the owner's inboxes (docs/agent-messages.md §4) are
// signed by their sender, then HPKE-sealed to the recipient inbox's key with
// the suite notices use. The inbox and the message id are part of the HPKE
// info, so a sealed message the relay moves to another inbox or id doesn't
// open.

func messageInfo(inbox, id string) []byte {
	return []byte(legacy.Message + inbox + "\x00" + id)
}

// NewInboxKey makes an inbox's X25519 key pair.
func NewInboxKey() (private, public []byte, err error) {
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	return k.Bytes(), k.PublicKey().Bytes(), nil
}

// InboxPublicKey is the public half of an inbox's private key.
func InboxPublicKey(private []byte) ([]byte, error) {
	k, err := ecdh.X25519().NewPrivateKey(private)
	if err != nil {
		return nil, err
	}
	return k.PublicKey().Bytes(), nil
}

// SealMessage encrypts m to an inbox key, for that inbox and message id.
func SealMessage(inboxKey []byte, inbox, id string, m proto.Message) ([]byte, error) {
	kem, kdf, aead := noticeSuite()
	if len(inboxKey) != 32 {
		return nil, errors.New("an inbox key is 32 bytes (X25519)")
	}
	pk, err := kem.NewPublicKey(inboxKey)
	if err != nil {
		return nil, err
	}
	body, err := proto.Marshal(m)
	if err != nil {
		return nil, err
	}
	return hpke.Seal(pk, kdf, aead, messageInfo(inbox, id), body)
}

// OpenMessage decrypts what SealMessage made, with the inbox's private key.
func OpenMessage(private []byte, inbox, id string, sealed []byte, out proto.Message) error {
	kem, kdf, aead := noticeSuite()
	k, err := kem.NewPrivateKey(private)
	if err != nil {
		return err
	}
	body, err := hpke.Open(k, kdf, aead, messageInfo(inbox, id), sealed)
	if err != nil {
		return err
	}
	return proto.Unmarshal(body, out)
}

// ContactInfo is a contact that verified against the owner's trust.
type ContactInfo struct {
	// Inbox is the client id (c_…) or machine id (m_…).
	Inbox string
	// Name is the agent's as the owner approved it; "" for a machine.
	Name string
	// Kind is "mcp" or "cli" for an agent, "machine" for a machine.
	Kind string
	// Key is the identity key that signs the inbox's registration and its
	// messages.
	Key *ecdsa.PublicKey
	// InboxKey is the registered X25519 key, nil until the inbox has one.
	InboxKey []byte
	// TakesMessages is how the inbox said it takes them.
	TakesMessages string
	// Tier is when a message reaches it: "push", "turn" or "pull" (TierOf).
	Tier string
	// Category is the kind of agent the owner set ("personal", "local"),
	// "" for none; CategoryMachineID, with "local", its machine.
	Category, CategoryMachineID string
}

// TierOf is when a message reaches an inbox that takes them as takes says
// (InboxRegistration.takes_messages): "push" for a session's (the machine
// hands it in at once, starting a turn if none runs), a socket's ("wait")
// and a doorbell's ("webhook"); "turn" for hooks, read at the agent's next
// turn; "pull" for an inbox read when its agent next checks it ("poll", or
// none said).
func TierOf(takes string) string {
	switch takes {
	case "session", "wait", "webhook":
		return "push"
	case "hook":
		return "turn"
	}
	return "pull"
}

// VerifyCard checks an agent's contact card: signed by the device key of a
// current trusted device, for this owner, its client id that of its key.
func (t *Trust) VerifyCard(card, deviceCert *identityv1.Signed) (*identityv1.ContactCard, *ecdsa.PublicKey, error) {
	_, dev, err := t.VerifyDeviceCert(deviceCert)
	if err != nil {
		return nil, nil, fmt.Errorf("card: %w", err)
	}
	c := &identityv1.ContactCard{}
	if err := VerifyWith(card, dev, c); err != nil {
		return nil, nil, fmt.Errorf("card: %w", err)
	}
	if c.OwnerId != t.ownerID {
		return nil, nil, errors.New("card: wrong owner")
	}
	if c.ClientId != ClientID(c.ClientPublicKey) {
		return nil, nil, errors.New("card: client id does not match key")
	}
	pub, err := ParsePublicKey(c.ClientPublicKey)
	if err != nil {
		return nil, nil, err
	}
	return c, pub, nil
}

// VerifyBoundary checks the owner's information boundary
// (docs/agent-messages.md §15.5): signed by one of the owner's trusted
// devices, for this owner, its rules readable.
func (t *Trust) VerifyBoundary(policy, deviceCert *identityv1.Signed) (*identityv1.BoundaryPolicy, error) {
	_, dev, err := t.VerifyDeviceCert(deviceCert)
	if err != nil {
		return nil, fmt.Errorf("information boundary: %w", err)
	}
	p := &identityv1.BoundaryPolicy{}
	if err := VerifyWith(policy, dev, p); err != nil {
		return nil, fmt.Errorf("information boundary: %w", err)
	}
	if p.OwnerId != t.ownerID {
		return nil, errors.New("information boundary: wrong owner")
	}
	if p.IssuedAt == nil {
		return nil, errors.New("information boundary: no issued_at")
	}
	if err := CheckBoundaryRules(p.Rules); err != nil {
		return nil, fmt.Errorf("information boundary: %w", err)
	}
	return p, nil
}

// CheckBoundaryRules says what is wrong with rules: too many, too long, a
// pattern that isn't a regular expression.
func CheckBoundaryRules(rules []*identityv1.BoundaryRule) error {
	if len(rules) > 100 {
		return errors.New("more than 100 rules")
	}
	for _, r := range rules {
		if r.Category == "" || len(r.Category) > 100 || len(r.Patterns) == 0 || len(r.Patterns) > 200 || len(r.RecipientKinds) > 20 {
			return fmt.Errorf("rule %q: a category (at most 100 bytes), 1 to 200 patterns, at most 20 kinds", r.Category)
		}
		for _, pat := range r.Patterns {
			if len(pat) > 1000 {
				return fmt.Errorf("rule %q: a pattern longer than 1000 bytes", r.Category)
			}
			if _, err := regexp.Compile(pat); err != nil {
				return fmt.Errorf("rule %q: %v", r.Category, err)
			}
		}
	}
	return nil
}

// VerifyContact checks a contact: who the inbox is, and its registration if
// it has one.
func (t *Trust) VerifyContact(c *identityv1.Contact) (*ContactInfo, error) {
	var info ContactInfo
	switch {
	case c.GetCard() != nil:
		card, pub, err := t.VerifyCard(c.Card, c.DeviceCert)
		if err != nil {
			return nil, err
		}
		info = ContactInfo{Inbox: card.ClientId, Name: card.Name, Kind: card.Kind, Key: pub, Category: card.Category, CategoryMachineID: card.CategoryMachineId}
	case c.GetMachineCert() != nil:
		mc, pub, err := t.VerifyMachineCert(c.MachineCert)
		if err != nil {
			return nil, err
		}
		info = ContactInfo{Inbox: mc.MachineId, Kind: "machine", Key: pub}
	default:
		return nil, errors.New("contact: neither a card nor a machine cert")
	}
	if c.Registration != nil {
		reg := &identityv1.InboxRegistration{}
		if err := VerifyWith(c.Registration, info.Key, reg); err != nil {
			return nil, fmt.Errorf("inbox registration: %w", err)
		}
		if reg.OwnerId != t.ownerID || reg.Inbox != info.Inbox || len(reg.InboxPublicKey) != 32 {
			return nil, errors.New("inbox registration: not this inbox's")
		}
		info.InboxKey, info.TakesMessages, info.Tier = reg.InboxPublicKey, reg.TakesMessages, reg.Tier
		if info.Tier == "" { // a registration from before tiers
			info.Tier = TierOf(reg.TakesMessages)
		}
	}
	return &info, nil
}

// SignRegistration signs an inbox's registration with its identity key.
func SignRegistration(key Signer, ownerID, inbox string, inboxKey []byte, takes string) (*identityv1.Signed, error) {
	return Sign(&identityv1.InboxRegistration{OwnerId: ownerID, Inbox: inbox, InboxPublicKey: inboxKey, TakesMessages: takes, Tier: TierOf(takes),
		IssuedAt: timestamppb.Now()}, key)
}

// SameCard reports whether two cards say the same (issued_at aside).
func SameCard(a, b *identityv1.ContactCard) bool {
	return a.OwnerId == b.OwnerId && a.ClientId == b.ClientId && bytes.Equal(a.ClientPublicKey, b.ClientPublicKey) && a.Name == b.Name && a.Kind == b.Kind &&
		a.Category == b.Category && a.CategoryMachineId == b.CategoryMachineId
}

// VerifyRoomCharter checks a room's charter (identity.v1.RoomCharter): signed
// by a device of this owner's, whose certificate the owner's root signed.
func (t *Trust) VerifyRoomCharter(charter, deviceCert *identityv1.Signed) (*identityv1.RoomCharter, error) {
	_, dev, err := t.VerifyDeviceCert(deviceCert)
	if err != nil {
		return nil, fmt.Errorf("room charter: %w", err)
	}
	c := &identityv1.RoomCharter{}
	if err := VerifyWith(charter, dev, c); err != nil {
		return nil, fmt.Errorf("room charter: %w", err)
	}
	if c.OwnerId != t.ownerID {
		return nil, errors.New("room charter: wrong owner")
	}
	if c.RoomId == "" || c.HostMachineId == "" {
		return nil, errors.New("room charter: no room or host")
	}
	return c, nil
}
