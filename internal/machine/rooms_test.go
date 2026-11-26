package machine

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	inboxv1 "github.com/samir2015dzh/aide/gen/go/aide/inbox/v1"
	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	"github.com/samir2015dzh/aide/internal/boundary"
	"github.com/samir2015dzh/aide/pkg/identity"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	if !strings.Contains(out.Text, "所有者说：\n\n看一下今天的 CI") || !strings.Contains(out.Text, "直接回复这条消息") {
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
	b := &roomBook{dir: n.roomBook().dir, rooms: map[string]*roomState{}, links: map[string]*roomLink{}}
	b.load(n)
	if st := b.rooms[r.RoomId]; st == nil || len(st.msgs) != 4 || st.keys["k1"] != m1.MessageId {
		t.Fatalf("not kept: %v", st)
	}
}

// TestRoomNamesFollowGrants (工单 #101, after #85): a room made while Muse's
// connection was named "Muse (127.0.0.1)" keeps that name in its member, its
// title and what Muse said; once the grant says "Muse" (the relay's migration,
// or the owner renaming it), the room says "Muse" everywhere, kept on disk too.
// A coding agent's name, the owner's own, is left alone.
func TestRoomNamesFollowGrants(t *testing.T) {
	h := newHarness(t, Limits{})
	n := h.n
	n.mu.Lock()
	n.grants = append(n.grants, &identityv1.Grant{GrantId: "g_muse", ClientId: "c_muse", ClientLabel: "Muse (127.0.0.1)", Version: 1, MachineIds: []string{"*"}})
	n.mu.Unlock()
	var sent []*inboxv1.Message
	n.roomBook().send = func(m *inboxv1.Message) error { sent = append(sent, m); return nil }
	r, err := n.OpenDirectRoom("c_muse")
	if err != nil {
		t.Fatal(err)
	}
	m1, _ := n.PostRoomMessage(context.Background(), &machinev1.PostRoomMessageRequest{RoomId: r.RoomId, Text: "合了告诉我"})
	answer := &inboxv1.Message{MessageId: "msg_01answer", From: &inboxv1.Address{Inbox: "c_muse"}, To: &inboxv1.Address{Inbox: n.machineID, Session: r.RoomId}, InReplyTo: sent[0].MessageId, Text: "合好了"}
	if got := n.takeRoomLetter(answer, &identity.ContactInfo{Inbox: "c_muse", Name: "Muse (127.0.0.1)"}); got != "handled" {
		t.Fatalf("answer: %s", got)
	}
	if l := roomMsgs(t, n, r.RoomId); l[1].FromName != "Muse (127.0.0.1)" || m1 == nil {
		t.Fatalf("before: %v", l)
	}

	// The relay renames the connection: the grant's next version says "Muse".
	n.mu.Lock()
	n.grants = append(n.grants, &identityv1.Grant{GrantId: "g_muse", ClientId: "c_muse", ClientLabel: "Muse", Version: 2, MachineIds: []string{"*"}})
	n.mu.Unlock()
	l, err := n.ListRoomMessages(&machinev1.ListRoomMessagesRequest{RoomId: r.RoomId})
	if err != nil {
		t.Fatal(err)
	}
	if l.Room.Name != "Muse" || l.Room.Members[0].Name != "Muse" || l.Messages[1].FromName != "Muse" || l.Room.LastLine != "Muse：合好了" {
		t.Fatalf("after the rename: %v %v", l.Room, l.Messages)
	}
	for _, x := range n.ListRooms().Rooms {
		if x.RoomId == r.RoomId && (x.Name != "Muse" || x.LastLine != "Muse：合好了") {
			t.Fatalf("listed: %v", x)
		}
	}
	b := &roomBook{dir: n.roomBook().dir, rooms: map[string]*roomState{}, links: map[string]*roomLink{}}
	b.load(n)
	if st := b.rooms[r.RoomId]; st == nil || st.room.Name != "Muse" || st.room.Members[0].Name != "Muse" || st.msgs[1].FromName != "Muse" {
		t.Fatalf("not kept: %v", st)
	}

	// The owner names the direct room: it keeps that name when the agent is renamed again.
	n.UpdateRoom(&machinev1.UpdateRoomRequest{RoomId: r.RoomId, Name: proto.String("和 Muse 的对话")})
	n.mu.Lock()
	n.grants = append(n.grants, &identityv1.Grant{GrantId: "g_muse", ClientId: "c_muse", ClientLabel: "Muse 2", Version: 3, MachineIds: []string{"*"}})
	n.mu.Unlock()
	if l, _ := n.ListRoomMessages(&machinev1.ListRoomMessagesRequest{RoomId: r.RoomId}); l.Room.Name != "和 Muse 的对话" || l.Messages[1].FromName != "Muse 2" {
		t.Fatalf("the owner's room name: %v %v", l.Room, l.Messages)
	}
}

