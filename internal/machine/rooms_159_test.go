package machine

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	inboxv1 "github.com/samir2015dzh/aide/gen/go/aide/inbox/v1"
	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	relayv1 "github.com/samir2015dzh/aide/gen/go/aide/relay/v1"
	"github.com/samir2015dzh/aide/internal/provider"
	"github.com/samir2015dzh/aide/internal/provider/fake"
	"github.com/samir2015dzh/aide/pkg/identity"
)

// table is a round table of personal agents (工单 #159), their copies kept
// as sent.
type table struct {
	t    *testing.T
	n    *Machine
	room *machinev1.Room
	mu   sync.Mutex
	sent []*inboxv1.Message
}

func newTable(t *testing.T, rounds uint32, names ...string) *table {
	t.Helper()
	h := newHarness(t, Limits{})
	x := &table{t: t, n: h.n}
	var members []*machinev1.CreateRoomMember
	h.n.mu.Lock()
	for _, name := range names {
		id := "c_" + strings.ToLower(strings.Fields(name)[0])
		h.n.grants = append(h.n.grants, &identityv1.Grant{GrantId: "g" + id, ClientId: id, ClientLabel: name, MachineIds: []string{"*"}})
		members = append(members, &machinev1.CreateRoomMember{BotKey: "c:" + id})
	}
	h.n.mu.Unlock()
	h.n.roomBook().send = func(m *inboxv1.Message) error { x.mu.Lock(); x.sent = append(x.sent, m); x.mu.Unlock(); return nil }
	r, err := h.n.CreateRoom(&machinev1.CreateRoomRequest{Name: "评审", Members: members, Mode: machinev1.RoomMode_ROOM_MODE_ROUNDTABLE, MaxRounds: rounds})
	if err != nil {
		t.Fatal(err)
	}
	x.room = r
	return x
}

// woken is the copies that woke the member with inbox id (not those for information), oldest first.
func (x *table) woken(id string) []*inboxv1.Message {
	x.mu.Lock()
	defer x.mu.Unlock()
	var out []*inboxv1.Message
	for _, m := range x.sent {
		if m.To.Inbox == id && !m.Fyi {
			out = append(out, m)
		}
	}
	return out
}

func (x *table) post(text string) {
	x.t.Helper()
	if _, err := x.n.PostRoomMessage(context.Background(), &machinev1.PostRoomMessageRequest{RoomId: x.room.RoomId, Text: text}); err != nil {
		x.t.Fatal(err)
	}
}

// answer: the member with inbox id answers the last copy that woke it.
func (x *table) answer(id, text string) {
	x.t.Helper()
	copies := x.woken(id)
	if len(copies) == 0 {
		x.t.Fatalf("%s was never woken", id)
	}
	m := &inboxv1.Message{MessageId: newID("msg_"), From: &inboxv1.Address{Inbox: id}, To: &inboxv1.Address{Inbox: x.n.machineID, Session: x.room.RoomId},
		InReplyTo: copies[len(copies)-1].MessageId, Text: text}
	if got := x.n.takeRoomLetter(m, &identity.ContactInfo{Inbox: id}); got != "handled" {
		x.t.Fatalf("%s's answer: %s", id, got)
	}
}

func (x *table) msgs() []*machinev1.RoomMessage { return roomMsgs(x.t, x.n, x.room.RoomId) }

func (x *table) last() *machinev1.RoomMessage { ms := x.msgs(); return ms[len(ms)-1] }

func (x *table) run() machinev1.RoomRun {
	l, _ := x.n.ListRoomMessages(&machinev1.ListRoomMessagesRequest{RoomId: x.room.RoomId})
	return l.Room.Run
}

