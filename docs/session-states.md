# Session states: what a session does, and how fresh that is

Every surface (the desktop app, the Android app, the CLI) says two things of
a session, and never folds them into one colored dot:

1. **its state**: what it is doing (§2), as its machine last said;
2. **its link**: how fresh that is (§3): its machine answers, or the turn is
   quiet, or the machine has not been heard from and what is shown is last
   known.

The reason is one failure, seen on another system: a connector dropped, the
session it ran stayed "running" for good, and the dispatcher, reading only
that, took a finished turn for a hung one. Here a session whose machine is
gone is never shown as running: it reads **lost · last known running · 7 min
ago**, and the CLI says the same in fields a program reads (§6).

One table works both out, in three languages checked against the same cases:
[internal/sessionstate](../internal/sessionstate/sessionstate.go) (the CLI,
and the core), [packages/core states.ts](../packages/core/src/states.ts) (the
desktop app), and the Android app's
[States.kt](../apps/android/app/src/main/java/io/aide/android/model/States.kt);
the cases are
[internal/sessionstate/testdata/cases.json](../internal/sessionstate/testdata/cases.json).
The enums are `SessionState` and `SessionLink` in
[session.proto](../proto/aide/session/v1/session.proto). Machines never send
them: whoever shows a session works them out, because the link depends on
what the viewer can reach, and on its clock.

## 1. Inputs

| From | What |
|---|---|
| the machine | `Session.status`, `progress.last_failure`, `progress.last_event_at` (when its last event was recorded; new, set with each event), `updated_at` (stands in for `last_event_at` from older machines) |
| the relay | whether the machine is online, and `last_seen`: while it is offline, **when it last heard from it** (it used to say when it noticed it was gone, `peerQuiet` = 20 s later) |
| the owner's device (aided's replica) | `Machine.unreachable` / `syncing` (as before), and new: `heard_at`, `unreachable_since`, `lost_spans` (§4); `GetStatusResponse.state_limits` |
| the CLI | what it just read (the machine answered: that is a heartbeat), the relay's `last_seen`, and the sessions it last read of each machine (§6) |

## 2. State

| State | From | zh-CN | en |
|---|---|---|---|
| `queued` | QUEUED, PREPARING | 排队中 | Queued |
| `running` | RUNNING | 运行中 | Running |
| `waiting_input` | WAITING_INPUT | 等你回答 | Waiting for you |
| `waiting_approval` | (none yet: coding sessions ask no approvals; reserved for Her's cards) | 等你审批 | Waiting for approval |
| `done_unseen` | IDLE, finished, not opened by the owner (once sessions keep what the owner saw: the attention triage work) | 已完成未看 | Done, not opened |
| `done` | IDLE, no failure | 已完成 | Done |
| `failed` | CRASHED; IDLE whose last turn failed | 出错 | Failed |
| `interrupted` | IDLE whose last turn was stopped on purpose | 已中断 | Interrupted |
| `archived` | ARCHIVED | 已归档 | Archived |

How a session moves between them is the machine's (docs/reliability-plan.md):
a message queues a turn (`queued` → `running`); a question pauses it
(`waiting_input`) until answered; the turn ends `done`, `failed` or
`interrupted`; the next message starts another; archiving is the only way out.
`done` and `done_unseen` differ only in whether the owner opened it.

## 3. Link

| Link | When | Shown (zh-CN) | Shown (en) | May be acted on |
|---|---|---|---|---|
| `live` | the machine answers | 运行中 | Running | yes |
| `stalled` | live, `running`, no event for the **stall limit** | 运行中 · 12 分钟无新事件 | Running · quiet 12 min | yes |
| `checking` | not heard from (or not readable) for less than the **lost limit** | 运行中？· 机器未应答 | Running? · machine not answering | no |
| `lost` | not heard from for the lost limit or longer; or its machine is not listed | 失联 · 最后已知 运行中 · 7 分钟前 | Lost · last known Running · 7 min ago | no |
| `syncing` | back, being read again, heard from now | 运行中 · 正在同步 | Running · catching up | no |

**Heartbeat.** A machine is heard from while the relay holds its connection
(the relay closes one silent for 20 s) and this device follows it without
error. While it is, `heard_at` is unset; once it is not, `heard_at` says when
it last was: the relay's `last_seen` when the relay says it is offline (the
true last byte), else this device's last look (every 2 s). So a machine that
dies silently reads `checking` from about 20 s (when the relay notices) and
`lost` at 30 s after its last byte; one that stops cleanly, at once and at
30 s. This device losing the relay itself stops every heartbeat the same
way: its sessions go `lost` too, though nothing says their machines are
offline.

