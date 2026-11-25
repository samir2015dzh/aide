package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
)

// 工单 #124 E2E: two real personal agents (Muse and Hermes) exchange 1:1
// mailbox messages and acknowledge them; the owner reads the complete
// chronological read-only shadow conversation through the owner daemon while
// the relay SQLite database holds ZERO plaintext.
func TestShadowConversationAndZeroPlaintextRelay(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	ctx := context.Background()
	w := newOwnerMac(t, []string{"AIDE_FAKE_PROVIDERS=1"}, &localv1.GrantSpec{})
	muse := w.addAgent("Muse", &localv1.GrantSpec{})
	hermes := w.addAgent("Hermes", &localv1.GrantSpec{})
	aide := filepath.Join(w.bin, "aide")

	// Wait until both agents have registered contacts/inboxes visible to each other.
	eventually(t, 30*time.Second, "Muse sees Hermes in contacts", func() bool {
		r := run(t, muse, 15*time.Second, aide, "message", "contacts", "-o", "json")
		return r.code == 0 && strings.Contains(r.stdout, "Hermes")
	})

	reqMarker := "SHADOW_REQ_PLAINTEXT_CHECK_124"
	repMarker := "SHADOW_REP_PLAINTEXT_CHECK_124"

	// 1. Muse sends a direct 1:1 message to Hermes.
	r := run(t, muse, 30*time.Second, aide, "message", "send", "--to", "Hermes", "--subject", "两件套同步", "--message", reqMarker, "-o", "json")
	if r.code != 0 {
		t.Fatalf("Muse send to Hermes failed: %+v", r)
	}
	var sentDoc struct {
		Message struct{ ID string } `json:"message"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &sentDoc); err != nil || sentDoc.Message.ID == "" {
		t.Fatalf("parse Muse send: %v %s", err, r.stdout)
	}
	reqID := sentDoc.Message.ID

	// 2. Hermes reads and acknowledges the message, then replies to Muse.
	eventually(t, 30*time.Second, "Hermes receives Muse message", func() bool {
		lr := run(t, hermes, 15*time.Second, aide, "message", "list", "-o", "json")
		return lr.code == 0 && strings.Contains(lr.stdout, reqMarker)
	})
	if ar := run(t, hermes, 15*time.Second, aide, "message", "ack", reqID); ar.code != 0 {
		t.Fatalf("Hermes ack failed: %+v", ar)
	}
	rr := run(t, hermes, 30*time.Second, aide, "message", "send", "--to", "Muse", "--reply-to", reqID, "--subject", "Re: 两件套同步", "--message", repMarker, "-o", "json")
	if rr.code != 0 {
		t.Fatalf("Hermes reply to Muse failed: %+v", rr)
	}
	var repDoc struct {
		Message struct{ ID string } `json:"message"`
	}
	if err := json.Unmarshal([]byte(rr.stdout), &repDoc); err != nil || repDoc.Message.ID == "" {
		t.Fatalf("parse Hermes reply: %v %s", err, rr.stdout)
	}
	eventually(t, 30*time.Second, "Muse receives Hermes reply", func() bool {
		lr := run(t, muse, 15*time.Second, aide, "message", "list", "-o", "json")
		return lr.code == 0 && strings.Contains(lr.stdout, repMarker)
	})
	if ar := run(t, muse, 15*time.Second, aide, "message", "ack", repDoc.Message.ID); ar.code != 0 {
		t.Fatalf("Muse ack failed: %+v", ar)
	}

	// 3. Owner reads the read-only shadow conversation even AFTER both agents acked.
	var shadow *machinev1.Room
	var msgs []*machinev1.RoomMessage
	eventually(t, 30*time.Second, "owner sees both acked messages in shadow room", func() bool {
		res, err := w.app.client.ListRooms(ctx, &localv1.ListRoomsRequest{})
		if err != nil {
			return false
		}
		for _, rm := range res.Rooms {
			if rm.Shadow && strings.Contains(rm.Name, "Muse") && strings.Contains(rm.Name, "Hermes") {
				shadow = rm
				ml, err := w.app.client.ListRoomMessages(ctx, &localv1.ListRoomMessagesRequest{MachineId: rm.HostMachineId, RoomId: rm.RoomId})
				if err == nil && len(ml.Messages) == 2 {
					msgs = ml.Messages
					return true
				}
			}
		}
		return false
	})
	if msgs[0].FromName != "Muse" || msgs[0].ToName != "Hermes" || msgs[0].Subject != "两件套同步" || msgs[0].Text != reqMarker {
		t.Fatalf("unexpected shadow msg 0: %+v", msgs[0])
	}
	if msgs[1].FromName != "Hermes" || msgs[1].ToName != "Muse" || msgs[1].InReplyTo != reqID || msgs[1].Text != repMarker {
		t.Fatalf("unexpected shadow msg 1: %+v", msgs[1])
	}

	// 4. Shadow conversation is strictly read-only for the owner.
	if _, err := w.app.client.PostRoomMessage(ctx, &localv1.PostRoomMessageRequest{
		MachineId: shadow.HostMachineId, RoomId: shadow.RoomId, Text: "cannot post", IdempotencyKey: "ro-1",
	}); err == nil {
		t.Fatal("expected PostRoomMessage on shadow conversation to fail")
	}

	// 5. Sealed guarantee: relay storage contains ZERO plaintext of either message.
	relayDir := filepath.Join(w.tmp, "relay")
	inspected := 0
	filepath.Walk(relayDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		inspected++
		if bytes.Contains(b, []byte(reqMarker)) || bytes.Contains(b, []byte(repMarker)) {
			t.Fatalf("SECURITY VIOLATION: relay file %s contains plaintext agent message", path)
		}
		return nil
	})
	if inspected == 0 {
		t.Fatalf("expected relay storage files under %s to inspect", relayDir)
	}
}