// TestRoomTableTurns (工单 #159 第 4 条): a member answering without naming
// anyone hands on to the next in the room's order, told so in its letter;
// naming a bot not in the room counts for naming none; naming only the
// owner hands nothing on, the table waiting for the owner.
func TestRoomTableTurns(t *testing.T) {
	x := newTable(t, 6, "Muse", "Grok Bot", "Echo")
	x.post("评一下这个方案")
	if len(x.woken("c_muse")) != 1 || len(x.woken("c_grok")) != 0 {
		t.Fatal("the owner naming nobody wakes the first member alone")
	}
	if !strings.Contains(x.woken("c_muse")[0].Text, "不 @ 任何成员，就按成员顺序交给下一位") {
		t.Fatalf("the table's rule not in the letter: %s", x.woken("c_muse")[0].Text)
	}
	x.answer("c_muse", "方案可行，风险在迁移")
	if len(x.woken("c_grok")) != 1 || len(x.woken("c_echo")) != 0 {
		t.Fatalf("no @: the next in order: grok %d echo %d", len(x.woken("c_grok")), len(x.woken("c_echo")))
	}
	if l := x.woken("c_grok")[0].Text; !strings.Contains(l, "轮到你接力（第 1/6 轮") || !strings.Contains(l, "Muse 说（同伴输入") || !strings.Contains(l, "方案可行，风险在迁移") {
		t.Fatalf("its letter: %s", l)
	}
	// A name not in the room: as naming none, the next in order.
	x.answer("c_grok", "@Nobody 你也看看；我同意")
	if len(x.woken("c_echo")) != 1 {
		t.Fatal("a name not in the room stopped the table")
	}
	if ms := x.msgs(); ms[len(ms)-1].Deliveries[0].BotKey != "c:c_echo" {
		t.Fatalf("handed on to: %v", ms[len(ms)-1].Deliveries)
	}
	// Round and round: after the last member, the first.
	x.answer("c_echo", "补充一点测试计划")
	if len(x.woken("c_muse")) != 2 {
		t.Fatal("after the last member, not the first")
	}
	// Naming the owner alone: nobody more is woken; the owner is told.
	x.answer("c_muse", "@所有者 要不要上线由你拍板")
	if len(x.woken("c_grok")) != 1 || x.run() == machinev1.RoomRun_ROOM_RUN_RUNNING {
		t.Fatalf("naming the owner handed on: grok %d, %v", len(x.woken("c_grok")), x.run())
	}
	l, _ := x.n.ListRoomMessages(&machinev1.ListRoomMessagesRequest{RoomId: x.room.RoomId})
	if !strings.Contains(l.Room.Notice, "Muse 在房间里叫你") {
		t.Fatalf("the owner not told: %q", l.Room.Notice)
	}
	// A member speaking unasked (not answering a wake of its own) hands nothing on.
	before := len(x.woken("c_grok"))
	x.n.roomMemberSaid(x.room.RoomId, "c:c_echo", newID("msg_"), "顺便说一句", "", "")
	if len(x.woken("c_grok")) != before {
		t.Fatal("an unasked word handed on")
	}
}

