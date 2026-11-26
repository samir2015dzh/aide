# Rooms

The owner and bots talking (design: the handover repository's
`ROOMS-DESIGN-2026-11-22.md`, reviewed and approved). This file says what
is built.

## G2-a: the owner's conversation with a personal agent

A personal agent's bot page has its conversation in the middle: a **direct
room**, the owner and that one agent.

- **Kept on a host machine**, never on the relay. aided keeps each room in
  `<data>/rooms/<id>.json` (the room, its last 2000 messages, the owner's
  idempotency keys). The owner's device finds the agent's direct room on
  whichever online machine hosts it (`OpenDirectRoom`), or makes it on this
  device's own machine, else on the first machine online by name, so the
  desktop and the phone find the same conversation. Reads and posts go to the
  host (`ListRoomMessages`, `PostRoomMessage`, owner only).
- **The owner's words go out one recipient at a time.** For each member the
  host sends its own copy (a message of its own, `msg_` and a ULID, kept in
  the delivery as `copy_id`) from the room's address
  (`<host machine>/<room id>`), with `Message.room` set, a chain of its own
  (`hop` 1: the owner's word), the room's last 8 messages for context, and
  how to answer. This is the host fanning out, not a group-send for agents:
  an agent still answers one to one (agent-messages §3.2 holds).
- **The owner's information boundary, per member's kind.** Before a copy
  goes, the host checks it against the boundary for that member's kind,
  told from its name ("Claude (claude.ai)" is `claude`). A match refuses
  that copy alone, saying so on the owner's message ("没有发给 X：内容触及你的
  信息边界（category）"); the others go.
- **The answer comes back into the room.** An agent answers with
  `aide message send --to <host>/<room id> --reply-to <its copy's id>`.
  The host takes it from the post office, or straight over the tunnel when
  it is online (`DeliverMessage`), from a member only, once (a repeat is
  dropped), and marks the owner's message it answers `replied` for that
  member.
- **On the page**: each of the owner's messages says what became of it:
  sent (waiting for the agent to read it), answered, refused (the boundary,
  in words), or not sent (and why). The page reads the room again every
  3 seconds while it is open. The work the agent handed out moves beside
  the conversation.

## G2-b/c: rooms of several bots

A room has a name, its members (personal agents and coding agents, up to
12), and how it is run. It sits in the roster above the bots, with its last
line and what the owner has not read; its page has the members, how it is
run, and one timeline.

- **Making one.** The owner's device picks the host (this device's own
  machine, else the first online by name) and signs a **charter** for the
  room with its device key (`identity.v1.RoomCharter`: owner, room id, host,
  and for each coding agent its machine, agent and project). The host keeps
  it with the room (`CreateRoom`; the room's id is the charter's).
- **A coding agent works in a session of its own**, one per room, in the
  project chosen for it, started as the owner's (the room's work is the
  owner's). Its first wake starts the session, later ones are sent into it;
  when a turn ends, what it said comes back into the room. The page links
  to the session.
- **Coding agents on another machine.** The host wakes them through the
  post office (`inbox.v1.RoomWake`, sealed machine to machine, queued while
  that machine is away). That machine takes the wake **only with the
  charter**: signed by one of the owner's devices that the owner's root key
  certified (`Trust.VerifyRoomCharter`), naming the machine that sent it as
  the host, and this machine, agent and project as a member. It checks the
  owner's information boundary for that agent's kind again, then starts or
  continues the session; what the agent says goes back to the host as a
  `RoomReply`. A forged charter, or one naming another machine, is refused.
  So two coding agents on two of the owner's machines work in one room.
- **Manual @** (the default): the owner's message wakes only the members it
  names (the chips over the box say whom it will wake); the others' personal
  agents get it for information. A member naming another in its answer only
  suggests it (`asks`), and the page offers "转给 X".
- **Round table**: the owner's message wakes those named, else the first
  member, and starts the room's chain anew (round 0, a trace of its own).
  A member naming another hands it on: the round goes up by one and the one
  named is woken with the speaker's words marked as peer input, never as
  the owner's. At the round limit (default 6, at most 20) the room wraps up
  and stops, saying so. Inside the room its rounds, not the hop limit, bound the chain
  (design §10).
