# Her's confirmation rules (2026-11-20)

> Not an approval mechanism. Her has no action approvals (2026-11-20): no approval card, no
> signer, no admission step, nothing that holds a tool call until the owner approves it. Asking
> first is a courtesy Her shows in the conversation, as a capable colleague would. The rules here
> decide one thing: whether she asks in the chat before an action of a kind. Nothing here stops
> or delays a tool call.

Status: built 2026-11-20 (borrowing list item 5). Builds on [her-memory.md](her-memory.md) and
the "acting with care" part of the bot prompt.

## 1. Goal

Before, "from now on, don't ask me before posting to the team channel" became a line of free
text in `MEMORY.md`. The owner could read it in the memory page but not find, check or delete it
as one thing, and nothing in the code knew of it. Now such a preference is a rule: kept per
assistant, matched by kind of action and target, listed in the apps, deleted there, and judged by
code that holds the hard floor whatever is written.

## 2. A rule

`confirm-rules.json` in the assistant's directory (`bots/<id>/`, beside `memory/`, so clearing
the memory keeps the rules). Each rule:

| field | |
|---|---|
| `id` | `c_` and six letters and digits |
| `action` | `send`, `publish`, `pay`, `delete`, `install`, `grant` |
| `target` | which targets: words with `*` for any run of characters, matched whole, ignoring case; `*` alone is every target |
| `want` | `skip`: do it without asking first; `ask`: always ask first |
| `note` | what the owner said, in their words |
| `created_at` | RFC 3339 |

## 3. The hard floor

Deleting data (`delete`), installing software (`install`) and giving a new client or a new
permission access (`grant`) are asked in the conversation every time. This is held in code
(`her-core/src/confirm_rules.rs`), not only in the prompt:

- `add` refuses a `skip` rule for them and says why; an `ask` rule is kept (asking more is fine);
- `decide` answers "ask first" for them before it looks at any rule, so a rule written into the
  file by hand changes nothing.

## 4. Deciding

`decide(rules, action, target)`:

1. hard floor: ask;
2. the rules of that action whose target matches; if any says `ask`, ask: **where rules
   disagree, asking wins**;
3. if any says `skip`, go ahead without asking;
4. no rule: ask first (the default the prompt already had for what is hard to undo or reaches
   other people).

An unreadable file covers nothing: ask.

## 5. How Her uses them

A bot's session has the tool `confirm_rules` (group `bot`), with `op`:

- `check` (action, target): before she sends, publishes or pays. The result starts "Ask first"
  or "No need to ask", with the reason;
- `add` (action, target, want, note): when the owner says not to ask again about a kind of action
  ("以后这类不用问我"), or always to ask first. She keeps it here, not in `MEMORY.md`;
- `list`; `remove` (id): when the owner takes a preference back.

The bot prompt ("acting with care") says to check before a send, publish or payment and do what
the result says. A `skip` only drops the question before a step of what the owner asked for; it
never starts a send nobody asked for, and the rest of that section (no replies on her own, a
refused send is final) stands.

## 6. In the apps

- `bots/confirm_rules` (`BotIdParams` → `BotConfirmRulesResult`): the rules, oldest first.
- `bots/confirm_rule_delete` (`BotConfirmRuleParams`): deletes one; she asks first again before
  what it covered. An unknown id is an invalid-params error.

Both are allowed through the Her entry (`internal/herhost/allow.go`). New rules come from the
chat only.

Desktop: the assistant's memory page has an "Asking first" section listing the rules (action,
target, what it says) with Delete, behind the confirm sheet. A machine whose her lacks the method
shows nothing. Android: not yet (§8).

## 7. Coding sessions: no link to the rules

The coding sessions' per-session preset is `--mode` (an ACP agent's session mode, checked by
`checkMode` in `internal/machine/defaults.go` and applied in `sessions.go`). Their tool gate
(`internal/machine/gate.go`) allows every call and audits it; Claude runs with skipped
permissions, Codex with `never` and full access, ACP answers `allow_once`. The rules act on Her
only, and the two layers do not meet:

- the gate's allow-everything stays as it is: a rule never reaches it, and nothing new waits on
  the owner;
- `--mode` stays what the caller picks for each session; no default mode is kept per project or
  agent from the rules. Her's mesh starts coding sessions without a mode, so there was nothing to
  carry a rule into, and a stored default mode would be a second preference store with its own
  UI that nobody asked for;
- when Her sends, publishes, pays or deletes through a coding session she starts, she asks (or
  not) in her own conversation, by her rules, before she hands the work over.

## 8. Not built

- The Android app's list and delete (the methods are there; the screen is not).
- A CLI command; the tool's `list` is the place to see them from a conversation.

## 9. Tests

- Unit (`her-core/src/confirm_rules.rs`): matching; no rule asks; a `skip` covers its targets
  only; asking wins a conflict; the hard floor is asked whatever the file says; the store
  refuses to loosen it; keep, list, remove.
- End to end (`e2e/her_confirm_rules_test.go`, on a Her Server reached as the apps reach it, with
  the mock model's `act` and `prefer`): she asks before a send with no rule; after "以后发团队群不
  用问我" she goes ahead, and still asks for another target; the rule is a file on the machine; a
  `skip` for deleting is refused and deleting is asked; an `ask` rule over the `skip` makes her ask;
  `bots/confirm_rules` lists both, and after `bots/confirm_rule_delete` the `skip` holds again, then
  with none she asks again.
