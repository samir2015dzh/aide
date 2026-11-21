# Contacts (personas)

> Status: 2026-11-20, borrowed item 19 ("bot as a contact", from Codync and
> Grok Bot's roster). Code: `pkg/relay/personas.go` (kept),
> `internal/owner/personas.go` (talking), the desktop's
> `sheets/Contacts.tsx`, Android's `ui/Contacts.kt`.

A **contact** is a long-lived name the owner hands work to, on top of
sessions: "Reviewer", "Front end", "Billing". The owner picks a contact,
then writes; which session the message goes to is the contact's business.
The real session shows under Sessions as any other, and the contact's page
links to it.

In the code a contact is a `Persona` (`aide.identity.v1.Persona`): `Contact`
and `ContactCard` already name the agents' inboxes
([agent-messages.md](agent-messages.md) §4).

## 1. What a contact is

| Field | |
|---|---|
| `name` | 1 to 64 characters |
| `icon`, `color` | its avatar: one of the built-in icons (§2) and a colour `#rrggbb`; no uploads |
| `machine_id`, `project_id`, `agent` | where its sessions run, and which coding agent |
| `model`, `effort`, `mode` | what they run with, as `StartSessionRequest` takes them; empty follows the machine's defaults for the agent |
| `session_id` | the session it talks in now (the relay's, never the client's) |

`mode` is the agent's own per-session preset (`AgentStatus.modes`), the
same as New Task offers. A contact grants nothing: its sessions are the
owner's, started by the owner's device as New Task starts them, and the
machine checks the model, effort and mode as it does there.

## 2. Avatars

Built in, the same on every screen: `robot` 🤖, `owl` 🦉, `fox` 🦊, `cat` 🐱,
`rocket` 🚀, `wrench` 🔧, `book` 📚, `bug` 🐞, on one of six colours. An
unknown or empty icon shows the name's first letter.

## 3. Talking to a contact (`LocalService.TalkToPersona`)

The owner's device (desktop or phone) reads the contact from the relay,
then:

| The contact's session | What happens |
|---|---|
| none yet | a new session starts with its presets (`reason` "first") |
| idle, running, waiting for an answer, failed or interrupted | the message goes to it (`SendMessage`), as the owner's next message would: a failed turn is retried in the same session, a running one takes it as it takes any |
| archived | a new session starts (`reason` "archived") |
| deleted, or its machine removed | a new session starts (`reason` "gone") |
| on another machine or project than the contact's now | a new session starts (`reason` "changed") |
| its machine can't be reached (offline) | **nothing is sent and nothing starts**: `Unavailable`, the message stays with the owner. A new session elsewhere would split the conversation, and one on the same machine could not start either |

A new session started for the contact becomes its session
(`BindPersonaSession`). If that fails, the session still runs and the next
message starts another (logged). Two devices writing to a contact without
a session at the same moment each start one; the last becomes its session.

Changing a contact's machine, project, agent, model, effort or mode forgets
its session (the relay does it on `SavePersona`): the next message starts a
new one with the new presets. A new name, icon or colour keeps it.

Deleting a contact deletes only the contact. Its sessions stay, listed as
before.

## 4. Where contacts are kept

On the owner's server (the relay's own database, table `personas`), through
`OwnerControlService.ListPersonas`, `SavePersona`, `DeletePersona` and
`BindPersonaSession`, which only the owner's devices may call (a device
cert, as doorbells and contact cards). So every device of the owner has the
same contacts and the same session under each, with nothing new to log in
to or approve.

Not on a machine: a contact is the owner's, not a machine's, and must be
listed while its machine is offline. Not on each device (`owner.db`): the
phone and the desktop would each have their own. Erasing everything
(`EraseOwner`) forgets them with the rest; a relay without the store answers
`Unimplemented`. A computer that is only a machine keeps none.

## 5. Where the owner finds them

- **Desktop**: the people button beside New Task at the head of the task
  list opens Contacts: the list, the session the one picked works in, and a
  message box (⌘↩ sends). Sent, the session opens. Add Contact… and Edit
  change one; Delete Contact is in Edit.
- **Android**: Sessions' **派活 / Hand Work** button opens the contacts;
  picking one opens its page (what it works with, the session it works in,
  a message box). Sent, the session's page opens over it. ＋ adds one, Edit
  changes or deletes it. This is the phone's first way to start work.

## 6. Not done

Uploaded avatars; a contact per agent of Her; a queue for a contact whose
machine is offline (borrowed item 2's queue would carry it).