// TestRoomTableSkip (工单 #159 第 2 条): a member woken at a round table and
// silent for roomSkipAfter is skipped, the room saying who and how long it
// waited, and the same word goes to the next in order; what it says later
// is kept, late, handing nothing on. With nobody left to hand on to, the
// last bot to speak wraps up (第 3 条), its answer sealed.
func TestRoomTableSkip(t *testing.T) {
	x := newTable(t, 6, "Muse", "Grok Bot", "Echo")
	ctx := context.Background()
	x.post("评一下这个方案")
	// Not yet: nothing happens before roomSkipAfter.
	x.n.roomTick(ctx, time.Now().Add(roomSkipAfter-10*time.Second))
	if len(x.woken("c_grok")) != 0 {
		t.Fatal("skipped too soon")
	}
	x.n.roomTick(ctx, time.Now().Add(roomSkipAfter+time.Second))
	ms := x.msgs()
	if d := ms[0].Deliveries[0]; d.State != machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_SKIPPED || !strings.Contains(d.Reason, "Muse 超时未回复（等了 3 分") {
		t.Fatalf("Muse's copy: %v", d)
	}
	if l := ms[len(ms)-1]; !l.System || !strings.Contains(l.Text, "Muse 超时未回复（等了 3 分") || !strings.Contains(l.Text, "交给 Grok Bot 接力") {
		t.Fatalf("the room's record: %v", l)
	}
	if len(x.woken("c_grok")) != 1 || !strings.Contains(x.woken("c_grok")[0].Text, "所有者在 Aide 的房间「评审」里说：\n\n评一下这个方案") || x.run() != machinev1.RoomRun_ROOM_RUN_RUNNING {
		t.Fatalf("the owner's word not handed to Grok: %v, %v", x.woken("c_grok"), x.run())
	}
	// Muse comes back late: kept, hands nothing on, stays skipped.
	x.answer("c_muse", "@Echo 我来晚了")
	if l := x.last(); !l.Late || len(l.Deliveries) != 0 || len(x.woken("c_echo")) != 0 || x.msgs()[0].Deliveries[0].State != machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_SKIPPED {
		t.Fatalf("a late answer: %v", l)
	}
	// Grok answers: the table goes on, the next in order after it.
	x.answer("c_grok", "同意，风险在迁移")
	if len(x.woken("c_echo")) != 1 {
		t.Fatal("the table did not go on after the skip")
	}
	// Echo silent too: Grok's word goes to Muse, next in order and not woken by it yet.
	x.n.roomTick(ctx, time.Now().Add(roomSkipAfter+time.Second))
	if len(x.woken("c_muse")) != 2 {
		t.Fatalf("Echo skipped, Muse not woken: %d", len(x.woken("c_muse")))
	}
	// Muse silent again: Grok said it, Echo and Muse had it: nobody left. Grok, the last to speak, wraps up.
	x.n.roomTick(ctx, time.Now().Add(roomSkipAfter+time.Second))
	call := x.last()
	if !call.System || !strings.Contains(call.Text, "没有别的成员能接力，请 Grok Bot 汇总共识收口") || len(x.woken("c_grok")) != 2 {
		t.Fatalf("no wrap-up: %v, grok woken %d", call, len(x.woken("c_grok")))
	}
	if l := x.woken("c_grok")[1].Text; !strings.Contains(l, "没有别的成员能接力了，还没有人以 [FINAL] 收口") || !strings.Contains(l, "回复以 [FINAL] 开头") {
		t.Fatalf("its letter: %s", l)
	}
	// Its answer is the room's result, [FINAL] or not.
	x.answer("c_grok", "结论：方案可行，先做迁移演练")
	if f := x.last(); !f.Final || len(f.FinalSha256) != 64 || x.run() == machinev1.RoomRun_ROOM_RUN_RUNNING {
		t.Fatalf("not sealed: %v, %v", f, x.run())
	}
}

