package machine

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	inboxv1 "github.com/samir2015dzh/aide/gen/go/aide/inbox/v1"
	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	"github.com/samir2015dzh/aide/pkg/identity"
)

// 工单 #123: 100-turn room bounded context length comparison (raw full history
// vs bounded task context vs light-chat context) and new-conversation isolation.
func TestRoomBoundedContextAndLightChat123(t *testing.T) {
	h := newHarness(t, Limits{})
	n := h.n
	ctx := context.Background()
	n.mu.Lock()
	n.grants = append(n.grants, &identityv1.Grant{GrantId: "g_muse", ClientId: "c_muse", ClientLabel: "Muse", MachineIds: []string{"*"}})
	n.mu.Unlock()
	var sent []*inboxv1.Message
	n.roomBook().send = func(m *inboxv1.Message) error { sent = append(sent, m); return nil }

	r, err := n.OpenDirectRoom("c_muse")
	if err != nil {
		t.Fatal(err)
	}
	// Populate a 100-turn room (~500 runes per message = ~50,000 runes total history).
	b := n.roomBook()
	rawHistoryRunes := 0
	b.mu.Lock()
	st := b.rooms[r.RoomId]
	for i := 1; i <= 100; i++ {
		from, name := "owner", ""
		if i%2 == 0 {
			from, name = "c:c_muse", "Muse"
		}
		txt := fmt.Sprintf("第 %03d 轮讨论内容：%s", i, strings.Repeat("历史记录正文", 80))
		rawHistoryRunes += len([]rune(txt))
		st.msgs = append(st.msgs, &machinev1.RoomMessage{
			MessageId: fmt.Sprintf("msg_%026d", i),
			RoomId:    r.RoomId,
			From:      from,
			FromName:  name,
			Text:      txt,
			At:        timestamppb.New(time.Now().Add(-time.Duration(101-i) * time.Minute)),
		})
	}
	b.mu.Unlock()

	// 1. Standard/task turn on the 100-turn room: strictly bounded + older summary.
	tTask := time.Now()
	if _, err := n.PostRoomMessage(ctx, &machinev1.PostRoomMessageRequest{RoomId: r.RoomId, Text: "请总结一下下一步计划", IdempotencyKey: "task-101"}); err != nil {
		t.Fatal(err)
	}
	taskDur := time.Since(tTask)
	taskPayload := sent[len(sent)-1]
	taskRunes := len([]rune(taskPayload.Text))
	if !strings.Contains(taskPayload.Text, "[更早摘要]") || !strings.Contains(taskPayload.Text, "此前另有") {
		t.Fatalf("expected compact older-turn summary in task payload: %s", taskPayload.Text)
	}
	if taskRunes > 2400 {
		t.Fatalf("task payload exceeded bound: %d runes (raw history %d runes)", taskRunes, rawHistoryRunes)
	}
	// Old turn 1 body must NOT be dragged verbatim.
	if strings.Contains(taskPayload.Text, "第 001 轮讨论内容："+strings.Repeat("历史记录正文", 40)) {
		t.Fatalf("turn 1 body dragged verbatim into turn 101 payload")
	}

	// 2. Light-chat turn on the same 100-turn room: tiny context, direct framing, [轻聊] subject.
	tLight := time.Now()
	if _, err := n.PostRoomMessage(ctx, &machinev1.PostRoomMessageRequest{RoomId: r.RoomId, Text: "在吗？午饭吃啥", IdempotencyKey: "light-102", LightChat: true}); err != nil {
		t.Fatal(err)
	}
	lightDur := time.Since(tLight)
	lightPayload := sent[len(sent)-1]
	lightRunes := len([]rune(lightPayload.Text))
	if !strings.HasPrefix(lightPayload.Subject, "[轻聊]") {
		t.Fatalf("expected [轻聊] subject prefix, got %q", lightPayload.Subject)
	}
	if !strings.Contains(lightPayload.Text, "所有者轻聊") || strings.Contains(lightPayload.Text, "[更早摘要]") {
		t.Fatalf("light chat payload framing wrong: %s", lightPayload.Text)
	}
	if lightRunes > 600 || lightRunes >= taskRunes/2 {
		t.Fatalf("light chat payload should be tiny: light=%d runes vs task=%d runes", lightRunes, taskRunes)
	}
	t.Logf("#123 100-turn context comparison: rawHistory=%d runes -> boundedTask=%d runes (%v) -> lightChat=%d runes (%v)",
		rawHistoryRunes, taskRunes, taskDur, lightRunes, lightDur)

	// 3. New conversation with the same bot: fresh direct room with ZERO old history,
	// while old room remains preserved and readable.
	r2, err := n.CreateRoom(&machinev1.CreateRoomRequest{
		Name:    "Muse · 新对话",
		Members: []*machinev1.CreateRoomMember{{BotKey: "c:c_muse", Name: "Muse"}},
		Mode:    machinev1.RoomMode_ROOM_MODE_LIGHT,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !r2.Direct || r2.RoomId == r.RoomId {
		t.Fatalf("expected fresh direct room, got %+v (old %s)", r2, r.RoomId)
	}
	if _, err := n.PostRoomMessage(ctx, &machinev1.PostRoomMessageRequest{RoomId: r2.RoomId, Text: "全新话题开始", IdempotencyKey: "new-1"}); err != nil {
		t.Fatal(err)
	}
	newPayload := sent[len(sent)-1]
	if strings.Contains(newPayload.Text, "第 100 轮") || strings.Contains(newPayload.Text, "下一步计划") {
		t.Fatalf("new conversation leaked old room context: %s", newPayload.Text)
	}
	if !strings.Contains(newPayload.Text, "所有者轻聊") {
		t.Fatalf("room created in ROOM_MODE_LIGHT should default to light chat: %s", newPayload.Text)
	}
	// Old room is still preserved and readable; OpenDirectRoom returns newest (r2).
	if oldMsgs, err := n.ListRoomMessages(&machinev1.ListRoomMessagesRequest{RoomId: r.RoomId}); err != nil || len(oldMsgs.Messages) < 100 {
		t.Fatalf("old room lost history: err=%v len=%d", err, len(oldMsgs.GetMessages()))
	}
	if latest, err := n.OpenDirectRoom("c_muse"); err != nil || latest.RoomId != r2.RoomId {
		t.Fatalf("OpenDirectRoom should return newest direct room %s, got %+v (%v)", r2.RoomId, latest, err)
	}
}

// 工单 #124: read-only shadow conversation aggregating 1:1 agent mailbox messages
// chronologically with >=30-day (90-day) retention and strict read-only enforcement.
func TestShadowConversationReadOnlyAndRetention124(t *testing.T) {
	h := newHarness(t, Limits{})
	n := h.n
	ctx := context.Background()
	n.mu.Lock()
	n.grants = append(n.grants,
		&identityv1.Grant{GrantId: "g_muse", ClientId: "c_muse", ClientLabel: "Muse", MachineIds: []string{"*"}},
		&identityv1.Grant{GrantId: "g_hermes", ClientId: "c_hermes", ClientLabel: "Hermes", MachineIds: []string{"*"}})
	n.mu.Unlock()

	now := time.Now()
	museInfo := &identity.ContactInfo{Inbox: "c_muse", Name: "Muse"}
	hermesInfo := &identity.ContactInfo{Inbox: "c_hermes", Name: "Hermes"}

	// A copy that says it is 95 days old (older than the post office keeps any: put at the oldest an honest one can be,
	// not dropped at once), retained (25d), and today's exchange (request + reply).
	msgs := []struct {
		m    *inboxv1.Message
		info *identity.ContactInfo
	}{
		{&inboxv1.Message{MessageId: "msg_01old95d000000000000000000", From: &inboxv1.Address{Inbox: "c_muse"}, To: &inboxv1.Address{Inbox: "c_hermes"}, SentAt: timestamppb.New(now.Add(-95 * 24 * time.Hour)), Subject: "back-dated", Text: "says it is old"}, museInfo},
		{&inboxv1.Message{MessageId: "msg_01old25d000000000000000000", From: &inboxv1.Address{Inbox: "c_muse"}, To: &inboxv1.Address{Inbox: "c_hermes"}, SentAt: timestamppb.New(now.Add(-25 * 24 * time.Hour)), Subject: "within-30d", Text: "kept past 30d window"}, museInfo},
		{&inboxv1.Message{MessageId: "msg_01req000000000000000000000", From: &inboxv1.Address{Inbox: "c_muse"}, To: &inboxv1.Address{Inbox: "c_hermes"}, SentAt: timestamppb.New(now.Add(-2 * time.Minute)), Subject: "两件套同步", Text: "请把信箱盯守也配上"}, museInfo},
		{&inboxv1.Message{MessageId: "msg_01rep000000000000000000000", From: &inboxv1.Address{Inbox: "c_hermes"}, To: &inboxv1.Address{Inbox: "c_muse"}, SentAt: timestamppb.New(now.Add(-1 * time.Minute)), InReplyTo: "msg_01req000000000000000000000", Subject: "Re: 两件套同步", Text: "收到，已配好定时巡检"}, hermesInfo},
	}
	for _, x := range msgs {
		if got := n.takeShadowLetter(x.m, x.info); got != "handled" {
			t.Fatalf("takeShadowLetter %s: %q", x.m.MessageId, got)
		}
	}
	// Duplicate delivery is idempotent.
	if got := n.takeShadowLetter(msgs[2].m, museInfo); got != "handled" {
		t.Fatalf("duplicate takeShadowLetter: %q", got)
	}

	rooms := n.ListRooms().Rooms
	var shadow *machinev1.Room
	for _, r := range rooms {
		if r.Shadow {
			shadow = r
		}
	}
	if shadow == nil || shadow.Direct || shadow.Name != "Hermes ↔ Muse" {
		t.Fatalf("expected shadow room Hermes ↔ Muse, got %+v", rooms)
	}
	res, err := n.ListRoomMessages(&machinev1.ListRoomMessagesRequest{RoomId: shadow.RoomId})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Messages) != 4 {
		t.Fatalf("expected 4 messages (back-dated one kept + 25d + req + reply), got %d: %+v", len(res.Messages), res.Messages)
	}
	if res.Messages[0].Subject != "back-dated" || res.Messages[0].At.AsTime().Before(now.Add(-shadowMaxAge-time.Minute)) ||
		res.Messages[1].Subject != "within-30d" || !res.Messages[1].At.AsTime().Equal(msgs[1].m.SentAt.AsTime()) ||
		res.Messages[2].FromName != "Muse" || res.Messages[2].ToName != "Hermes" || res.Messages[2].Subject != "两件套同步" ||
		res.Messages[3].FromName != "Hermes" || res.Messages[3].ToName != "Muse" || res.Messages[3].InReplyTo != "msg_01req000000000000000000000" {
		t.Fatalf("unexpected shadow timeline: %+v", res.Messages)
	}
	// Retention: what was recorded more than 90 days ago goes when the next copy comes.
	b := n.roomBook()
	b.mu.Lock()
	st := b.rooms[shadow.RoomId]
	st.msgs = append([]*machinev1.RoomMessage{{MessageId: "msg_01expired0000000000000000", RoomId: shadow.RoomId, From: "c:c_muse", Text: "recorded long ago", At: timestamppb.New(now.Add(-95 * 24 * time.Hour))}}, st.msgs...)
	b.mu.Unlock()
	// A forward-dated copy is put at now, not after everything to come.
	future := &inboxv1.Message{MessageId: "msg_01future00000000000000000", From: &inboxv1.Address{Inbox: "c_hermes"}, To: &inboxv1.Address{Inbox: "c_muse"}, SentAt: timestamppb.New(now.Add(48 * time.Hour)), Text: "from the future"}
	if got := n.takeShadowLetter(future, hermesInfo); got != "handled" {
		t.Fatalf("takeShadowLetter future: %q", got)
	}
	res, _ = n.ListRoomMessages(&machinev1.ListRoomMessagesRequest{RoomId: shadow.RoomId})
	if len(res.Messages) != 5 || res.Messages[0].MessageId == "msg_01expired0000000000000000" {
		t.Fatalf("expected the 95-day-old record pruned and 5 left, got %+v", res.Messages)
	}
	if last := res.Messages[4]; last.MessageId != future.MessageId || last.At.AsTime().After(time.Now()) {
		t.Fatalf("forward-dated copy not put at now: %+v", last)
	}

	// Strict read-only: PostRoomMessage, UpdateRoom, StopRoom must all fail.
	if _, err := n.PostRoomMessage(ctx, &machinev1.PostRoomMessageRequest{RoomId: shadow.RoomId, Text: "owner tries to speak"}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("PostRoomMessage on shadow must fail with FailedPrecondition, got %v", err)
	}
	newName := "renamed"
	if _, err := n.UpdateRoom(&machinev1.UpdateRoomRequest{RoomId: shadow.RoomId, Name: &newName}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("UpdateRoom on shadow must fail with FailedPrecondition, got %v", err)
	}
	if _, err := n.StopRoom(ctx, shadow.RoomId); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("StopRoom on shadow must fail with FailedPrecondition, got %v", err)
	}
}
