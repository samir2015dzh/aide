package machine

import (
	"context"
	"strings"
	"testing"
	"time"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	inboxv1 "github.com/samir2015dzh/aide/gen/go/aide/inbox/v1"
	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
)

// TestRoomUntaken (工单 #160): a message to a personal agent that is never taken from the post office does not stay
// "sent: waiting for it" for the post office's month. A day on the owner is told, in the message's line and the room's
// notice (which their devices notify with), once, that it may not arrive; when the post office lets it expire, the
// owner is told again, not only shown it failed. One that was taken is not told of as untaken.
func TestRoomUntaken(t *testing.T) {
	h := newHarness(t, Limits{})
	n := h.n
	ctx := context.Background()
	n.mu.Lock()
	for _, id := range []string{"c_away", "c_here"} {
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
	away, _ := n.OpenDirectRoom("c_away")
	here, _ := n.OpenDirectRoom("c_here")
	for _, r := range []*machinev1.Room{away, here} {
		if _, err := n.PostRoomMessage(ctx, &machinev1.PostRoomMessageRequest{RoomId: r.RoomId, Text: "在吗", LightChat: true}); err != nil {
			t.Fatal(err)
		}
	}
	post[sent[0].MessageId], post[sent[1].MessageId] = "waiting", "taken"
	read := func(r *machinev1.Room) (*machinev1.Room, *machinev1.RoomDelivery) {
		l, _ := n.ListRoomMessages(&machinev1.ListRoomMessagesRequest{RoomId: r.RoomId})
		return l.Room, l.Messages[0].Deliveries[0]
	}

	// Two minutes: quiet, told of; not yet "may not arrive".
	t0 := time.Now()
	n.roomTick(ctx, t0.Add(roomQuiet+time.Second))
	if _, d := read(away); !d.Reminded || d.Untaken || d.State != machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_SENT {
		t.Fatalf("two minutes on: %v", d)
	}

	// A day: still waiting at the post office, so it may not arrive, said once.
	day := t0.Add(roomUntaken + time.Second)
	n.roomTick(ctx, day)
	room, d := read(away)
	if !d.Untaken || d.State != machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_SENT {
		t.Fatalf("a day on: %v", d)
	}
	if !strings.Contains(room.Notice, "Bot c_away") || !strings.Contains(room.Notice, "一天没来取信") || !room.NoticeAt.AsTime().Equal(day) {
		t.Fatalf("the room's notice a day on: %q at %v", room.Notice, room.NoticeAt.AsTime())
	}
	n.roomTick(ctx, day.Add(time.Hour))
	if again, _ := read(away); !again.NoticeAt.AsTime().Equal(room.NoticeAt.AsTime()) {
		t.Fatal("told twice")
	}
	// Taken but not answered: not untaken.
	if _, d := read(here); d.Untaken || d.State != machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_TAKEN {
		t.Fatalf("a taken one a day on: %v", d)
	}

	// The post office let it expire: failed, and the owner told so.
	post[sent[0].MessageId] = "expired"
	month := t0.Add(31 * 24 * time.Hour)
	n.roomTick(ctx, month)
	room, d = read(away)
	if d.State != machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_FAILED || d.Reason == "" {
		t.Fatalf("expired: %v", d)
	}
	if !strings.Contains(room.Notice, "过期") || !room.NoticeAt.AsTime().Equal(month) {
		t.Fatalf("the room's notice once expired: %q", room.Notice)
	}
	if room.Run == machinev1.RoomRun_ROOM_RUN_RUNNING {
		t.Fatal("the room still waits on an expired message")
	}
}
