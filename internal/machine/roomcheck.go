package machine

import (
	"context"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	relayv1 "github.com/samir2015dzh/aide/gen/go/aide/relay/v1"
	"github.com/samir2015dzh/aide/pkg/identity"
)

// Whether the members picked for a room can be reached now (工单 #159), asked
// for real rather than read from the roster, which says "online" of an agent
// whose inbox was never set up and lets a coding agent whose sign-in is not
// confirmed be picked: a personal agent's grant here and what the post office
// says of its inbox now; a coding agent of this machine's checked again now.

// inboxReach is what the post office says of a personal agent's inbox now.
type inboxReach struct {
	contact    bool // among the owner's contacts, its card verified
	registered bool // its inbox has a key
	// taking: something takes its letters now (taking).
	taking   bool
	lastSeen time.Time
}

// reachInbox asks the post office of an inbox now (not the contacts kept
// for a minute, which it refreshes).
func (n *Machine) reachInbox(ctx context.Context, inbox string) (inboxReach, error) {
	n.mu.Lock()
	conn := n.conn
	n.mu.Unlock()
	if conn == nil {
		return inboxReach{}, errNotConnected
	}
	ms, err := n.messageService(conn)
	if err != nil {
		return inboxReach{}, err
	}
	res, err := ms.Contacts(ctx, &relayv1.ContactsRequest{})
	if err != nil {
		return inboxReach{}, err
	}
	n.contacts.mu.Lock()
	n.contacts.keep(res)
	n.contacts.mu.Unlock()
	var r inboxReach
	var info *identity.ContactInfo
	for _, c := range res.Contacts {
		if i, err := n.trust.VerifyContact(c); err == nil && i.Inbox == inbox {
			info, r.contact, r.registered = i, true, i.InboxKey != nil
		}
	}
	var presence *relayv1.Presence
	for _, p := range res.Presence {
		if p.Inbox == inbox {
			presence = p
			if p.LastSeen != nil {
				r.lastSeen = p.LastSeen.AsTime()
			}
		}
	}
	// A room's letter goes to the agent itself: its own doorbell's ("").
	woken := slices.ContainsFunc(res.Woken, func(w *relayv1.Woken) bool { return w.Inbox == inbox && slices.Contains(w.Names, "") })
	r.taking = info != nil && taking(info, presence, woken)
	return r, nil
}

// taking tells whether something takes an inbox's letters now, as its
// registration says it takes them (the CLI's expectation, docs/agent-messages
// §15): a doorbell or a webhook wakes it; one that takes them in its session
// or its next turn does while it is connected or was active lately; one that
// polls only when it checks: an MCP connection checks with its calls (active
// lately), while a command line connected only carries its commands (the link
// keeps a connection between them), so being online says nothing of it.
func taking(info *identity.ContactInfo, p *relayv1.Presence, woken bool) bool {
	live := p.GetOnline() || p.GetRecent()
	switch {
	case woken || info.TakesMessages == "webhook":
		return true
	case identity.TierOf(info.TakesMessages) != "pull":
		return live
	}
	return info.Kind == "mcp" && live
}

// CheckRoomMembers says of each member whether it can be reached now (工单
// #159). Members of other machines are not this one's to say: they are
// asked of their own machine.
func (n *Machine) CheckRoomMembers(ctx context.Context, members []*machinev1.CreateRoomMember) *machinev1.CheckRoomMembersResponse {
	res := &machinev1.CheckRoomMembersResponse{}
	var checked bool
	for _, m := range members {
		c := &machinev1.RoomMemberCheck{BotKey: m.BotKey}
		res.Members = append(res.Members, c)
		if id, ok := strings.CutPrefix(m.BotKey, "c:"); ok {
			n.checkInbox(ctx, id, c)
			continue
		}
		machineID, agent, _ := strings.Cut(strings.TrimPrefix(m.BotKey, "m:"), "/")
		if !strings.HasPrefix(m.BotKey, "m:") || machineID != n.machineID {
			c.Problem, c.Detail = machinev1.RoomMemberProblem_ROOM_MEMBER_PROBLEM_AGENT_MISSING, "not on this machine"
			continue
		}
		if !checked { // every coding agent here checked once, now
			n.Discover(ctx)
			checked = true
		}
		paused, _ := n.agentPaused(agent)
		n.mu.Lock()
		i, ok := n.infos[agent]
		hidden, paused := n.isHiddenLocked(agent), paused || n.paused
		n.mu.Unlock()
		switch {
		case !ok || !i.Available || hidden:
			c.Problem, c.Detail = machinev1.RoomMemberProblem_ROOM_MEMBER_PROBLEM_AGENT_MISSING, i.Detail
		case paused:
			c.Problem = machinev1.RoomMemberProblem_ROOM_MEMBER_PROBLEM_AGENT_PAUSED
		case i.LoginUnconfirmed:
			c.Problem = machinev1.RoomMemberProblem_ROOM_MEMBER_PROBLEM_AGENT_UNCONFIRMED
		case !i.LoggedIn:
			c.Problem = machinev1.RoomMemberProblem_ROOM_MEMBER_PROBLEM_AGENT_SIGNED_OUT
		}
	}
	return res
}

// checkInbox: a personal agent has a grant here, and an inbox the post
// office says something takes letters from now.
func (n *Machine) checkInbox(ctx context.Context, clientID string, c *machinev1.RoomMemberCheck) {
	if n.grantName(clientID) == "" {
		c.Problem = machinev1.RoomMemberProblem_ROOM_MEMBER_PROBLEM_NO_GRANT
		return
	}
	r, err := n.roomBook().reach(ctx, clientID)
	switch {
	case err != nil:
		c.Problem = machinev1.RoomMemberProblem_ROOM_MEMBER_PROBLEM_RELAY_UNREACHABLE
	case !r.contact:
		c.Problem = machinev1.RoomMemberProblem_ROOM_MEMBER_PROBLEM_NOT_CONTACT
	case !r.registered:
		c.Problem = machinev1.RoomMemberProblem_ROOM_MEMBER_PROBLEM_NO_INBOX
	case !r.taking:
		c.Problem = machinev1.RoomMemberProblem_ROOM_MEMBER_PROBLEM_NOT_TAKING
	}
	if !r.lastSeen.IsZero() {
		c.LastSeen = timestamppb.New(r.lastSeen)
	}
}

func (l *Local) CheckRoomMembers(ctx context.Context, req *machinev1.CheckRoomMembersRequest) *machinev1.CheckRoomMembersResponse {
	return l.n.CheckRoomMembers(ctx, req.Members)
}

func (s *adminService) CheckRoomMembers(ctx context.Context, req *machinev1.CheckRoomMembersRequest) (*machinev1.CheckRoomMembersResponse, error) {
	if err := ownerOnly(ctx); err != nil {
		return nil, err
	}
	return s.n.CheckRoomMembers(ctx, req.Members), nil
}
