# Taking over a DSH session

The owner can take over one of DeepSeek Harness's own sessions (opened in
DSH Desktop, or by another tool) from Aide: send it messages and stop
its turn, from the desktop app or the phone. Work order: the handover
repository's "接管 DSH 原生会话并发送消息" (the owner's ruling: A2, the way
Agents Anywhere does it). Inventory and measurements: `DSH-TAKEOVER.md` and
`TAKEOVER-MEASURE.md` there.

## 1. Why a plugin

A DSH session is owned by one DSH Host at a time: one process writes it,
holding the session's write lock (a `flock` on `session.lock`, or on
Windows a named semaphore), from when it opens the session until it closes
it. DSH Desktop holds every session it has open. A second writer can't
have it: DSH's ACP server (`dsh --profile acp`, which Aide's own DSH
sessions run in) can't resume a session another process holds, and takes
one prompt at a time, with no queue.

So Aide doesn't write a DSH session itself. A plugin in DSH Desktop's
profile (`apps/dsh-bridge`, `@aide/dsh-bridge`) calls DSH's own session
controller in-process, as DSH's window does: the Host that owns the
session writes it, and a message sent while a turn runs is queued for the
next turn (`mode: 'queue'`), as one typed in DSH would be.

## 2. The bridge

- **Endpoint**: a Unix socket in `<DSH home>/aide-bridge/` (0700), or on
  Windows a named pipe with a random name. `bridge.json` there (0600) holds
  the endpoint and a random token; every request carries the token.
- **Protocol** (one JSON object per line, version 1): `hello` (version,
  which DSH services the Host has), `state {sessionId}` (whether the
  session's Agent runs in this Host), `prompt {sessionId, requestId,
  text}`, `cancel {sessionId}`.
- **Request ids** start `aide-`. DSH keeps the id as the message's
  `rpcId` and drops a second message with the same one, so a retry can't
  send twice; and Aide reads a message with such an `rpcId` as sent by
  the owner from Aide (only the owner sends through the bridge).
- **No silent waits**: the plugin gives DSH 20 seconds to accept a message
  and then answers `timeout` (DSH's own web page was measured to hang
  silently on a held lock); aided gives each call a deadline.

## 3. aided

`MachineAdminService` (owner only; `LocalService` passes them through):

- `SetDSHTakeover`: Aide's own per-session switch, kept in the
  machine's store, off for every session until the owner turns it on.
  `DSHSession.takeover` shows it.
- `SendDSHMessage`: the session must be one of DSH's own (not one Aide
  started) and taken over. aided dials the bridge (3 s), asks whether
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

Aide prepares (`aided dsh-bridge prepare`, `PrepareDSHBridge`, or
"准备安装…" in the desktop app): it backs up DSH's configuration files (the
DSH home's and each profile's `package.json`, `cordis.patch.yml` and the
like; not sessions or packages) into
`<DSH home>/aide-bridge/backups/<time>`, and writes the plugin's package
there (aided carries it: `apps/dsh-bridge/embed.go`). The owner installs
that file in DSH Desktop › Plugins and restarts DSH Desktop.
`aided dsh-bridge status` says whether the plugin answers.

## 5. Apps

On a DSH session's page (desktop and Android): a 接管 switch, which asks
first what it means; taken over, a box to write in, Send, and Stop while a
turn runs. A failed send says it was not sent and why, keeps the text, and
shows the plugin's state; a timeout keeps the message's id for the retry.
Messages sent from Aide read "经 Aide 发送".

Separately, every message in an Aide session now names its writer
(`UserMessage.from`, `from_name`): the owner joining a session one of
their agents started is shown as such.

## 6. The list: a machine gone, an empty list

- **A machine that can't be reached** keeps its DSH sessions in the list as
  this device last read them (`DSHSessionEntry.lost`, `read_at`; kept in the
  owner's process, not on disk), as Aide's own sessions are kept
  (docs/session-states.md): **失联 · 最后已知 在 DSH 中打开 · 3 分钟前**,
  never as what they do now, and with no switch or box to write in. Their
  conversation is read once the machine is back. The relay itself gone, every
  other machine's are lost the same way; this computer's are read as ever.
- **Which DSH home was read**: each machine says (`dsh_home`). With no
  session found, the page shows it, and that DSH Desktop keeping its data
  elsewhere is fixed by setting the user environment variable `DSH_HOME` to
  that folder (aided reads the user's environment as DSH does).
- The Agent page of DeepSeek Harness (Settings › Machines) links to the page.

## 7. Checked against DSH 0.2.2-rc.5's packages (not run)

What the plugin relies on, read from the npm packages of the version on the
owner's computer (`@deepseek-ai/dsh@0.2.2-rc.5`) and from Agents Anywhere's
bridge (`@agents-anywhere/dsh-bridge-next@2.1.0`), which calls the same:

- `sessionController.prompt({sessionId, requestId, mode: 'queue', content},
  signal)`: a cold session is resumed by it ("prompt … may resolve or resume
  an ordinary Session"); a retry with the same `requestId` is answered with
  the first acceptance; the in-process call is the owner's (a `principal`
  is recorded only for a non-owner Remote caller). AA calls it the same
  way, then `sessions.flush(agent.session)`, as the plugin does.
- `agents.get(id)?.cancel({kind: 'user'})`: as AA. Cancelling needs the
  session live, so `cancel` answers "not running here" otherwise.
- `ctx.effect(…)`: Cordis 4.1.0 (rc.5's) has it.
- `engines.dsh` `>=0.2.2-rc.1 <0.3.0-0`: DSH checks `engines.dsh` alone when
  a plugin declares it, and prereleases take part in the range, so rc.5 and
  a 0.2.2 release load it.
- Installing from a path: the plugin manager reads an absolute path's
  `package.json` (and a tarball after fetching it); DSH Desktop brings its
  own package manager for this ("application-owned profiles supply their
  bundled package-manager invocation"), where the npm `dsh plugin` needs
  pnpm on PATH.

## 8. Not known yet (to check on the owner's computer)

- That DSH Desktop's Plugins page offers installing from a file (the plugin
  manager takes paths and tarballs; whether its page asks for one).
- Where DSH Desktop keeps its profile when it is not at
  `<DSH home>/profiles/desktop` (then it is not backed up; the app says so).
- That DSH Desktop loads the plugin and its services answer as read above
  (the plugin was not run inside a real DSH here: running DSH is not allowed
  in the environment this was built in).
