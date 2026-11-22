package machine

import (
	"context"
	"strings"
	"testing"
	"time"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	inboxv1 "github.com/samir2015dzh/aide/gen/go/aide/inbox/v1"
	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	"github.com/samir2015dzh/aide/internal/boundary"
	"github.com/samir2015dzh/aide/pkg/identity"
)

// TestDirectRoom (ROOMS-DESIGN, G2-a): the owner's conversation with a personal
// agent, kept on its host. Made once; the owner's words go to the agent as its
// own message (one recipient, a chain of its own, the room's address to
// answer to, the earlier messages for context); an answer to it comes back into
// the room and marks it replied; a repeat is kept once; the same idempotency
// key says it once; the owner's information boundary for the agent's kind
// keeps its copy back, saying so.
func TestDirectRoom(t *testing.T) {
	h := newHarness(t, Limits{})
	n := h.n
	ctx := context.Background()
	n.mu.Lock()
	n.grants = append(n.grants,
		&identityv1.Grant{GrantId: "g_muse", ClientId: "c_muse", ClientLabel: "Muse", MachineIds: []string{"*"}},
		&identityv1.Grant{GrantId: "g_cl", ClientId: "c_claude", ClientLabel: "Claude (claude.ai)", MachineIds: []string{"*"}})
	n.mu.Unlock()
	var sent []*inboxv1.Message
	n.roomBook().send = func(m *inboxv1.Message) error { sent = append(sent, m); return nil }

	if _, err := n.OpenDirectRoom("c_nobody"); err == nil {
		t.Fatal("a room with an agent that has no grant here")
	}
	r, err := n.OpenDirectRoom("c_muse")
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := n.OpenDirectRoom("c_muse"); again.RoomId != r.RoomId || !r.Direct || r.Name != "Muse" || r.HostMachineId != n.machineID {
		t.Fatalf("one direct room per agent: %v %v", r, again)
	}

	m1, err := n.PostRoomMessage(ctx, &machinev1.PostRoomMessageRequest{RoomId: r.RoomId, Text: "看一下今天的 CI", IdempotencyKey: "k1"})
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := n.PostRoomMessage(ctx, &machinev1.PostRoomMessageRequest{RoomId: r.RoomId, Text: "看一下今天的 CI", IdempotencyKey: "k1"}); again.MessageId != m1.MessageId || len(sent) != 1 {
		t.Fatalf("said twice: %v, %d sent", again, len(sent))
	}
	d := m1.Deliveries[0]
	out := sent[0]
	if d.State != machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_SENT || d.CopyId != out.MessageId || out.To.Inbox != "c_muse" ||
		out.From.Inbox != n.machineID || out.From.Session != r.RoomId || out.Room.GetRoomId() != r.RoomId || out.Hop != 1 || out.TraceId == "" {
		t.Fatalf("its copy: %v %v", d, out)
	}
	if !strings.Contains(out.Text, "所有者说：\n\n看一下今天的 CI") || !strings.Contains(out.Text, "in_reply_to") {
		t.Fatalf("what it reads: %s", out.Text)
	}

	// Its answer, to the room's address, in reply to its copy.
	info := &identity.ContactInfo{Inbox: "c_muse", Name: "Muse"}
	answer := &inboxv1.Message{MessageId: "msg_01answer", From: &inboxv1.Address{Inbox: "c_muse"}, To: &inboxv1.Address{Inbox: n.machineID, Session: r.RoomId}, InReplyTo: out.MessageId, Text: "CI 全绿"}
	if got := n.takeRoomLetter(answer, info); got != "handled" {
		t.Fatalf("answer: %s", got)
	}
	if got := n.takeRoomLetter(answer, info); got != "handled" {
		t.Fatalf("a repeat: %s", got)
	}
	l, _ := n.ListRoomMessages(&machinev1.ListRoomMessagesRequest{RoomId: r.RoomId})
	if len(l.Messages) != 2 || l.Messages[1].From != "c:c_muse" || l.Messages[1].FromName != "Muse" || l.Messages[1].InReplyTo != m1.MessageId ||
		l.Messages[0].Deliveries[0].State != machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_REPLIED {
		t.Fatalf("the room: %v", l.Messages)
	}
	// Someone not in the room: refused.
	if got := n.takeRoomLetter(&inboxv1.Message{MessageId: "msg_01other", To: answer.To}, &identity.ContactInfo{Inbox: "c_claude"}); got != "refused" {
		t.Fatalf("a stranger: %s", got)
	}

	// The next one carries the conversation so far.
	n.PostRoomMessage(ctx, &machinev1.PostRoomMessageRequest{RoomId: r.RoomId, Text: "好，谢谢"})
	if txt := sent[len(sent)-1].Text; !strings.Contains(txt, "[Muse] CI 全绿") || !strings.Contains(txt, "[所有者] 看一下今天的 CI") {
		t.Fatalf("no context: %s", txt)
	}

	// The boundary, for a Claude-kind agent only (placeholder rules): its copy is refused, said so.
	n.contacts.mu.Lock()
	n.contacts.at = time.Now()
	n.contacts.checker = boundary.Compile(&identityv1.BoundaryPolicy{Rules: []*identityv1.BoundaryRule{
		{Category: "example", Patterns: []string{`placeholder-secret`}, RecipientKinds: []string{"claude"}}}})
	n.contacts.mu.Unlock()
	rc, _ := n.OpenDirectRoom("c_claude")
	before := len(sent)
	m, _ := n.PostRoomMessage(ctx, &machinev1.PostRoomMessageRequest{RoomId: rc.RoomId, Text: "placeholder-secret 在这里"})
	if d := m.Deliveries[0]; d.State != machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_REFUSED || !strings.Contains(d.Reason, "信息边界（example）") || len(sent) != before {
		t.Fatalf("to Claude: %v, %d sent", d, len(sent)-before)
	}
	if m, _ := n.PostRoomMessage(ctx, &machinev1.PostRoomMessageRequest{RoomId: r.RoomId, Text: "placeholder-secret 在这里"}); m.Deliveries[0].State != machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_SENT {
		t.Fatalf("not Claude: %v", m.Deliveries[0])
	}

	// Kept: read again by a machine opened on the same data.
	b := &roomBook{dir: n.roomBook().dir, rooms: map[string]*roomState{}}
	b.load(n)
	if st := b.rooms[r.RoomId]; st == nil || len(st.msgs) != 4 || st.keys["k1"] != m1.MessageId {
		t.Fatalf("not kept: %v", st)
	}
}
