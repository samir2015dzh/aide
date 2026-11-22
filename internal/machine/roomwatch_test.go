package machine

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	inboxv1 "github.com/samir2015dzh/aide/gen/go/aide/inbox/v1"
	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	"github.com/samir2015dzh/aide/pkg/identity"
)

// TestRoomWatch (批 H): the post office's word on each copy (工单 #71: sent →
// it took the letter → answered), the limit of bots at work at once (工单
// #70: past it a wake waits, queued, and goes when one is done), a member
// woken and quiet two minutes told of once (工单 #70), and the replies that
// wait at the post office for an away host listed in the status (工单 #74).
func TestRoomWatch(t *testing.T) {
	h := newHarness(t, Limits{})
	n := h.n
	ctx := context.Background()
	var clients []string
	n.mu.Lock()
	for i := range roomMaxBusy + 2 {
		id := fmt.Sprintf("c_%02d", i)
		clients = append(clients, id)
		n.grants = append(n.grants, &identityv1.Grant{GrantId: "g" + id, ClientId: id, ClientLabel: "Bot " + id, MachineIds: []string{"*"}})
	}
	n.mu.Unlock()
	b := n.roomBook()
	var sent []*inboxv1.Message
	b.send = func(m *inboxv1.Message) error { sent = append(sent, m); return nil }
	post := map[string]string{}
	b.states = func(_ context.Context, ids []string) (map[string]string, error) {
		out := map[string]string{}
		for _, id := range ids {
			out[id] = post[id]
		}
		return out, nil
	}

	// A direct room with each: the first roomMaxBusy are woken, the last two wait.
	var rooms []*machinev1.Room
	var msgs []*machinev1.RoomMessage
	for _, c := range clients {
		r, err := n.OpenDirectRoom(c)
		if err != nil {
			t.Fatal(err)
		}
		m, err := n.PostRoomMessage(ctx, &machinev1.PostRoomMessageRequest{RoomId: r.RoomId, Text: "在吗"})
		if err != nil {
			t.Fatal(err)
		}
		rooms, msgs = append(rooms, r), append(msgs, m)
	}
	if len(sent) != roomMaxBusy {
		t.Fatalf("%d woken at once, want %d", len(sent), roomMaxBusy)
	}
	for i, m := range msgs {
		want := machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_SENT
		if i >= roomMaxBusy {
			want = machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_QUEUED
		}
		if m.Deliveries[0].State != want {
			t.Fatalf("room %d: %v, want %v", i, m.Deliveries[0].State, want)
		}
	}
	state := func(i int) *machinev1.RoomDelivery {
		l, _ := n.ListRoomMessages(&machinev1.ListRoomMessagesRequest{RoomId: rooms[i].RoomId})
		return l.Messages[0].Deliveries[0]
	}

	// The post office: the first took it; the second's was refused.
	post[sent[0].MessageId], post[sent[1].MessageId] = "taken", "refused"
	n.roomTick(ctx, time.Now())
	if d := state(0); d.State != machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_TAKEN {
		t.Fatalf("taken: %v", d)
	}
	if d := state(1); d.State != machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_REFUSED || d.Reason == "" {
		t.Fatalf("refused: %v", d)
	}
	// The refused one is no longer at work: the first waiting one went.
	if len(sent) != roomMaxBusy+1 || state(roomMaxBusy).State != machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_SENT || state(roomMaxBusy+1).State != machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_QUEUED {
		t.Fatalf("after one done: %d sent, %v %v", len(sent), state(roomMaxBusy), state(roomMaxBusy+1))
	}
	// An answer: the last waiting one goes.
	info := &identity.ContactInfo{Inbox: clients[0]}
	n.takeRoomLetter(&inboxv1.Message{MessageId: "msg_answer0", To: &inboxv1.Address{Inbox: n.machineID, Session: rooms[0].RoomId}, InReplyTo: sent[0].MessageId, Text: "在"}, info)
	n.drainRooms(ctx)
	if d := state(0); d.State != machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_REPLIED {
		t.Fatalf("replied: %v", d)
	}
	if state(roomMaxBusy+1).State != machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_SENT {
		t.Fatalf("the last one still waits: %v", state(roomMaxBusy+1))
	}

	// Two minutes on: the ones still at work and not taken are told of, once each; the room says so.
	later := time.Now().Add(roomQuiet + time.Second)
	n.roomTick(ctx, later)
	if d := state(2); !d.Reminded {
		t.Fatalf("quiet two minutes, not told: %v", d)
	}
	l, _ := n.ListRoomMessages(&machinev1.ListRoomMessagesRequest{RoomId: rooms[2].RoomId})
	if !strings.Contains(l.Room.Notice, "两分钟") || l.Room.NoticeAt == nil {
		t.Fatalf("the room's notice: %q", l.Room.Notice)
	}
	at := l.Room.NoticeAt.AsTime()
	n.roomTick(ctx, later.Add(time.Minute))
	if l, _ := n.ListRoomMessages(&machinev1.ListRoomMessagesRequest{RoomId: rooms[2].RoomId}); !l.Room.NoticeAt.AsTime().Equal(at) {
		t.Fatal("told twice")
	}
	if d := state(0); d.Reminded {
		t.Fatal("an answered one told of")
	}

	// A reply for a host that is away: it waits in the status until the post office says it was taken.
	n.roomReply("m_host", "room_x", "s1", "claude", "w1", "做完了", "")
	out := n.roomOutboxInStatus()
	if len(out) != 1 || out[0].RoomId != "room_x" || out[0].HostMachineId != "m_host" || out[0].Replies != 1 {
		t.Fatalf("outbox: %v", out)
	}
	for id := range b.outbox {
		post[id] = "waiting"
	}
	n.roomTick(ctx, time.Now())
	if len(n.roomOutboxInStatus()) != 1 {
		t.Fatal("gone while it waits")
	}
	for id := range b.outbox {
		post[id] = "handled"
	}
	n.roomTick(ctx, time.Now())
	if out := n.roomOutboxInStatus(); len(out) != 0 {
		t.Fatalf("still waiting once taken: %v", out)
	}
}
