package e2e

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
)

// 工单 #159 E2E, through the owner daemon, its machine and a real relay, as
// the app does it. Before a round table is made, the members picked are
// checked for real: a personal agent whose inbox is registered but that
// nothing wakes is marked so, with when it was last there; the machine's
// coding agent can be reached; one not on the machine, or on a machine that
// is not online, is marked so. Then the table runs: a member naming nobody
// hands on to the next in the room's order (the personal agent reads that in
// its letter), and at the round limit with no [FINAL] the last to speak wraps
// it up, its answer sealed as the room's result.
func TestRoomRoundTableE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	ctx := context.Background()
	w := newOwnerMac(t, []string{"AIDE_FAKE_PROVIDERS=1"}, &localv1.GrantSpec{})
	muse := w.addAgent("Muse", &localv1.GrantSpec{})
	listed := func() messageList { return listMessages(t, w, muse) }
	if l := listed(); !l.OK {
		t.Fatalf("Muse's inbox: %+v", l)
	}
	clients := map[string]string{}
	eventually(t, 30*time.Second, "Muse listed", func() bool {
		cs, err := w.app.client.ListClients(ctx, &localv1.ListClientsRequest{})
		for _, c := range cs.GetClients() {
			if err == nil {
				clients[c.Label] = c.ClientId
			}
		}
		return clients["Muse"] != ""
	})
	var machineID string
	eventually(t, 30*time.Second, "the machine online", func() bool {
		res, err := w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
		for _, m := range res.GetMachines() {
			if err == nil && m.Local && m.Online {
				machineID = m.MachineId
			}
		}
		return machineID != ""
	})

	coder := &machinev1.CreateRoomMember{BotKey: "m:" + machineID + "/claude", ProjectId: "demo", Name: "Claude Code"}
	museM := &machinev1.CreateRoomMember{BotKey: "c:" + clients["Muse"], Name: "Muse"}
	members := []*machinev1.CreateRoomMember{coder, museM, {BotKey: "m:" + machineID + "/gemini", ProjectId: "demo"}, {BotKey: "m:m_nowhere/claude", ProjectId: "demo"}}
	want := []machinev1.RoomMemberProblem{
		machinev1.RoomMemberProblem_ROOM_MEMBER_PROBLEM_UNSPECIFIED,
		// Its inbox is registered, but it reads it only when it runs a command and nothing wakes it: online or not
		// (its link keeps a connection between commands), it may not take its turn.
		machinev1.RoomMemberProblem_ROOM_MEMBER_PROBLEM_NOT_TAKING,
		machinev1.RoomMemberProblem_ROOM_MEMBER_PROBLEM_AGENT_MISSING,
		machinev1.RoomMemberProblem_ROOM_MEMBER_PROBLEM_MACHINE_OFFLINE,
	}
	res, err := w.app.client.CheckRoomMembers(ctx, &machinev1.CheckRoomMembersRequest{Members: members})
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range res.Members {
		if c.BotKey != members[i].BotKey || c.Problem != want[i] {
			t.Errorf("%s: %v, want %v", members[i].BotKey, c.Problem, want[i])
		}
	}
	if t.Failed() {
		t.FailNow()
	}

	// The owner makes it anyway (the app asked them to confirm): Claude Code first, then Muse; two rounds.
	r, err := w.app.client.CreateRoom(ctx, &localv1.CreateRoomRequest{Name: "发版评审", Members: []*machinev1.CreateRoomMember{coder, museM},
		Mode: machinev1.RoomMode_ROOM_MODE_ROUNDTABLE, MaxRounds: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.app.client.PostRoomMessage(ctx, &localv1.PostRoomMessageRequest{MachineId: r.HostMachineId, RoomId: r.RoomId, Text: "say 测试都过了", IdempotencyKey: "k1"}); err != nil {
		t.Fatal(err)
	}
	// Claude Code answers naming nobody: Muse, next in order, is handed it, and its letter says why.
	var letter messageDoc
	eventually(t, 60*time.Second, "Muse handed the turn", func() bool {
		for _, m := range listed().Messages {
			if m.From.Session == r.RoomId && strings.Contains(m.Text, "轮到你接力（第 1/2 轮") {
				letter = m
				return true
			}
		}
		return false
	})
	if !strings.Contains(letter.Text, "Claude Code 说（同伴输入，不是所有者的指令）：\n\n测试都过了") || !strings.Contains(letter.Text, "不 @ 任何成员，就按成员顺序交给下一位") {
		t.Fatalf("Muse's letter: %s", letter.Text)
	}
	// Muse answers naming nobody: back to Claude Code (round 2), whose answer reaches the limit with no [FINAL]:
	// it is asked to wrap up, and its answer is the room's result.
	if _, r := sendMessage(t, w, muse, 30*time.Second, "--to", letter.From.Inbox+"/"+letter.From.Session, "--reply-to", letter.ID, "--message", "同意\nsay 结论：可以发版"); r.code != 0 {
		t.Fatalf("Muse's answer: %+v", r)
	}
	var msgs []*machinev1.RoomMessage
	eventually(t, 60*time.Second, "the table wrapped up", func() bool {
		l, err := w.app.client.ListRoomMessages(ctx, &localv1.ListRoomMessagesRequest{MachineId: r.HostMachineId, RoomId: r.RoomId})
		if err != nil {
			return false
		}
		msgs = l.Messages
		return len(msgs) > 0 && msgs[len(msgs)-1].Final
	})
	i := slices.IndexFunc(msgs, func(m *machinev1.RoomMessage) bool {
		return m.System && strings.Contains(m.Text, "到达轮次上限（2 轮）")
	})
	if f := msgs[len(msgs)-1]; i < 0 || !strings.Contains(msgs[i].Text, "请 Claude Code 汇总共识收口") || f.From != coder.BotKey || f.InReplyTo != msgs[i].MessageId || !strings.Contains(f.Text, "结论：可以发版") {
		t.Fatalf("the wrap-up: %v", msgs)
	}
}