**Limits.** Set on each device, the CLI included:

| | Default | Variable | Why |
|---|---|---|---|
| lost | 30 s | `AIDE_LOST_AFTER` | the relay's 20 s plus a reconnect's few: a blip never reads lost |
| stall | 5 min | `AIDE_STALL_AFTER` | past a model's longest usual think; the machine's own watchdog stops a quiet turn at 15 min |

A value is a Go duration (`90s`, `10m`) or seconds (`90`). The core sends its
limits to its screens (`GetStatusResponse.state_limits`).

**Recovery.** Once the machine answers again its sessions are read again
(state-sync §4.3): each shows what it really is (the turn done meanwhile, or
`failed` if aided restarted), its link `live`, by itself.

## 4. Lost spans

The owner's device keeps, per machine, the times it was lost (not heard from
for the lost limit or longer): from its last heartbeat to when it was heard
from again, a day's worth, at most 20, across restarts (`owner.db`,
`lost_spans`). `Machine.lost_spans` carries them; a session's page shows the
last one during the session: **曾失联 14:02–14:20 / Out of touch 14:02–14:20**
(**失联自 14:02** while it lasts). An outage that ends between two looks,
past the limit, is kept all the same.

## 5. Where it shows

- **Desktop**: a row whose machine is not live shows the line (a dashed grey
  ring before it) in place of its mark; a stalled row shows "quiet 12 min"
  before its clock; a session's header shows the line, "quiet …" when
  stalled, and the last lost span. Sessions not live are neither in
  progress nor waiting, and cannot be answered, interrupted or archived
  (`withKnownState`, as before, now also once a machine is not heard from).
- **Android**: the same line in the row's status, the session page's
  header, and its bottom bar.
- **Notifications** are unchanged: they never fire for a session not live.

## 6. The CLI

Every session object (`session get`, `list`, `start`, `wait` …) has, besides
`status` (the machine's, unchanged):

| Field | |
|---|---|
| `state` | §2's names |
| `link` | §3's names |
| `state_text` | the line, in English: `running · quiet 12m`, `lost · last known running · 7m ago` |
| `stale` | what is shown is last known (`checking`, `lost`, `syncing`) |
| `machine_online` | the machine answered this command |
| `read_at` | when this client read it from its machine |
| `heartbeat_at`, `heartbeat_age_seconds` | when its machine was last heard from: now, for one just read |
| `last_event_at`, `last_event_age_seconds` | its last event (null when not known) |
| `stalled_seconds` | how long a stalled turn has been quiet (0 otherwise) |

The CLI keeps the sessions it last read of each machine
(`$AIDE_HOME/sessions-seen.json`, the newest 100 per machine). While a
machine cannot be read:

- `session list` lists them under **`lost_sessions`**, never under
  `sessions`: a program that reads only `sessions` and `status` sees no
  running session of a machine that is gone; the text output lists them
  under "LOST".
- `session get` still fails as `unreachable` (exit code 4), and its JSON
  carries the session as last read, `link` `checking` or `lost`.

For a dispatcher: `link` tells whether to believe `state`. `live` and
`stalled` are the machine's word now; `stalled` with a large
`stalled_seconds` is the one to look into (`session events --after`); never
act on a `lost` session's `state` without reading it again once its machine is
back.

## 7. Compared with Agents Anywhere

What AA's DSH connector does (its npm package `@agents-anywhere/dsh-bridge-next`
2.1.0, read only): a heartbeat every 20 s, a reconnect every 3 s, and, on
connect, a sync of every existing session; each session carries
`lastActivityAt` (its last event's time) and the items of its timeline
`running` / `done` / `failed` / `interrupted`. Its lists show a status dot and
the last activity time.

| Situation | AA | Here | Why |
|---|---|---|---|
| running | dot + last activity | `running`, with the turn's clock | same |
| idle | dot + last activity | `done` / `failed` / `interrupted`, with when it changed | finer: why it stopped |
| quiet for long | the same running dot | `running · quiet 12 min` (stall limit) | the dispatcher's main question: really at work? |
| connector / machine gone | the last reported state stays (the 2026-11-08 failure: "running" for good) | `checking`, then at 30 s `lost · last known running · N min ago`, never "running"; the CLI the same, in fields | the work order's rule |
| just back | sync of existing sessions, states realign | read again (`syncing`), states realign by themselves; the lost span kept | same, plus the trace |