// TestRoomTableWrapUp (工单 #159 第 3 条): at the round limit with no
// [FINAL], the last to speak is asked to wrap up, and its [FINAL] answer is
// sealed, the table stopped at its limit. Asked and silent, the next in
// order is asked instead; with nobody left, the table stops, said so.
func TestRoomTableWrapUp(t *testing.T) {
	x := newTable(t, 2, "Muse", "Grok Bot")
	x.post("评一下这个方案")
	x.answer("c_muse", "第一轮看法")
	x.answer("c_grok", "第二轮看法")
	if len(x.woken("c_muse")) != 2 {
		t.Fatal("round 2 not handed on")
	}
	x.answer("c_muse", "第三次发言，到上限了")
	call := x.last()
	if !call.System || !strings.Contains(call.Text, "到达轮次上限（2 轮），还没有人以 [FINAL] 收口：请 Muse 汇总共识收口") || len(call.Deliveries) != 1 || call.Deliveries[0].BotKey != "c:c_muse" {
		t.Fatalf("the call to wrap up: %v", call)
	}
	letter := x.woken("c_muse")[2].Text
	if !strings.Contains(letter, "自主圆桌到了轮次上限（2 轮），还没有人以 [FINAL] 收口") || !strings.Contains(letter, "第三次发言，到上限了") || strings.Contains(letter, "房间成员：") {
		t.Fatalf("its letter: %s", letter)
	}
	if x.run() != machinev1.RoomRun_ROOM_RUN_RUNNING {
		t.Fatalf("waiting for the wrap-up: %v", x.run())
	}
	x.answer("c_muse", "[FINAL] 共识：可行，先演练迁移")
	f := x.last()
	l, _ := x.n.ListRoomMessages(&machinev1.ListRoomMessagesRequest{RoomId: x.room.RoomId})
	if !f.Final || f.InReplyTo != call.MessageId || l.Room.Run != machinev1.RoomRun_ROOM_RUN_STOPPED_LIMIT || !strings.Contains(l.Room.Notice, "Muse 交出了共识结果") {
		t.Fatalf("sealed: %v, %v %q", f, l.Room.Run, l.Room.Notice)
	}

	// Again, Muse silent when asked to wrap up: Grok is asked; Grok silent: nobody is left, the table stops.
	ctx := context.Background()
	x.post("再评一次")
	x.answer("c_muse", "一")
	x.answer("c_grok", "二")
	x.answer("c_muse", "三")
	x.n.roomTick(ctx, time.Now().Add(roomSkipAfter+time.Second))
	if l := x.last(); !l.System || !strings.Contains(l.Text, "Muse 超时未回复") || !strings.Contains(l.Text, "改请 Grok Bot 汇总共识收口") {
		t.Fatalf("not asked of Grok: %v", l)
	}
	if c := x.woken("c_grok"); !strings.Contains(c[len(c)-1].Text, "原本请来收口的 Muse 超时未回复") {
		t.Fatalf("Grok's letter: %s", c[len(c)-1].Text)
	}
	x.n.roomTick(ctx, time.Now().Add(roomSkipAfter+time.Second))
	if l := x.last(); !l.System || !strings.Contains(l.Text, "没有别的成员接得上，圆桌停下了") || x.run() != machinev1.RoomRun_ROOM_RUN_STOPPED_LIMIT {
		t.Fatalf("the table not stopped: %v, %v", l, x.run())
	}
}

// unconfirmed is a coding agent found here whose sign-in its check can't tell.
type unconfirmed struct{ fake.Provider }

func (unconfirmed) Discover(context.Context) provider.Info {
	return provider.Info{Name: "shy", Available: true, LoginUnconfirmed: true}
}

