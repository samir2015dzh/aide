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

## Not yet (G2-b, G2-c)

Rooms with several members and @-mentions, coding agents in rooms (a
session each), rooms in the roster, the round table (rounds, stop, `[FINAL]`),
unread counts and push.
