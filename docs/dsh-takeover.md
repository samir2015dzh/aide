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
  which DSH services the Host has, and `features` beyond the first
  plugin: `options`, `selections`, `from`, `outbox`), `state {sessionId}` (whether the
  session's Agent runs in this Host), `prompt {sessionId, requestId,
  text, model?: {provider, model, effort?}, permission?}`, `options
  {sessionId}`, `cancel {sessionId}`, `outbox {waitMs}`, `outboxDone
  {id, ok, …}`.
- **Agents both ways** (plugin 0.3.0, features `from` and `outbox`;
  docs/agent-messages.md §15.3): `prompt` may carry `from: {address,
  name}` and `messageId`, which the plugin puts before the text for the
  session's model; the plugin registers the tool `aide_message_send` in
  the Host's tool registry (`ctx.get('tools').register`, a
  `ToolDefinition` written out: JSON Schema parameters and output, so the
  plugin needs nothing of DSH's at run time), and keeps what sessions send
  for aided: `outbox {waitMs}` hands them out (once each, waiting up to 25
  s), `outboxDone {id, ok, messageId?, to?, error?}` answers the tool,
  which otherwise fails after 30 s ("is Aide running?"). The plugin never
  sends anything itself: aided decides who a send may go to, and sends it.
- **Model and permissions** (plugin 0.2.0), as DSH's own pickers and
  Agents Anywhere's bridge set them: `options` lists DSH's models
  (`llm.listProviders`, `listModels`, each model's reasoning efforts from
  `resolveModelInfo`; a provider DSH can't list now is left out) and
  permission presets (`permissionPresets.catalog()`, not `custom`), and
  for a session DSH Desktop has open, its own (`sessionProjections`
  `modelSelection`, `permissionPresets.current`). A prompt that names
  them opens the session (`sessionController.resolveAgent`), checks the
  model (`llm.resolveCallConfig`), and sets what differs:
  `sessionController.selectModel`, and the session's `/permission
  <preset>` command; then queues the message. A selection DSH refuses
  stops the message (nothing is sent). aided sends selections only to a
  plugin that lists `selections`; an older one is told to be updated.
- **Request ids** start `aide-`. DSH keeps the id as the message's
  `rpcId` and drops a second message with the same one, so a retry can't
  send twice; and Aide reads a message with such an `rpcId` as sent from
  Aide: by the agent the machine recorded for that id (below), else by the
  owner.
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
- `DSHSessionOptions`: the plugin's `options`. `SendDSHMessage` takes
  `model` (`DSHModel`) and `permission`.

For an agent (`MachineService`, the owner's ruling of 2026-11-20: the
dispatcher may write, only where the owner opened the door):

- `SendDSHMessage` and `DSHSessionOptions`: only a DSH session the agent
  may read (as `ReadDSHSession`: its grant sees sessions it did not start,
  in the session's folder) **and** the owner took over; otherwise
  NOT_FOUND, or FAILED_PRECONDITION "the owner has not taken over this DSH
  session". Turning takeover on, stopping a turn and preparing the plugin
  stay the owner's.
- The machine records each agent's message (`dsh-sent/<request id>`: its
  client id and name) before it goes; reading the session, that message is
  the agent's (`UserMessage.from`, `from_name`), shown "Muse 经 Aide 发送".
  Another sender can't reuse the id. The audit log names the sender.
- The CLI: `aide message send --to dsh_… --message … [--model
  provider/model[/effort]] [--permission <preset>]`; `aide session get
  dsh_…` shows `takeover` and, taken over, the choices.
- `ListDSHSessions` gives the owner the bridge's state: not installed, not
  running (DSH Desktop closed, or the plugin off), running, incompatible.

The audit log records each switch, send (the message's hash) and stop.

## 4. Installing

DSH reserves the `desktop` profile for DSH Desktop: the npm `dsh` refuses
to manage its plugins. Installing is DSH's job, through DSH Desktop's
plugin manager, which checks the plugin against the running DSH
(`engines.dsh`), restores the profile when an install fails, keeps a plugin
that won't load from taking the Host down, and turns it off or removes it.
aided never writes into the profile.

**Handed to DSH (工单 #59).** The owner presses 让 DSH 装好它 (Connect ›
DSH, the DSH page, or a bot's workbench on the phone) once. aided
(`InstallDSHBridge`) prepares, as below, then starts a DSH session in
`<DSH home>/aide-bridge` (the project `aide-dsh-bridge`, worked in place)
whose task (`dshnative.InstallPrompt`) is: install the package with DSH's
plugin manager; when it fails, read the operation's `pnpm.log` rather than
DSH's message, and find what really stops it; check the profile for
packages an earlier failure left half removed and repair them; restart DSH
Desktop; check with `aided dsh-bridge status`; and report with
`aided dsh-bridge report --ok` or `--failed --reason "<why, for the owner>"`.
One already under way is not started twice.

What it was written from: on a Windows machine DSH Desktop said "没有写入权限"
(no write permission), but the folder's ACL was fine. The operation's
`pnpm.log` had `EPERM … rename <pkg>_tmp_… -> <pkg>` for a package whose
executable another plugin's resident connector was running: Windows will
not replace a running image, and DSH sorts every EPERM as a permission
problem. The failed install had also removed most of another package (its
`package.json` gone), and the repair after it hit the same lock.

The report lands in `<DSH home>/aide-bridge/install-report.json` (the
install itself in `install.json`). aided's DSH loop (every 5 s) checks the
agents again when a report comes and when the plugin starts answering, so
the Connect page follows without the owner asking: DSH at it (with its
task to open), failed (DSH's reason as written), installed but not answering
(restart DSH Desktop), installed. A task that ended without a report shows
as failed, saying to open it.

**By hand**, folded under 让 DSH 装好它 for when DSH can't: Aide prepares
(`aided dsh-bridge prepare`, `PrepareDSHBridge`): it backs up DSH's
configuration files (the DSH home's and each profile's `package.json`,
`cordis.patch.yml` and the like; not sessions or packages) into
`<DSH home>/aide-bridge/backups/<time>`, and writes the plugin's package
there (aided carries it: `apps/dsh-bridge/embed.go`). The owner installs
that file in DSH Desktop › Plugins and restarts DSH Desktop.
`aided dsh-bridge status` says whether the plugin answers.

## 5. Apps

On a DSH session's page (desktop and Android): a 接管 switch, which asks
first what it means; taken over, a box to write in, Send, and Stop while a
turn runs. On the desktop, above the box, the model (with its effort) and
the permission preset the session goes on with, from the plugin's
options (left as they are unless changed); Android sends without them. A failed send says it was not sent and why, keeps the text, and
shows the plugin's state; a timeout keeps the message's id for the retry.
Messages sent from Aide read "经 Aide 发送"; an agent's, "<name> 经 Aide
发送".

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
