package machine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	relayv1 "github.com/samir2015dzh/aide/gen/go/aide/relay/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
)

// What a room's host keeps an eye on (批 H):
//
//   - 工单 #70: no more than roomMaxBusy bots at work for the rooms here at
//     once. A wake past that waits, its copy marked queued in the room, and
//     goes as soon as one of those at work is done (answered, failed, the
//     room stopped). And a member woken that shows no sign of life for
//     roomQuiet is told of to the owner, once.
//   - 工单 #71: what the post office says of each copy that went there: the
//     member took it ("taken", or further) — the copy is then taken, not
//     just sent; the post office refused or let it expire — said so.
//   - 工单 #160: a copy still not taken roomUntaken on is told of to the
//     owner, once: it may not arrive (the post office keeps it for its
//     retention, a month, before it expires); and one that expired is told
//     of too, not only marked failed.
//   - 工单 #74: what this machine's coding agents said for a room another
//     machine hosts, while that host is away, waits at the post office: the
//     status lists it (RoomOutbox) until the host takes it, so the owner's
//     devices can show it waits.

const (
	roomMaxBusy  = 8
	roomQuiet    = 2 * time.Minute
	roomWatchGap = 5 * time.Second
	// roomUntaken: sent and not taken for so long, a copy may not arrive (工单 #160); what the owner is told says
	// "a day".
	roomUntaken = 24 * time.Hour
)

// roomQueued is a wake waiting for a free place (工单 #70), kept with its room.
type roomQueued struct {
	Msg     string    `json:"msg"`
	Copy    string    `json:"copy"`
	Bot     string    `json:"bot"`
	Speaker string    `json:"speaker,omitempty"`
	Text    string    `json:"text"`
	At      time.Time `json:"at"`
}

// roomOut is a reply this machine sent a room's host (工单 #74).
type roomOut struct {
	room, host string
	at         time.Time
}

// busy is how many wakes of the rooms here wait for their member (b.mu held).
func (b *roomBook) busy() int {
	n := 0
	for _, st := range b.rooms {
		for _, m := range st.msgs {
			for _, d := range m.Deliveries {
				if d.State == machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_SENT || d.State == machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_TAKEN {
					n++
				}
			}
		}
	}
	return n
}

// admit lets go as many of msg's wakes (pending marked them sent) as there is
// room for; the others wait in the room's queue, their copies marked queued
// (b.mu held).
func (b *roomBook) admit(st *roomState, msg *machinev1.RoomMessage, wakes []wake) []wake {
	free := roomMaxBusy - (b.busy() - len(wakes))
	if free >= len(wakes) {
		for _, w := range wakes {
			b.woken[w.copyID] = time.Now()
		}
		return wakes
	}
	free = max(free, 0)
	now := time.Now()
	for _, w := range wakes[:free] {
		b.woken[w.copyID] = now
	}
	for _, w := range wakes[free:] {
		for _, d := range msg.Deliveries {
			if d.CopyId == w.copyID {
				d.State = machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_QUEUED
			}
		}
		st.queue = append(st.queue, roomQueued{Msg: msg.MessageId, Copy: w.copyID, Bot: w.mem.BotKey, Speaker: w.speaker, Text: w.text, At: now})
	}
	return wakes[:free]
}

// drainRooms wakes those waiting in the rooms' queues, oldest first, while
// there is room (工单 #70).
func (n *Machine) drainRooms(ctx context.Context) {
	b := n.roomBook()
	type next struct {
		st     *roomState
		msg    *machinev1.RoomMessage
		room   *machinev1.Room
		w      wake
		before []*machinev1.RoomMessage
	}
	b.mu.Lock()
	free := roomMaxBusy - b.busy()
	var go_ []next
	for free > 0 {
		var st *roomState
		for _, x := range b.rooms {
			if len(x.queue) > 0 && (st == nil || x.queue[0].At.Before(st.queue[0].At)) {
				st = x
			}
		}
		if st == nil {
			break
		}
		q := st.queue[0]
		st.queue = st.queue[1:]
		i := slices.IndexFunc(st.msgs, func(m *machinev1.RoomMessage) bool { return m.MessageId == q.Msg })
		mem := memberOf(st.room, q.Bot)
		if i < 0 || mem == nil {
			continue
		}
		msg := st.msgs[i]
		for _, d := range msg.Deliveries {
			if d.CopyId == q.Copy {
				d.State = machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_SENT
			}
		}
		b.woken[q.Copy] = time.Now()
		go_ = append(go_, next{st: st, msg: msg, room: cloneRoom(st.room), w: wake{mem: mem, speaker: q.Speaker, text: q.Text, copyID: q.Copy}, before: lastMsgs(st.msgs[:i])})
		n.settleRun(st)
		b.save(st)
		free--
	}
	b.mu.Unlock()
	for _, x := range go_ {
		d := n.wakeMember(ctx, x.room, x.w, x.before)
		b.mu.Lock()
		settle(x.msg, []*machinev1.RoomDelivery{d})
		n.settleRun(x.st)
		b.save(x.st)
		b.mu.Unlock()
	}
}

