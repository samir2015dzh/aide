# Session insights

A session's review, for whoever dispatched the work: how its turns went and
what a better prompt for such work would say. It goes into the delivery
record and feeds the next dispatch.

## 1. What it says

`SessionService.GetSessionInsights` (read-only, `ACCESS_READ`) returns a
`SessionInsights`, worked out from the session's stored events on its
machine:

| | From |
|---|---|
| Each turn's time | `turn_started` to `turn_completed` (a turn still running: to its last event) |
| Failures by kind | failed turns (`turn_completed.failure`, stalled and interrupted ones too); the failure the machine retried a turn after (the turn's last `error` before it went back to `queued`); a failure with no turn in progress (the agent could not start) |
| Retries | `auto_retries`: the turn went back to `queued` while in progress (the machine retrying after a rate limit, or in a fresh agent session); `retried_turns`: a turn that started after a failed one (not an interrupted one) |
| Stalls, interrupts | the failed turns of kind `stalled` and `interrupted` |
| Rollbacks | `rolled_back` events, and the turns each discarded (`turn`+1 to `last_turn`, the latest turns of those numbers, marked `rolled_back`; a rollback numbers the next turns again) |
| Questions | `question_asked` to `question_resolved`, per turn and in all; one still waiting counts 0 |
| Tool calls | `tool_call` by kind, and those whose `tool_result` was an error |
| Changes | each turn's `diff_stat`; the session's cumulative one |
| Tokens | the sum of the turns' `usage` (failed turns report none) |

Times are the machine's clock, in milliseconds. A question's wait counts
in the turn it was asked in.

## 2. Findings and the suggested prompt

Rules, no model: the review never asks a coding agent or any account, so it
costs nothing and needs nothing set up.

| Finding | When |
|---|---|
| `clarifying_questions` | the agent asked a question |
| `follow_ups` | 2 or more messages came after the first prompt |
| `repeated_failures` | 2 or more turns failed (not counting interrupted ones) |
| `stalled` | a turn stalled |
| `interrupted` | someone interrupted a turn |
| `rolled_back` | the session was rolled back |
| `missing_context` | 10 or more read and search calls, over 3 times the edits |

The suggested prompt is the session's first prompt, then sections as the
findings call for them: what later messages added (each once), the answers
the agent got (`question → answer`), a `Done when` placeholder (follow-ups or
questions), a `Context` section with the files it edited (missing context),
and `Constraints` (small steps that report, for a stall; the approach not
to take again, for a rollback; where to stop, for an interrupt; stop on a
repeated failure). Placeholders in `<…>` are for the
dispatcher to fill in.

## 3. When it is made

Never at the end of a turn, so it costs a session nothing while it works:

- when the session is archived, off the archive's own path;
- when someone asks (`GetSessionInsights`).

The machine keeps the last review in its `kv` table (`insights/<session>`)
with the event it reached, and returns it while no event has come since;
deleting the session deletes it.

## 4. Where to read it

- CLI: `aide session insights <id>`: Markdown to paste into a delivery
  record; `-o json` gives `insights` ([cli-output.md](cli-output.md) §4).
- MCP: `session_insights`, `readOnlyHint` ([mcp-gateway.md](mcp-gateway.md) §7).
- Desktop: the session page's info panel, under the session's cards:
  **Review This Task** shows the Markdown, to copy whole or just the
  suggested prompt.
- Android: not yet.

Who may read the session may read its review: another client gets
`not_found`, as for the session.
