package owner

import (
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
)

// 工单 #123: a bot has several direct conversations on its host, which lists
// them in no order; the one a bot's page opens on is the newest, every time
// (the audit saw a random one).
func TestNewestDirectRoom(t *testing.T) {
	now := time.Now()
	room := func(id, client string, ago time.Duration, direct, shadow bool) *machinev1.Room {
		return &machinev1.Room{RoomId: id, Direct: direct, Shadow: shadow, UpdatedAt: timestamppb.New(now.Add(-ago)),
			Members: []*machinev1.RoomMember{{BotKey: "c:" + client, Address: client}}}
	}
	rooms := []*machinev1.Room{
		room("room_old", "c_muse", 3*time.Hour, true, false),
		room("room_new", "c_muse", time.Minute, true, false),
		room("room_mid", "c_muse", time.Hour, true, false),
		room("room_group", "c_muse", 0, false, false),
		room("room_other", "c_hermes", 0, true, false),
	}
	// Whichever order the host gives them in.
	for i := range len(rooms) {
		order := append(append([]*machinev1.Room{}, rooms[i:]...), rooms[:i]...)
		if got := newestDirectRoom(order, "c_muse"); got == nil || got.RoomId != "room_new" {
			t.Fatalf("order %d: got %v, want room_new", i, got)
		}
	}
	if got := newestDirectRoom(rooms, "c_nobody"); got != nil {
		t.Fatalf("no direct room with c_nobody, got %v", got)
	}
}
