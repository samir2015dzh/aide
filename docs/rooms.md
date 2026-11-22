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
  the owner's. At the round limit (default 6, at most 20) the room stops and
  says so. Inside the room its rounds, not the hop limit, bound the chain
  (design §10).
- **`[FINAL]`**: an answer starting with it is the room's agreed result,
  sealed with its SHA-256, shown as a card to copy or export as Markdown.
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
