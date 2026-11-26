package owner

import (
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
)

// 工单 #164: each of the owner's machines keeps its own copy of a shadow
// conversation; the owner's devices see one, put together by message id in
// the post office's order, so a machine that missed some (it was away past
// the post office's keeping, or its copy never came) shows nothing missing
// while another has it.
func TestShadowCopiesAreOne(t *testing.T) {
	at := func(m int) *timestamppb.Timestamp {
		return timestamppb.New(time.Unix(1_800_000_000, 0).Add(time.Duration(m) * time.Minute))
	}
	msg := func(id string, m int) *machinev1.RoomMessage { return &machinev1.RoomMessage{MessageId: id, At: at(m)} }
	work := &machinev1.ListRoomMessagesResponse{Room: &machinev1.Room{RoomId: "room_shadow_1", Shadow: true, UpdatedAt: at(3)}, Messages: []*machinev1.RoomMessage{msg("a", 1), msg("c", 3)}}
	home := &machinev1.ListRoomMessagesResponse{Room: &machinev1.Room{RoomId: "room_shadow_1", Shadow: true, UpdatedAt: at(4), LastLine: "latest"}, Messages: []*machinev1.RoomMessage{msg("a", 1), msg("b", 2), msg("d", 4)}}
	got := mergeShadow([]*machinev1.ListRoomMessagesResponse{work, home}, 0)
	var ids []string
	for _, m := range got.Messages {
		ids = append(ids, m.MessageId)
	}
	if want := "a b c d"; strings.Join(ids, " ") != want || got.Room.LastLine != "latest" {
		t.Fatalf("merged %v (room %q), want %s", ids, got.Room.LastLine, want)
	}
	if last := mergeShadow([]*machinev1.ListRoomMessagesResponse{work, home}, 2).Messages; len(last) != 2 || last[0].MessageId != "c" {
		t.Fatalf("limit: %v", last)
	}

	rooms := oneShadowEach([]*machinev1.Room{
		{RoomId: "room_shadow_1", Shadow: true, UpdatedAt: at(3)},
		{RoomId: "room_x", UpdatedAt: at(1)},
		{RoomId: "room_shadow_1", Shadow: true, UpdatedAt: at(4), LastLine: "latest"},
	})
	if len(rooms) != 2 || rooms[0].LastLine != "latest" || rooms[1].RoomId != "room_x" {
		t.Fatalf("rooms: %v", rooms)
	}
}