// TestCheckRoomMembers (工单 #159 第 1 条): each member picked for a room is
// checked for real: a personal agent's grant here and what the post office
// says of its inbox now (a contact? registered? does anything take its
// letters?); a coding agent of this machine's checked again (there? paused?
// signed in?). The roster's word is not taken for any of it.
func TestCheckRoomMembers(t *testing.T) {
	h := newHarnessWith(t, Limits{}, map[string]provider.Provider{"shy": unconfirmed{}})
	n := h.n
	n.mu.Lock()
	for _, id := range []string{"c_ok", "c_ghost", "c_quiet", "c_stranger"} {
		n.grants = append(n.grants, &identityv1.Grant{GrantId: "g" + id, ClientId: id, ClientLabel: id, MachineIds: []string{"*"}})
	}
	n.mu.Unlock()
	seen := time.Now().Add(-time.Hour).Truncate(time.Second)
	asked := 0
	n.roomBook().reach = func(_ context.Context, id string) (inboxReach, error) {
		asked++
		switch id {
		case "c_ok":
			return inboxReach{contact: true, registered: true, taking: true}, nil
		case "c_ghost": // online in the roster, its inbox never set up
			return inboxReach{contact: true}, nil
		case "c_quiet":
			return inboxReach{contact: true, registered: true, lastSeen: seen}, nil
		}
		return inboxReach{}, nil
	}
	if err := n.setAgentPaused(&identityv1.SetAgentPaused{Agent: "fake", Paused: true}); err != nil {
		t.Fatal(err)
	}
	here := "m:" + n.machineID + "/"
	keys := []string{"c:c_ok", "c:c_ghost", "c:c_quiet", "c:c_stranger", "c:c_nogrant", here + "fake", here + "shy", here + "codex", "m:m_elsewhere/fake"}
	var members []*machinev1.CreateRoomMember
	for _, k := range keys {
		members = append(members, &machinev1.CreateRoomMember{BotKey: k, ProjectId: "demo"})
	}
	want := []machinev1.RoomMemberProblem{
		machinev1.RoomMemberProblem_ROOM_MEMBER_PROBLEM_UNSPECIFIED,
		machinev1.RoomMemberProblem_ROOM_MEMBER_PROBLEM_NO_INBOX,
		machinev1.RoomMemberProblem_ROOM_MEMBER_PROBLEM_NOT_TAKING,
		machinev1.RoomMemberProblem_ROOM_MEMBER_PROBLEM_NOT_CONTACT,
		machinev1.RoomMemberProblem_ROOM_MEMBER_PROBLEM_NO_GRANT,
		machinev1.RoomMemberProblem_ROOM_MEMBER_PROBLEM_AGENT_PAUSED,
		machinev1.RoomMemberProblem_ROOM_MEMBER_PROBLEM_AGENT_UNCONFIRMED,
		machinev1.RoomMemberProblem_ROOM_MEMBER_PROBLEM_AGENT_MISSING,
		machinev1.RoomMemberProblem_ROOM_MEMBER_PROBLEM_AGENT_MISSING,
	}
	res := n.CheckRoomMembers(context.Background(), members)
	if len(res.Members) != len(keys) {
		t.Fatalf("%d answers for %d members", len(res.Members), len(keys))
	}
	for i, c := range res.Members {
		if c.BotKey != keys[i] || c.Problem != want[i] {
			t.Errorf("%s: %v, want %v", keys[i], c.Problem, want[i])
		}
	}
	if c := res.Members[2]; c.LastSeen == nil || !c.LastSeen.AsTime().Equal(seen) {
		t.Fatalf("when it was last there: %v", c.LastSeen)
	}
	if asked != 4 {
		t.Fatalf("the post office asked %d times, want once for each personal agent with a grant here", asked)
	}
	// Resumed and checked again: it can take its turn.
	n.setAgentPaused(&identityv1.SetAgentPaused{Agent: "fake"})
	if c := n.CheckRoomMembers(context.Background(), members[5:6]).Members[0]; c.Problem != machinev1.RoomMemberProblem_ROOM_MEMBER_PROBLEM_UNSPECIFIED {
		t.Fatalf("fake resumed: %v", c.Problem)
	}
	// The post office out of reach: said so, not taken for reachable.
	n.roomBook().reach = func(context.Context, string) (inboxReach, error) { return inboxReach{}, errors.New("not connected") }
	if c := n.CheckRoomMembers(context.Background(), members[:1]).Members[0]; c.Problem != machinev1.RoomMemberProblem_ROOM_MEMBER_PROBLEM_RELAY_UNREACHABLE {
		t.Fatalf("no post office: %v", c.Problem)
	}
}

// TestTaking (工单 #159 第 1 条): whether something takes a personal
// agent's letters now, as its inbox's registration says it takes them, not
// whether it shows online: a command line polling its inbox is online while
// its link keeps a connection, and reads nothing until its next command.
func TestTaking(t *testing.T) {
	online, recent, gone := &relayv1.Presence{Online: true}, &relayv1.Presence{Recent: true}, &relayv1.Presence{}
	for _, c := range []struct {
		kind, takes string
		p           *relayv1.Presence
		woken, want bool
	}{
		{"cli", "poll", online, false, false},
		{"cli", "poll", gone, true, true}, // its doorbell wakes it
		{"cli", "webhook", gone, false, true},
		{"cli", "wait", online, false, true},
		{"cli", "wait", gone, false, false},
		{"cli", "hook", recent, false, true},
		{"mcp", "poll", recent, false, true}, // an MCP connection checks with its calls
		{"mcp", "poll", gone, false, false},
		{"mcp", "poll", nil, false, false},
	} {
		if got := taking(&identity.ContactInfo{Kind: c.kind, TakesMessages: c.takes}, c.p, c.woken); got != c.want {
			t.Errorf("%s %s %v woken=%v: %v, want %v", c.kind, c.takes, c.p, c.woken, got, c.want)
		}
	}
}
