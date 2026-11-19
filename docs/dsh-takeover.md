# Taking over a DSH session

The owner can take over one of DeepSeek Harness's own sessions (opened in
DSH Desktop, or by another tool) from Hermesh: send it messages and stop
its turn, from the desktop app or the phone. Work order: the handover
repository's "接管 DSH 原生会话并发送消息" (the owner's ruling: A2, the way
Agents Anywhere does it). Inventory and measurements: `DSH-TAKEOVER.md` and
`TAKEOVER-MEASURE.md` there.

## 1. Why a plugin

A DSH session is owned by one DSH Host at a time: one process writes it,
holding the session's write lock (a `flock` on `session.lock`, or on
Windows a named semaphore), from when it opens the session until it closes
it. DSH Desktop holds every session it has open. A second writer can't
have it: DSH's ACP server (`dsh --profile acp`, which Hermesh's own DSH
sessions run in) can't resume a session another process holds, and takes
one prompt at a time, with no queue.

So Hermesh doesn't write a DSH session itself. A plugin in DSH Desktop's
profile (`apps/dsh-bridge`, `@hermesh/dsh-bridge`) calls DSH's own session
controller in-process, as DSH's window does: the Host that owns the
session writes it, and a message sent while a turn runs is queued for the
next turn (`mode: 'queue'`), as one typed in DSH would be.

## 2. The bridge

- **Endpoint**: a Unix socket in `<DSH home>/hermesh-bridge/` (0700), or on
  Windows a named pipe with a random name. `bridge.json` there (0600) holds
  the endpoint and a random token; every request carries the token.
- **Protocol** (one JSON object per line, version 1): `hello` (version,
  which DSH services the Host has), `state {sessionId}` (whether the
  session's Agent runs in this Host), `prompt {sessionId, requestId,
  text}`, `cancel {sessionId}`.
- **Request ids** start `hermesh-`. DSH keeps the id as the message's
  `rpcId` and drops a second message with the same one, so a retry can't
  send twice; and Hermesh reads a message with such an `rpcId` as sent by
  the owner from Hermesh (only the owner sends through the bridge).
- **No silent waits**: the plugin gives DSH 20 seconds to accept a message
  and then answers `timeout` (DSH's own web page was measured to hang
  silently on a held lock); hermeshd gives each call a deadline.

## 3. hermeshd

`MachineAdminService` (owner only; `LocalService` passes them through):

- `SetDSHTakeover`: Hermesh's own per-session switch, kept in the
  machine's store, off for every session until the owner turns it on.
  `DSHSession.takeover` shows it.
- `SendDSHMessage`: the session must be one of DSH's own (not one Hermesh
  started) and taken over. hermeshd dials the bridge (3 s), asks whether
  DSH Desktop runs the session's Agent, and, if not but another DSH process
  holds its lock, refuses before DSH is asked ("another DSH program has
  this session open"). Then it hands the message over (25 s). Refusals are
  FAILED_PRECONDITION, DEADLINE_EXCEEDED, NOT_FOUND or INVALID_ARGUMENT,
  each with what to do; nothing is left pending.
- `InterruptDSHSession`: the live Agent's `cancel({kind: 'user'})`.
- `ListDSHSessions` gives the owner the bridge's state: not installed, not
  running (DSH Desktop closed, or the plugin off), running, incompatible.

The audit log records each switch, send (the message's hash) and stop.

## 4. Installing

DSH reserves the `desktop` profile for DSH Desktop: the npm `dsh` refuses
to manage its plugins. Installing is DSH Desktop's job, in its Plugins
page, whose plugin manager checks the plugin against the running DSH
(`engines.dsh`), restores the profile when an install fails, keeps a plugin
that won't load from taking the Host down, and turns it off or removes it.

Hermesh prepares (`hermeshd dsh-bridge prepare`, `PrepareDSHBridge`, or
"准备安装…" in the desktop app): it backs up DSH's configuration files (the
DSH home's and each profile's `package.json`, `cordis.patch.yml` and the
like; not sessions or packages) into
`<DSH home>/hermesh-bridge/backups/<time>`, and writes the plugin's package
there (hermeshd carries it: `apps/dsh-bridge/embed.go`). The owner installs
that file in DSH Desktop › Plugins and restarts DSH Desktop.
`hermeshd dsh-bridge status` says whether the plugin answers.

## 5. Apps

On a DSH session's page (desktop and Android): a 接管 switch, which asks
first what it means; taken over, a box to write in, Send, and Stop while a
turn runs. A failed send says it was not sent and why, keeps the text, and
shows the plugin's state; a timeout keeps the message's id for the retry.
Messages sent from Hermesh read "经 Hermesh 发送".

Separately, every message in a Hermesh session now names its writer
(`UserMessage.from`, `from_name`): the owner joining a session one of
their agents started is shown as such.

## 6. Not known yet (to check on the owner's computer)

- Whether DSH Desktop's Plugins page installs from a file path (the plugin
  manager's API takes paths and tarballs).
- Where DSH Desktop keeps its profile when it is not at
  `<DSH home>/profiles/desktop` (then it is not backed up; the app says so).
- That DSH Desktop loads the plugin and its services are as read from DSH
  0.2.2-rc.5's packages (the plugin was not run inside a real DSH here).