// roomTrust makes n trust a test owner, and gives a way to sign a room's
// charter as one of that owner's devices.
func roomTrust(t *testing.T, ns ...*Machine) (sign func(*identityv1.RoomCharter) (charter, cert *identityv1.Signed), ownerID string) {
	t.Helper()
	root, _ := identity.GenerateKey()
	device, _ := identity.GenerateKey()
	ownerID = identity.OwnerID(identity.MarshalPublicKey(root.Public()))
	rc, err := identity.Sign(&identityv1.RootCert{OwnerId: ownerID, Version: 1, IssuedAt: timestamppb.Now(), RootKeys: []*identityv1.RootKey{{PublicKey: identity.MarshalPublicKey(root.Public())}}}, root)
	if err != nil {
		t.Fatal(err)
	}
	dc, err := identity.Sign(&identityv1.OwnerDeviceCert{OwnerId: ownerID, DevicePublicKey: identity.MarshalPublicKey(device.Public()), IssuedAt: timestamppb.Now()}, root)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range ns {
		tr := identity.NewTrust(ownerID)
		if _, err := tr.AcceptRootChain(&identityv1.RootChain{Certs: []*identityv1.Signed{rc}}); err != nil {
			t.Fatal(err)
		}
		n.mu.Lock()
		n.ownerID, n.trust = ownerID, tr
		n.mu.Unlock()
	}
	return func(c *identityv1.RoomCharter) (*identityv1.Signed, *identityv1.Signed) {
		c.OwnerId, c.IssuedAt = ownerID, timestamppb.Now()
		signed, err := identity.Sign(c, device)
		if err != nil {
			t.Fatal(err)
		}
		return signed, dc
	}, ownerID
}

func roomMsgs(t *testing.T, n *Machine, id string) []*machinev1.RoomMessage {
	t.Helper()
	l, err := n.ListRoomMessages(&machinev1.ListRoomMessagesRequest{RoomId: id})
	if err != nil {
		t.Fatal(err)
	}
	return l.Messages
}