// dropQueued: a stopped room wakes nobody more; its waiting wakes fail, said so (b.mu held).
func dropQueued(st *roomState) {
	for _, q := range st.queue {
		for _, m := range st.msgs {
			for _, d := range m.Deliveries {
				if d.CopyId == q.Copy && d.State == machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_QUEUED {
					d.State, d.Reason = machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_FAILED, "房间停下了：没有再叫醒它"
				}
			}
		}
	}
	st.queue = nil
}

// roomWatch looks at the rooms here every few seconds until ctx is done.
func (n *Machine) roomWatch(ctx context.Context) {
	t := time.NewTicker(roomWatchGap)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n.roomTick(ctx, time.Now())
		}
	}
}

// roomTick: what the post office says of the copies and replies that went
// there; members woken and quiet; then those waiting, woken while there is room.
func (n *Machine) roomTick(ctx context.Context, now time.Time) {
	b := n.roomBook()
	b.mu.Lock()
	var ids []string
	for _, st := range b.rooms {
		for _, m := range st.msgs {
			for _, d := range m.Deliveries {
				if d.State != machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_SENT {
					continue
				}
				if mem := memberOf(st.room, d.BotKey); mem != nil && (mem.Address != "" || mem.MachineId != n.machineID) {
					ids = append(ids, d.CopyId)
				}
			}
		}
	}
	for id := range b.outbox {
		ids = append(ids, id)
	}
	b.mu.Unlock()

	states := map[string]string{}
	for len(ids) > 0 {
		batch := ids[:min(100, len(ids))]
		ids = ids[len(batch):]
		got, err := b.states(ctx, batch)
		if err != nil {
			break // not connected now: next time
		}
		for k, v := range got {
			states[k] = v
		}
	}

	type cand struct {
		room, copy string
		mem        *machinev1.RoomMember
		state      machinev1.RoomDeliveryState
		woke       time.Time
	}
	var cands []cand
	type told struct{ room, name, line string }
	var tell []told
	b.mu.Lock()
	live := map[string]bool{}
	for id, o := range b.outbox {
		if s, ok := states[id]; (ok && s != "waiting") || now.Sub(o.at) > 7*24*time.Hour {
			delete(b.outbox, id)
		}
	}
	for _, st := range b.rooms {
		changed := false
		// The owner is told, in the room and on their devices (工单 #160).
		notice := func(line string) {
			st.room.Notice, st.room.NoticeAt = line, timestamppb.New(now)
			tell = append(tell, told{st.room.RoomId, st.room.Name, line})
		}
		for _, m := range st.msgs {
			for _, d := range m.Deliveries {
				mem := memberOf(st.room, d.BotKey)
				if d.State == machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_SENT {
					switch states[d.CopyId] {
					case "taken", "handed", "handled", "held":
						d.State, changed = machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_TAKEN, true
					case "refused":
						d.State, d.Reason, changed = machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_REFUSED, "对方没有收下这条", true
					case "expired":
						d.State, d.Reason, changed = machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_FAILED, "在邮局放太久过期了，对方一直没来取", true
						if mem != nil {
							notice(fmt.Sprintf("%s 一直没来取信：消息在邮局放到过期，没有送到", mem.Name))
						}
					}
				}
				if d.State == machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_SENT || d.State == machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_TAKEN {
					live[d.CopyId] = true
				}
				if !live[d.CopyId] {
					continue
				}
				woke := m.At.AsTime()
				if w, ok := b.woken[d.CopyId]; ok {
					woke = w
				}
				// At the post office a day and not taken: it may not arrive. Said instead of quiet, were that not said yet.
				if d.State == machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_SENT && !d.Untaken && mem != nil && now.Sub(woke) >= roomUntaken {
					d.Untaken, d.Reminded, changed = true, true, true
					notice(fmt.Sprintf("%s 已一天没来取信，这条消息可能送不到", mem.Name))
				}
				if d.Reminded {
					continue
				}
				if mem != nil && now.Sub(woke) >= roomQuiet {
					cands = append(cands, cand{room: st.room.RoomId, copy: d.CopyId, mem: proto.Clone(mem).(*machinev1.RoomMember), state: d.State, woke: woke})
				}
			}
		}
		if changed {
			n.settleRun(st)
			b.save(st)
		}
	}
	for id := range b.woken {
		if !live[id] {
			delete(b.woken, id)
		}
	}
	b.mu.Unlock()

	// Sessions are read without the rooms' lock (a session's turn ending takes it).
	quiet := map[string]bool{}
	for _, c := range cands {
		if !n.stirring(c.mem, c.state, c.woke) {
			quiet[c.copy] = true
		}
	}
	b.mu.Lock()
	for _, c := range cands {
		st := b.rooms[c.room]
		if !quiet[c.copy] || st == nil {
			continue
		}
		for _, m := range st.msgs {
			for _, d := range m.Deliveries {
				if d.CopyId != c.copy || d.Reminded || (d.State != machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_SENT && d.State != machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_TAKEN) {
					continue
				}
				d.Reminded = true
				line := fmt.Sprintf("%s 被叫醒两分钟了，还没动静", c.mem.Name)
				st.room.Notice, st.room.NoticeAt = line, timestamppb.New(now)
				tell = append(tell, told{st.room.RoomId, st.room.Name, line})
				b.save(st)
			}
		}
	}
	b.mu.Unlock()
	for _, x := range tell {
		go n.notify(&sessionv1.Notice{Kind: "room", At: timestamppb.New(now), Title: x.name, RoomId: x.room, RoomLine: x.line}, "room:"+x.room, func() {})
	}
	n.drainRooms(ctx)
}

