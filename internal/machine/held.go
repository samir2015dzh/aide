package machine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/samir2015dzh/aide/internal/brandenv"
	"slices"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	inboxv1 "github.com/samir2015dzh/aide/gen/go/aide/inbox/v1"
	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	relayv1 "github.com/samir2015dzh/aide/gen/go/aide/relay/v1"
	"github.com/samir2015dzh/aide/internal/boundary"
)

// Messages held for the owner before they reach a session
// (docs/agent-messages.md §15.5): the owner's ruling (option 丙) holds a
// message from an agent let in less than newAgentDays() ago, and one in a
// chain at hop holdHop or more, to a session that agent did not start
// (what agents write to each other: every DSH session is the owner's); and the owner's information boundary holds
// one that matches a rule for the session's coding agent. Nothing else is
// configurable. They wait here, on this machine (the post office only
// knows them "held"), until the owner releases one to its session or
// discards it, from one of their devices; a day later, unreleased, they
// expire. Only messages are held: no action waits for approval here.

const (
	holdHop = 3
	heldFor = 24 * time.Hour
	heldKey = "held-messages"
	maxHeld = 500
)

// newAgentDays: an agent let in fewer days ago than this is new. 7, the
// owner's to rule (no setting); AIDE_TEST_NEW_AGENT_DAYS changes it for
// Aide's own tests only, whose agents are all let in a moment before.
func newAgentDays() int {
	if v, err := strconv.Atoi(brandenv.Get("TEST_NEW_AGENT_DAYS")); err == nil && v >= 0 {
		return v
	}
	return 7
}

// heldMessage is a message waiting for the owner.
type heldMessage struct {
	Message  []byte    `json:"message"` // inboxv1.Message
	ClientID string    `json:"client_id"`
	Name     string    `json:"name"`
	Reason   string    `json:"reason"`
	Detail   string    `json:"detail"`
	HeldAt   time.Time `json:"held_at"`
	// PostOffice: it came from the post office, which is told what became
	// of it (one handed over the tunnel never reached it).
	PostOffice bool `json:"post_office"`
}

func (h *heldMessage) message() *inboxv1.Message {
	m := &inboxv1.Message{}
	proto.Unmarshal(h.Message, m)
	return m
}

var errBoundaryOut = errors.New("refused: boundary")

// boundaryError is a message a sender refuses: it matches the owner's
// information boundary in category.
type boundaryError struct{ category string }

func (e *boundaryError) Error() string {
	return fmt.Sprintf("refused: boundary (%s): the owner's information boundary keeps this out of messages to this recipient", e.category)
}
func (e *boundaryError) Unwrap() error { return errBoundaryOut }

// boundary is the owner's information boundary, as the post office last
// handed it with the contacts (fetched again when older than a minute);
// nil when there is none or it does not verify.
func (n *Machine) boundary() *boundary.Checker {
	n.contacts.mu.Lock()
	stale := time.Since(n.contacts.at) > time.Minute
	n.contacts.mu.Unlock()
	n.mu.Lock()
	conn := n.conn
	n.mu.Unlock()
	if stale && conn != nil {
		if ms, err := n.messageService(conn); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			res, err := ms.Contacts(ctx, &relayv1.ContactsRequest{})
			cancel()
			if err == nil {
				n.contacts.mu.Lock()
				n.contacts.keep(res)
				n.contacts.mu.Unlock()
			}
		}
	}
	n.contacts.mu.Lock()
	defer n.contacts.mu.Unlock()
	if b := n.contacts.boundary; b != n.contacts.checkerOf {
		c, err := boundary.FromContacts(n.trust, b)
		if err != nil {
			n.log.Warn("the owner's information boundary does not verify", "err", err)
		}
		n.contacts.checker, n.contacts.checkerOf = c, b
	}
	return n.contacts.checker
}

// sessionKind is the kind of recipient a session is, for the boundary: the
// coding agent it runs; "deepseek" for a DSH session.
func (n *Machine) sessionKind(session string) string {
	if strings.HasPrefix(session, "dsh_") {
		return "deepseek"
	}
	if r, err := n.st.Session(session); err == nil && r != nil {
		return r.Agent
	}
	return ""
}