- **`[FINAL]`**: an answer starting with it is the room's agreed result,
  sealed with its SHA-256, shown as a card to copy or export as Markdown.
- **Keeping the table going** (工单 #159):
  - *No one named*: a member's answer naming no member (or only bots not in
    the room) hands on to the next member in the room's order, round and
    round; its letter says so ("轮到你接力"). One naming only the owner
    (`@所有者`) hands nothing on: the table waits for the owner. A word that
    answers no wake of its own hands nothing on either.
  - *Silent members are skipped*: a member woken that has not answered for
    `roomSkipAfter` (3 minutes: a minute after the owner is told it is
    quiet; a coding agent here still at work counts from its session's last
    event) is skipped (`ROOM_DELIVERY_STATE_SKIPPED`), the room saying who and
    how long it waited, and, when the table waits on nobody else, the same
    word goes to the next member in order it has not reached yet. What the
    skipped one says later is kept as late, handing nothing on.
  - *Wrapping up*: at the round limit with no `[FINAL]`, or when a skip
    leaves nobody to hand on to, the last bot to speak is asked to wrap up
    (the host's line names it); its answer is sealed as the room's result,
    `[FINAL]` or not. Silent too, the next in order is asked; with nobody
    left the table stops, said so.
  - *Checked before it starts*: the new-room sheet asks for real whether
    each member picked can be reached (`CheckRoomMembers`): a personal agent's
    grant on the host, its inbox at the post office and whether anything
    takes its letters now (a doorbell, a webhook, a session or hook while
    connected, an MCP connection active lately; a command line that only
    polls does not, online or not); a coding agent checked again on its own
    machine (there, not paused, signed in, its sign-in confirmed), a machine
    that does not answer marked so. Each row says why; a room with one that
    may not take its turn is made only when the owner makes it again knowing
    that.
- **Stop**: nobody more is woken, coding agents at work for the room are
  stopped (on other machines too, with a stop wake), and what comes after is
  kept as late, handing nothing on.
- **Unread and notices**: what the owner has not read is counted from the
  last message they saw (`MarkRoomSeen`, when the page shows the newest). A
  final result, the round limit, or a member naming the owner (`@所有者`)
  sends a notice of kind `room`.
- **The roster follows rooms on the Watch, asking nothing.** Each machine
  puts the rooms it hosts (without their charters) in its status
  (`MachineStatus.rooms`, owner only), which the Watch already looks at every
  second: an idle window still makes no calls. Only an open room's page
  reads its messages (every 3 seconds). The phone tells of a room whose
  unread went up on the Watch, unless that room's page is open.

## 批 H (工单 #69–#71, #73, #74)

- **Taken** (#71): a member's copy goes sent → taken → replied. The host asks the
  post office (relay MessageService.Status) every few seconds what became of
  the copies it sent: `taken`, `handed`, `handled` or `held` is taken;
  `refused` and `expired` say so. A coding agent on the host has it at once.
- **At most 8 at work** (#70): no more than `roomMaxBusy` (8) wakes of the
  host's rooms wait on their members at once. Past that a wake is queued
  (`ROOM_DELIVERY_STATE_QUEUED`, kept in the room's file) and goes, oldest
  first, as soon as one is done (answered, failed, refused, the room
  stopped; a stopped room's queued wakes fail, said so).
- **Quiet for two minutes** (#70): a member woken that shows no sign of life
  for two minutes (a copy not taken; a session here not at work since) is
  told of once: the delivery is marked `reminded`, the room's `notice` says
  so, and a `room` notice goes. Sessions handed out directly are told of on
  the owner's devices (core `quietSessions`, Android `LiveNotices.quietKey`):
  a turn started two minutes ago with nothing from the agent, once a turn.
- **Notified** (#69): `Room.notice`/`notice_at` is what the host last told of
  the room. The desktop's main process posts a notification when it is new,
  or when more is unread, unless the room's page is in front; a click opens
  it. Android shows the host's words the same way.
- **Waiting for the host** (#74): a reply a machine's coding agent sends to a
  room's host waits at the post office while the host is away; the sending
  machine lists it in its status (`MachineStatus.room_outbox`) until the host
  takes it, and the owner's devices show it in the roster and the room.
- **Deleting** (#73): the owner chooses the room alone (its members'
  sessions stay in their projects) or with the sessions started for it,
  listed before deleting.