// stirring tells whether a member woken at woke shows a sign of life (工单
// #70): one elsewhere has taken its copy from the post office; one here has
// had its session at work since. Called without b.mu.
func (n *Machine) stirring(mem *machinev1.RoomMember, state machinev1.RoomDeliveryState, woke time.Time) bool {
	if state != machinev1.RoomDeliveryState_ROOM_DELIVERY_STATE_TAKEN {
		return false
	}
	if mem.MachineId != n.machineID || mem.SessionId == "" || n.sessions == nil {
		return true
	}
	s, err := n.sessions.Get(&Principal{Kind: PrincipalOwner}, mem.SessionId)
	if err != nil {
		return false
	}
	last := s.GetProgress().GetLastEventAt()
	return last != nil && last.AsTime().After(woke)
}

// messageStates is what the post office says of messages (by id): "waiting", "taken", "handed", …
func (n *Machine) messageStates(ctx context.Context, ids []string) (map[string]string, error) {
	n.mu.Lock()
	conn := n.conn
	n.mu.Unlock()
	if conn == nil {
		return nil, errors.New("not connected")
	}
	ms, err := n.messageService(conn)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	res, err := ms.Status(ctx, &relayv1.MessageStatusRequest{MessageIds: ids})
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, s := range res.States {
		out[s.MessageId] = s.State
	}
	return out, nil
}

// roomOutboxInStatus is what waits at the post office for rooms' hosts, by room (工单 #74).
func (n *Machine) roomOutboxInStatus() []*machinev1.RoomOutbox {
	b := n.roomBook()
	b.mu.Lock()
	defer b.mu.Unlock()
	by := map[string]*machinev1.RoomOutbox{}
	for _, o := range b.outbox {
		x := by[o.room]
		if x == nil {
			x = &machinev1.RoomOutbox{RoomId: o.room, HostMachineId: o.host, Since: timestamppb.New(o.at)}
			by[o.room] = x
		}
		x.Replies++
		if o.at.Before(x.Since.AsTime()) {
			x.Since = timestamppb.New(o.at)
		}
	}
	out := make([]*machinev1.RoomOutbox, 0, len(by))
	for _, x := range by {
		out = append(out, x)
	}
	slices.SortFunc(out, func(a, b *machinev1.RoomOutbox) int { return a.Since.AsTime().Compare(b.Since.AsTime()) })
	return out
}