// holdWhy says why to hold a message from g for its session, and in words;
// "" to let it through.
func (n *Machine) holdWhy(g *identityv1.Grant, m *inboxv1.Message, now time.Time) (reason, detail string) {
	if cat := n.boundary().Check(m.Text, n.sessionKind(m.GetTo().GetSession())); cat != "" {
		return "boundary", "it matches the owner's information boundary: " + cat
	}
	// The owner's ruling is for what agents write to each other: a
	// session an agent started itself takes its words as its own work.
	if r, err := n.st.Session(m.GetTo().GetSession()); err == nil && r != nil && r.CreatedBy == g.GetClientId() {
		return "", ""
	}
	if f, days := g.GetFirstIssuedAt(), newAgentDays(); f != nil && now.Sub(f.AsTime()) < time.Duration(days)*24*time.Hour {
		return "new_agent", fmt.Sprintf("%s was let in less than %d days ago", firstNonEmptyStr(g.GetClientLabel(), g.GetClientId()), days)
	}
	if m.Hop >= holdHop {
		return "long_chain", fmt.Sprintf("it is hop %d of a chain of messages between agents", m.Hop)
	}
	return "", ""
}

func firstNonEmptyStr(a ...string) string {
	for _, s := range a {
		if s != "" {
			return s
		}
	}
	return ""
}

// hold keeps a message for the owner; false when it is kept already.
func (n *Machine) hold(m *inboxv1.Message, clientID, name, reason, detail string, postOffice bool) (bool, error) {
	b, err := proto.Marshal(m)
	if err != nil {
		return false, err
	}
	n.heldMu.Lock()
	defer n.heldMu.Unlock()
	list := n.heldList()
	for _, h := range list {
		if h.message().MessageId == m.MessageId {
			return false, nil
		}
	}
	if len(list) >= maxHeld {
		return false, cerr(connect.CodeResourceExhausted, "%d messages wait for the owner already", maxHeld)
	}
	list = append(list, &heldMessage{Message: b, ClientID: clientID, Name: name, Reason: reason, Detail: detail, HeldAt: time.Now().UTC(), PostOffice: postOffice})
	if err := n.saveHeld(list); err != nil {
		return false, err
	}
	n.audit.Log(AuditRecord{Kind: "message.held", Subject: clientID, Session: m.GetTo().GetSession(), Decision: "hold", Detail: "message=" + m.MessageId + " reason=" + reason})
	n.log.Info("a message held for the owner", "message", m.MessageId, "session", m.GetTo().GetSession(), "reason", reason)
	return true, nil
}

// heldList reads the queue (heldMu held).
func (n *Machine) heldList() []*heldMessage {
	var list []*heldMessage
	if b, _ := n.st.Get(heldKey); b != nil {
		json.Unmarshal(b, &list)
	}
	return list
}

func (n *Machine) saveHeld(list []*heldMessage) error {
	b, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return n.st.Set(heldKey, b)
}

// HeldMessages lists the messages waiting for the owner, oldest first,
// expiring those a day old first.
func (n *Machine) HeldMessages() *machinev1.ListHeldMessagesResponse {
	n.expireHeld(time.Now())
	n.heldMu.Lock()
	defer n.heldMu.Unlock()
	res := &machinev1.ListHeldMessagesResponse{}
	for _, h := range n.heldList() {
		m := h.message()
		res.Messages = append(res.Messages, &machinev1.HeldMessage{MessageId: m.MessageId, From: inboxAddress(m.From), FromName: h.Name, SessionId: m.GetTo().GetSession(),
			Text: m.Text, Subject: m.Subject, Reason: h.Reason, Detail: h.Detail, HeldAt: timestamppb.New(h.HeldAt), ExpiresAt: timestamppb.New(h.HeldAt.Add(heldFor)), Hop: m.Hop})
	}
	return res
}

// take removes a held message from the queue.
func (n *Machine) takeHeld(id string) *heldMessage {
	n.heldMu.Lock()
	defer n.heldMu.Unlock()
	list := n.heldList()
	i := slices.IndexFunc(list, func(h *heldMessage) bool { return h.message().MessageId == id })
	if i < 0 {
		return nil
	}
	h := list[i]
	list = slices.Delete(list, i, i+1)
	n.saveHeld(list)
	return h
}

