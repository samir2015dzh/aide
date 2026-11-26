package machine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"connectrpc.com/connect"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	inboxv1 "github.com/samir2015dzh/aide/gen/go/aide/inbox/v1"
	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
)

// What the owner reads when a message did not reach an agent (工单 #139
// P01): who, why in words, what to do; never the error's own words or IDs.
func TestDeliveryFailure(t *testing.T) {
	for _, c := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("c_wmexon2lzsr2bmydpo3n4uka has no inbox yet: %w", errNoInbox), "它那边的 Aide 还收不了消息"},
		{fmt.Errorf("c_x is not among the owner's contacts: %w", errNotContact), "已不在你的联系人里"},
		{errNotConnected, "连不上中继"},
		{connect.NewError(connect.CodeUnavailable, errors.New("dial tcp: i/o timeout")), "连不上中继"},
		{fmt.Errorf("sending: %w", context.DeadlineExceeded), "连不上中继"},
		{errors.New("permission_denied: sealed to another key"), "可以稍后重发"},
	} {
		got := deliveryFailure("Hermes", c.err)
		if !strings.HasPrefix(got, "没送到 Hermes：") || !strings.Contains(got, c.want) {
			t.Errorf("%v: %q", c.err, got)
		}
		for _, raw := range []string{"c_", "inbox", "contacts", "dial", "permission", "deadline"} {
			if strings.Contains(got, raw) {
				t.Errorf("%v: %q shows %q", c.err, got, raw)
			}
		}
	}
}

// A direct room's message that could not be handed on (工单 #139 P01/P02):
// failed, said in words with what to do, never the error's own words or
// IDs; sent again, it is a new message, the failed one kept as it went.
func TestDirectRoomFailureInWords(t *testing.T) {
	h := newHarness(t, Limits{})
	n := h.n
	ctx := context.Background()
	n.mu.Lock()
	n.grants = append(n.grants, &identityv1.Grant{GrantId: "g_h", ClientId: "c_wmexon2lzsr2bmydpo3n4uka", ClientLabel: "Hermes Agent", MachineIds: []string{"*"}})
	n.mu.Unlock()
	fail := true
	n.roomBook().send = func(m *inboxv1.Message) error {
		if fail {
			return fmt.Errorf("%s has no inbox yet: %w", m.To.Inbox, errNoInbox)
		}
		return nil
	}
	r, err := n.OpenDirectRoom("c_wmexon2lzsr2bmydpo3n4uka")
	if err != nil {
		t.Fatal(err)
	}
	m, err := n.PostRoomMessage(ctx, &machinev1.PostRoomMessageRequest{RoomId: r.RoomId, Text: "在吗", IdempotencyKey: "k1"})
	if err != nil {
		t.Fatal(err)
	}
	d := m.Deliveries[0]
	if d.State != machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_FAILED || !strings.HasPrefix(d.Reason, "没送到 Hermes Agent：") || strings.Contains(d.Reason, "c_") || strings.Contains(d.Reason, "inbox") {
		t.Fatalf("failed: %v", d)
	}
	fail = false
	again, err := n.PostRoomMessage(ctx, &machinev1.PostRoomMessageRequest{RoomId: r.RoomId, Text: "在吗", IdempotencyKey: "k2"})
	if err != nil || again.MessageId == m.MessageId || again.Deliveries[0].State != machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_SENT {
		t.Fatalf("sent again: %v %v", again, err)
	}
	l, _ := n.ListRoomMessages(&machinev1.ListRoomMessagesRequest{RoomId: r.RoomId})
	if len(l.Messages) != 2 || l.Messages[0].Deliveries[0].State != machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_FAILED {
		t.Fatalf("the room: %v", l.Messages)
	}
}
