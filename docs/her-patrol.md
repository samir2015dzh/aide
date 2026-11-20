# Her's patrols: routines that only look

> **2026-11-20: Her has no action approvals.** A patrol does not bring them back. Nothing here waits for the owner's approval of a step: what a patrol may not do is refused in the code, and what it would have done reaches the owner as a suggestion, which they take up, or not, in their own conversation.

Status: built 2026-11-20 (HANDOVER.md, borrow list item 18). It builds on
the routines (her-core `routine.rs`, her-server `routines.rs`)
and the tool policy of `her-core/src/policy.rs`.

## 1. What it is

A patrol is a routine whose runs look for problems and fix none. The owner
wants Her to go looking on her own, "but not to act on her own": a patrol
checks, keeps notes, and says what it would do. The owner decides.

A routine is a patrol or not by one flag, `patrol`, kept with the routine:

- the bot sets it with `manage_routine` (`create` or `update`, `patrol:
  true`), when the owner asks for something checked but not touched;
- the owner sets it in the desktop app: the routine's page has a switch,
  **只读巡查 / Only Look**, and the list marks a patrol with **巡查 / Patrol**.
  Both call `bots/routine_set_patrol` (her-protocol `BotRoutineSetPatrolParams`);
- `bots/routine` and the bot's routine list (`RoutineSummary.patrol`) say
  which routines are patrols; `manage_routine get` says so to the model.

It is kept in `her.db`, `routines.patrol` (the database's fourth step).

## 2. What a patrol run may do

The run is an ordinary routine run (a session of its own, with the bot's
folder as its workspace) whose session carries its notes folder
(`SessionView::patrol`). The tool policy, the `before_tool` hook every call
goes through, refuses in a patrol run every call but these:

- a call whose tool says it only reads: `ToolKind::ReadOnly`, judged for the
  call (`kind_for`), so `call_tool` on a built-in tool is judged as that tool;
- `write` and `edit` of a path inside the notes folder, directly or through
  `call_tool`; the path is resolved and its links followed before it is
  compared, so `..` and a link out of the folder do not get out.

Everything else is refused, however it is reached:

- **The shell.** `bash` changes things; it is refused whatever the command,
  `ls` too. A patrol reads with `read` and the document tools.
- **Connectors.** Every MCP tool, directly or through `call_tool`, even one
  that says it only reads: `readOnlyHint` is the server's claim, used for
  running calls side by side, never trusted (her-mcp `types.rs`).
  `tool_search`, which only lists tools, is allowed.
- **Tools that say nothing.** `Tool::kind` is `Mutating` unless a tool says
  otherwise, so a tool added later without a kind is refused, not let
  through. Tools that act on other agents or the user but may run beside
  other calls (`spawn_agent`, `send_message`, `stop_agents`, `send_to_user`,
  `connector_connect`) are `ToolKind::Concurrent`: they keep running in
  parallel elsewhere, and a patrol may not call them.
- **Sub-agents and the mesh.** A routine run has neither (it gets no agent
  tools, no mesh, no connectors' setup, no `manage_routine`); were they
  there, they are not read-only and would be refused.

A refusal starts with `This routine is a patrol: it only looks`, names the
tool, and tells the model not to try another way but to write it down as a
suggestion.

## 3. Notes and suggestions

The notes folder is `~/.her/bots/<bot>/patrol/<routine>/`, beside the bot's
working folder and its memory, made when a run starts. It stays between runs:
what the patrol found last time is there for the next. The run's brief names
it (`your notes, in \`…\``).

What the patrol was refused becomes the owner's to decide, by code, not by
the model's good will: the run's report gets, after its reply, a list of
every refused call (its title, such as `touch patrol-bash.txt` or `notes:
Echo`) under

> Not done, as this routine only looks. Suggested for the user, who decides whether to do it:

A run that was refused anything is never quiet, even if its reply starts
with `NOTHING_TO_REPORT`: the report goes to the bot, who passes it on with
`send_to_user`, and the routine page shows it with the run's steps (each
refusal and its reason). The owner then asks Her, in the conversation, to do
what they want done.

## 4. Where it is

| Part | Where |
|---|---|
| The flag | her-protocol `RoutineSummary.patrol`, `BotRoutineSetPatrolParams`, `bots/routine_set_patrol`; her-server `routines.rs` (`SCHEMA_V4`, `bots_routine_set_patrol`); aided `internal/herhost/allow.go` |
| The run | her-core `routine.rs` (`RoutineRun::patrol`, `manage_routine`'s `patrol`), `agents/run.rs` (the brief, the refused list), `kernel.rs` / `session.rs` (`patrol` in the session) |
| The refusal | her-core `policy.rs` (`patrol_refusal`); her-tools `ToolKind` (`Mutating` by default, `Concurrent`) |
| The app | desktop `her/RoutinesPanel.tsx` (`PatrolSwitch`, the row's tag). Android does not show it yet |

## 5. Tests

- her-core `policy.rs`: what a patrol may and may not call, the notes folder
  and a way out of it, a tool that says nothing, a connector's tool that says
  it only reads, `call_tool` either way.
- her-core `tests/routines.rs`: a patrol run reads and writes its note,
  nothing else changes, and the refused calls reach the report and the bot,
  quiet reply or not.
- her-server `routines.rs`: the flag survives a restart, a run gets its
  folder, the switch turns it off.
- e2e `her_patrol_test.go` (TestHerPatrol): the owner asks Her for a patrol,
  the app's switch turns it off and on, a run tries the shell, a file, a file
  through `call_tool`, a connector's tool directly and through `call_tool`;
  each is refused, reading and the note work, nothing changed on the machine,
  and Her passes the suggestions on.
- desktop `e2e/her.spec.ts`: the switch on a routine's page, the row's tag.