// ReleaseHeldMessage releases a held message to its session, or discards
// it: its sender is told, and the post office learns what became of it.
func (n *Machine) ReleaseHeldMessage(ctx context.Context, id string, release bool) error {
	h := n.takeHeld(id)
	if h == nil {
		return cerr(connect.CodeNotFound, "no message %s waits for the owner here", id)
	}
	m := h.message()
	if !release {
		n.audit.Log(AuditRecord{Kind: "message.held", Subject: "owner", Session: m.GetTo().GetSession(), Decision: "deny", Detail: "message=" + m.MessageId + " discarded"})
		n.ackHeld(h, "refused")
		n.bounce(m, "the owner discarded it")
		return nil
	}
	var g *identityv1.Grant
	for _, x := range n.Local().Grants() {
		if x.ClientId == h.ClientID {
			g = x
		}
	}
	if g == nil {
		n.ackHeld(h, "refused")
		n.bounce(m, "its sender's grant no longer covers this machine")
		return cerr(connect.CodeFailedPrecondition, "its sender's grant no longer covers this machine: discarded")
	}
	n.audit.Log(AuditRecord{Kind: "message.held", Subject: "owner", Session: m.GetTo().GetSession(), Decision: "allow", Detail: "message=" + m.MessageId + " released"})
	_, _, err := n.deliverLetter(ctx, &Principal{Kind: PrincipalClient, ClientID: g.ClientId, Grant: g}, m, h.Name, nil)
	if err != nil && connect.CodeOf(err) != connect.CodeAlreadyExists {
		n.ackHeld(h, "refused")
		n.bounce(m, err.Error())
		return err
	}
	n.ackHeld(h, "handed")
	if h.PostOffice {
		if id := n.dshOf(m.To.Session); id != "" {
			n.watchDSHTurn(id, m.To.Session, m.MessageId)
		} else {
			n.handedToTurn(m.To.Session, m.MessageId)
		}
	}
	return nil
}

// ackHeld tells the post office what became of a held message from it.
func (n *Machine) ackHeld(h *heldMessage, outcome string) {
	if !h.PostOffice {
		return
	}
	n.mu.Lock()
	conn := n.conn
	n.mu.Unlock()
	if conn == nil {
		return
	}
	id := h.message().MessageId
	go func() {
		ms, err := n.messageService(conn)
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if _, err := ms.Ack(ctx, &relayv1.AckMessagesRequest{Acks: []*relayv1.MessageAck{{MessageId: id, Outcome: outcome}}}); err != nil {
			n.log.Warn("acknowledging a held message", "message", id, "err", err)
		}
	}()
}

// expireHeld drops the messages held a day: expired, their senders told.
func (n *Machine) expireHeld(now time.Time) {
	n.heldMu.Lock()
	list := n.heldList()
	var keep, gone []*heldMessage
	for _, h := range list {
		if now.Sub(h.HeldAt) >= heldFor {
			gone = append(gone, h)
		} else {
			keep = append(keep, h)
		}
	}
	if len(gone) > 0 {
		n.saveHeld(keep)
	}
	n.heldMu.Unlock()
	for _, h := range gone {
		m := h.message()
		n.audit.Log(AuditRecord{Kind: "message.held", Subject: "owner", Session: m.GetTo().GetSession(), Decision: "expire", Detail: "message=" + m.MessageId})
		n.ackHeld(h, "expired")
		n.tell(m, "Expired", fmt.Sprintf("Your message %s to %s was held for the owner and expired: not released within a day", m.MessageId, inboxAddress(m.To)))
	}
}

// serveHeld expires held messages while the machine runs.
func (n *Machine) serveHeld(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Minute):
			n.expireHeld(time.Now())
		}
	}
}

func (s *adminService) ListHeldMessages(ctx context.Context, _ *machinev1.ListHeldMessagesRequest) (*machinev1.ListHeldMessagesResponse, error) {
	if err := ownerOnly(ctx); err != nil {
		return nil, err
	}
	return s.n.HeldMessages(), nil
}

func (s *adminService) ReleaseHeldMessage(ctx context.Context, req *machinev1.ReleaseHeldMessageRequest) (*machinev1.ReleaseHeldMessageResponse, error) {
	if err := ownerOnly(ctx); err != nil {
		return nil, err
	}
	if err := s.n.ReleaseHeldMessage(ctx, req.MessageId, req.Release); err != nil {
		return nil, err
	}
	return &machinev1.ReleaseHeldMessageResponse{}, nil
}