// waitRoom waits for a message from a member in a room, and returns it.
func waitRoom(t *testing.T, n *Machine, id, from, text string) *machinev1.RoomMessage {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		for _, m := range roomMsgs(t, n, id) {
			if m.From == from && strings.Contains(m.Text, text) {
				return m
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no %q from %s in the room: %v", text, from, roomMsgs(t, n, id))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestRoom (ROOMS-DESIGN, G2-b/c): a room of two personal agents and a coding
// agent here. Manual: only those the owner names are woken (the others get it
// for information), a member naming another only suggests it; the coding agent
// talks in a session of its own, and its turn's answer comes into the room.
// Round table: a member naming another hands it on, up to the round limit; a
// [FINAL] reply is sealed; the owner's stop keeps what comes after, handing
// nothing on. What the owner has not read is counted.
func TestRoom(t *testing.T) {
	h := newHarness(t, Limits{})
	n := h.n
	ctx := context.Background()
	sign, _ := roomTrust(t, n)
	n.mu.Lock()
	n.grants = append(n.grants,
		&identityv1.Grant{GrantId: "g_muse", ClientId: "c_muse", ClientLabel: "Muse", MachineIds: []string{"*"}},
		&identityv1.Grant{GrantId: "g_grok", ClientId: "c_grok", ClientLabel: "Grok Bot", MachineIds: []string{"*"}})
	n.mu.Unlock()
	var sent []*inboxv1.Message
	var mu sync.Mutex
	n.roomBook().send = func(m *inboxv1.Message) error { mu.Lock(); sent = append(sent, m); mu.Unlock(); return nil }
	sentTo := func(inbox string, fyi bool) []*inboxv1.Message {
		mu.Lock()
		defer mu.Unlock()
		var out []*inboxv1.Message
		for _, m := range sent {
			if m.To.Inbox == inbox && m.Fyi == fyi {
				out = append(out, m)
			}
		}
		return out
	}
	fakeKey := "m:" + n.machineID + "/fake"
	members := []*machinev1.CreateRoomMember{{BotKey: "c:c_muse"}, {BotKey: "c:c_grok"}, {BotKey: fakeKey, ProjectId: "demo", Name: "Fake"}}
	// A coding agent without the owner's charter: no.
	if _, err := n.CreateRoom(&machinev1.CreateRoomRequest{Name: "评审", Members: members}); err == nil {
		t.Fatal("a coding agent in a room without a charter")
	}
	charter, cert := sign(&identityv1.RoomCharter{RoomId: "room_01test", HostMachineId: n.machineID, Members: []*identityv1.RoomCharterMember{{MachineId: n.machineID, Agent: "fake", ProjectId: "demo"}}})
	r, err := n.CreateRoom(&machinev1.CreateRoomRequest{Name: "评审", Members: members, Charter: charter, CharterDeviceCert: cert})
	if err != nil {
		t.Fatal(err)
	}
	if r.RoomId != "room_01test" || len(r.Members) != 3 || r.MaxRounds != roomRoundsDflt || r.Direct {
		t.Fatalf("the room: %v", r)
	}
	// In the machine's status, for the owner's roster on its Watch: without its charter, and not for agents.
	if rs := n.Status().Rooms; len(rs) != 1 || rs[0].RoomId != r.RoomId || rs[0].Charter != nil {
		t.Fatalf("rooms in the status: %v", rs)
	}
	if rs := n.statusFor(nil).Rooms; rs != nil {
		t.Fatalf("an agent sees rooms: %v", rs)
	}
	post := func(text string, mentions ...string) *machinev1.RoomMessage {
		t.Helper()
		m, err := n.PostRoomMessage(ctx, &machinev1.PostRoomMessageRequest{RoomId: r.RoomId, Text: text, Mentions: mentions})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	muse := func(text string) bool {
		t.Helper()
		copies := sentTo("c_muse", false)
		last := copies[len(copies)-1]
		return n.takeRoomLetter(&inboxv1.Message{MessageId: newID("msg_"), From: &inboxv1.Address{Inbox: "c_muse"}, To: &inboxv1.Address{Inbox: n.machineID, Session: r.RoomId}, InReplyTo: last.MessageId, Text: text},
			&identity.ContactInfo{Inbox: "c_muse", Name: "Muse"}) == "handled"
	}

	// Manual: nobody named, nobody woken; the personal agents hear it.
	if m := post("大家好"); len(m.Deliveries) != 0 {
		t.Fatalf("said to all: %v", m.Deliveries)
	}
	// What they hear for information goes on its own (tellOthers): wait for it.
	for deadline := time.Now().Add(5 * time.Second); len(sentTo("c_muse", true)) != 1 || len(sentTo("c_grok", true)) != 1; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("heard: %d %d", len(sentTo("c_muse", true)), len(sentTo("c_grok", true)))
		}
	}
	// One named: it alone is woken.
	m1 := post("@Muse 看一下 CI", "c:c_muse")
	if len(m1.Deliveries) != 1 || m1.Deliveries[0].BotKey != "c:c_muse" || len(sentTo("c_muse", false)) != 1 || len(sentTo("c_grok", false)) != 0 {
		t.Fatalf("named one: %v", m1.Deliveries)
	}
	if !strings.Contains(sentTo("c_muse", false)[0].Text, "房间成员：@Muse、@Grok Bot、@Fake") {
		t.Fatalf("its copy: %s", sentTo("c_muse", false)[0].Text)
	}
	// In manual mode a member naming another only suggests it.
	muse("CI 红了，@Fake 你看看")
	ms := roomMsgs(t, n, r.RoomId)
	if last := ms[len(ms)-1]; len(last.Asks) != 1 || last.Asks[0] != fakeKey || len(last.Deliveries) != 0 {
		t.Fatalf("a suggestion: %v", last)
	}
	if l := n.ListRooms().Rooms[0]; l.Unread != 1 || !strings.Contains(l.LastLine, "Muse：CI 红了") {
		t.Fatalf("unread: %v", l)
	}
	if l, _ := n.MarkRoomSeen(r.RoomId); l.Unread != 0 {
		t.Fatalf("seen: %v", l)
	}
	// The coding agent, named: a session of its own here; its answer comes in.
	m2 := post("say 我来修", fakeKey)
	got := waitRoom(t, n, r.RoomId, fakeKey, "我来修")
	if got.InReplyTo != m2.MessageId {
		t.Fatalf("its answer: %v", got)
	}
	if roomMsgs(t, n, r.RoomId)[len(roomMsgs(t, n, r.RoomId))-2].Deliveries[0].State != machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_REPLIED {
		t.Fatal("not marked answered")
	}
	sid := ""
	for _, x := range n.ListRooms().Rooms[0].Members {
		if x.BotKey == fakeKey {
			sid = x.SessionId
		}
	}
	if sid == "" {
		t.Fatal("no session for the coding agent")
	}
	post("say 再看一眼", fakeKey)
	waitRoom(t, n, r.RoomId, fakeKey, "再看一眼")
	for _, x := range n.ListRooms().Rooms[0].Members {
		if x.BotKey == fakeKey && x.SessionId != sid {
			t.Fatal("a second session: it talks in one")
		}
	}

	// Round table: Muse hands it on to Fake, which answers; the round ends there.
	rt, two := machinev1.RoomMode_ROOM_MODE_ROUNDTABLE, uint32(1)
	n.UpdateRoom(&machinev1.UpdateRoomRequest{RoomId: r.RoomId, Mode: &rt, MaxRounds: &two})
	post("开始", "c:c_muse")
	muse("@Fake\nsay @Muse 第一棒")
	waitRoom(t, n, r.RoomId, fakeKey, "第一棒")
	// Fake named Muse at round 1 of 1: the limit, said so.
	deadline := time.Now().Add(5 * time.Second)
	for n.ListRooms().Rooms[0].Run != machinev1.RoomRun_ROOM_RUN_STOPPED_LIMIT && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if ms := roomMsgs(t, n, r.RoomId); !ms[len(ms)-1].System || !strings.Contains(ms[len(ms)-1].Text, "轮次上限（1 轮）") {
		t.Fatalf("the limit: %v", ms[len(ms)-1])
	}
	// [FINAL]: sealed.
	post("收个尾", "c:c_muse")
	muse("[FINAL] 结论：先修 CI")
	ms = roomMsgs(t, n, r.RoomId)
	if f := ms[len(ms)-1]; !f.Final || len(f.FinalSha256) != 64 || n.ListRooms().Rooms[0].Run != machinev1.RoomRun_ROOM_RUN_UNSPECIFIED {
		t.Fatalf("final: %v", f)
	}
	// Stopped: what comes after is kept, hands nothing on.
	post("再来", "c:c_muse")
	if _, err := n.StopRoom(ctx, r.RoomId); err != nil {
		t.Fatal(err)
	}
	before := len(sentTo("c_grok", false))
	muse("@Grok Bot 接着")
	ms = roomMsgs(t, n, r.RoomId)
	if l := ms[len(ms)-1]; !l.Late || len(l.Deliveries) != 0 || len(sentTo("c_grok", false)) != before {
		t.Fatalf("after stop: %v", l)
	}
	// The owner speaks again: the room goes on.
	post("继续", "c:c_muse")
	if n.ListRooms().Rooms[0].Run != machinev1.RoomRun_ROOM_RUN_RUNNING {
		t.Fatal("not going on")
	}
}

// TestRoomAcrossMachines: a coding agent on another machine of the owner's, in
// a room this machine hosts. The member machine takes the room's wake only
// with the owner's charter naming this host, it, the agent and the project;
// its agent's answer comes back into the room.
func TestRoomAcrossMachines(t *testing.T) {
	host, member := newHarness(t, Limits{}), newHarness(t, Limits{})
	ctx := context.Background()
	sign, _ := roomTrust(t, host.n, member.n)
	asMachine := func(n *Machine) *identity.ContactInfo {
		return &identity.ContactInfo{Inbox: n.machineID, Kind: "machine"}
	}
	host.n.roomBook().send = func(m *inboxv1.Message) error {
		if m.To.Inbox == member.n.machineID {
			go member.n.takeRoomWake(ctx, m, asMachine(host.n))
		}
		return nil
	}
	member.n.roomBook().send = func(m *inboxv1.Message) error {
		if m.RoomReply != nil {
			host.n.takeRoomLetter(m, asMachine(member.n))
		}
		return nil
	}
	key := "m:" + member.n.machineID + "/fake"
	charter, cert := sign(&identityv1.RoomCharter{RoomId: "room_01across", HostMachineId: host.n.machineID,
		Members: []*identityv1.RoomCharterMember{{MachineId: member.n.machineID, Agent: "fake", ProjectId: "demo"}}})
	r, err := host.n.CreateRoom(&machinev1.CreateRoomRequest{Name: "两台", Members: []*machinev1.CreateRoomMember{{BotKey: key, ProjectId: "demo", Name: "Fake · other"}}, Charter: charter, CharterDeviceCert: cert})
	if err != nil {
		t.Fatal(err)
	}
	host.n.PostRoomMessage(ctx, &machinev1.PostRoomMessageRequest{RoomId: r.RoomId, Text: "say 另一台收到", Mentions: []string{key}})
	got := waitRoom(t, host.n, r.RoomId, key, "另一台收到")
	if got.FromName != "Fake · other" {
		t.Fatalf("its answer: %v", got)
	}
	if s := host.n.ListRooms().Rooms[0].Members[0].SessionId; s == "" {
		t.Fatal("the host doesn't know its session there")
	}

	// A wake whose charter names another host, or another project: refused.
	forged, fcert := sign(&identityv1.RoomCharter{RoomId: "room_01across", HostMachineId: "m_someoneelse",
		Members: []*identityv1.RoomCharterMember{{MachineId: member.n.machineID, Agent: "fake", ProjectId: "demo"}}})
	wakeWith := func(c, dc *identityv1.Signed, project string) string {
		return member.n.takeRoomWake(ctx, &inboxv1.Message{MessageId: newID("msg_"), Text: "say 不该跑", To: &inboxv1.Address{Inbox: member.n.machineID, Session: "room_01across"},
			RoomWake: &inboxv1.RoomWake{Charter: c, CharterDeviceCert: dc, Agent: "fake", ProjectId: project, WakeId: newID("msg_")}}, asMachine(host.n))
	}
	if got := wakeWith(forged, fcert, "demo"); got != "refused" {
		t.Fatalf("another host's charter: %s", got)
	}
	if got := wakeWith(charter, cert, "other"); got != "refused" {
		t.Fatalf("a project not in the charter: %s", got)
	}
	// A charter signed by a key that is not the owner's device: refused.
	stranger, _ := identity.GenerateKey()
	bad, _ := identity.Sign(&identityv1.RoomCharter{OwnerId: "o_x", RoomId: "room_01across", HostMachineId: host.n.machineID}, stranger)
	if got := wakeWith(bad, cert, "demo"); got != "refused" {
		t.Fatalf("a stranger's charter: %s", got)
	}
}

// TestRoomDirectReplySettlesUnkeyed verifies #135 P02: a direct-room reply without
// InReplyTo settles the one owner delivery awaiting it as REPLIED and clears
// Reason (with several waiting: TestRoomDirectUnkeyedReplyWithTwoWaiting).
func TestRoomDirectReplySettlesUnkeyed(t *testing.T) {
	h := newHarness(t, Limits{})
	n := h.n
	ctx := context.Background()
	n.mu.Lock()
	n.grants = append(n.grants, &identityv1.Grant{GrantId: "g_muse", ClientId: "c_muse", ClientLabel: "Muse", MachineIds: []string{"*"}})
	n.mu.Unlock()
	n.roomBook().send = func(m *inboxv1.Message) error { return nil }
	r, err := n.OpenDirectRoom("c_muse")
	if err != nil {
		t.Fatal(err)
	}
	posted, err := n.PostRoomMessage(ctx, &machinev1.PostRoomMessageRequest{RoomId: r.RoomId, Text: "ping"})
	if err != nil {
		t.Fatal(err)
	}
	if len(posted.Deliveries) != 1 || posted.Deliveries[0].State != machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_SENT {
		t.Fatalf("expected SENT delivery, got %v", posted.Deliveries)
	}
	if got := n.takeRoomLetter(&inboxv1.Message{
		MessageId: newID("msg_"),
		To:        &inboxv1.Address{Inbox: n.machineID, Session: r.RoomId},
		Text:      "pong",
	}, &identity.ContactInfo{Inbox: "c_muse", Name: "Muse"}); got != "handled" {
		t.Fatalf("takeRoomLetter: %s", got)
	}
	msgs := roomMsgs(t, n, r.RoomId)
	if d := msgs[0].Deliveries[0]; d.State != machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_REPLIED || d.Reason != "" {
		t.Fatalf("expected REPLIED with empty reason, got state=%v reason=%q", d.State, d.Reason)
	}
	// Settled because it alone waited, not because the reply said so: not written down as what it answers (审计 B).
	if msgs[1].InReplyTo != "" {
		t.Fatalf("a guessed InReplyTo written down: %q", msgs[1].InReplyTo)
	}
}
