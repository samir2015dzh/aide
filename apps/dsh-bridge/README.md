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
- **What it does not do**: read or write DSH's session files, change
  settings, start sessions, or talk to anything off this computer.
- **Install / remove**: from Aide (it backs up the profile first), or
  `dsh plugin --profile desktop add <path to the package>` and
  `dsh plugin --profile desktop remove @aide/dsh-bridge`; or turn it off
  in DSH's Plugins page. Restart DSH after installing.

Design: Aide's `docs/dsh-takeover.md`.
