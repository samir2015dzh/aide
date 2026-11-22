# @aide/dsh-bridge

A DeepSeek Harness (DSH) plugin that lets this computer's Aide take over
DSH sessions: send the owner's messages into a session (queued as its next
turn, as DSH's own window queues them while a turn runs) and stop its
current turn. It runs inside the DSH Host (DSH Desktop's `desktop` profile)
and calls DSH's own session controller in-process, so the Host that owns a
session is the one that writes it: no second writer, no lock taken behind
DSH's back.

- **Who can reach it**: only this user's aided. It listens on a Unix
  socket in `<DSH_HOME>/aide-bridge/` (0700) or, on Windows, a named pipe
  with a random name; `bridge.json` there (0600) holds the endpoint and a
  random token that every request must carry.
- **What it does**: `hello`, `state`, `prompt` (queue mode, with Aide's
  request id, which DSH records as the message's `rpcId` and uses to drop a
  retry), `cancel`. A prompt DSH has not accepted within 20 seconds fails as
  `timeout`; nothing is left silently pending.
- **Agents both ways** (0.3.0): a prompt from one of the owner's agents
  says, before the text, who wrote it; the plugin registers the tool
  `aide_message_send`, whose sends it keeps for aided (`outbox`,
  `outboxDone`). aided decides who a send may go to (an agent, only from a
  session the owner took over) and sends it; the plugin sends nothing
  itself.
- **What it does not do**: read or write DSH's session files, change
  settings, start sessions, or talk to anything off this computer.
- **Install / remove**: from Aide, which hands installing it to DSH (it
  backs up the profile first, then a DSH task installs it with DSH Desktop's
  plugin manager, restarts DSH Desktop and reports with
  `aided dsh-bridge report`); by hand, in DSH Desktop's Plugins page from the
  package Aide prepared. Turn it off or remove it there too.

Design: Aide's `docs/dsh-takeover.md`.
