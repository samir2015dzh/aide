# Machine ops: one-shot commands

> Status: implemented (2026-11-16, WO #6). For operations an agent does on
> the owner's machines without a coding agent: pulling code, checking a
> status or a version, running a deploy script.

In one sentence: **an agent the owner allowed runs one command on a machine,
end to end through the relay, and gets its output and exit code; the machine
keeps a log of every call, refused ones too, which the owner sees.**

## 1. The scope

- `Grant.machine_exec` (identity.v1) is a scope of its own: **off** for every
  grant until the owner turns on **Run commands** for that agent in the app
  (Settings → Access → the agent; on the iPhone, Grants → the agent). It is
  signed into the grant like the rest of its scope (`UpdateClient`; the
  server signs the new grant version for the device, [accounts.md](accounts.md) §1);
  turning it off is the same, and takes effect as soon as the
  machines see the new grant version.
- It covers the machines the grant covers. Projects and coding agents don't
  limit it: a command runs as the user aided runs as, anywhere that user
  may go.
- Sessions don't need it, and having it changes nothing about sessions.
  Her's grant never has it.

## 2. The call

`MachineOpsService.Exec` on the machine (machine.v1), over the agent's
end-to-end tunnel; the relay only carries it, as it carries sessions.

| Field | |
|---|---|
| `command` | Run by `powershell.exe -NoProfile -NonInteractive -EncodedCommand` on Windows (its output set to UTF-8 first), by `/bin/sh -c` elsewhere, with a new terminal's environment (`shellenv`, as coding agents get) and no stdin |
| `workdir` | `~/…` or a full path; empty: the home folder. A missing folder fails the call |
| `timeout_seconds` | Default 60, at most 600. At the end the command and everything it started (its process group; on Windows its job) is ended |

The answer has `exit_code` (-1 when ended), `stdout` and `stderr` (each at
most 1 MiB, as UTF-8; `*_truncated` says what was cut), `timed_out`,
`started_at`, `ended_at`, `shell` and the full `workdir`. On Windows,
PowerShell's own rules give the exit code: `exit N` sets it, and a failing
last command gives 1. On Windows anything the command started in the
background ends with it.

The machine refuses (`PermissionDenied`) a caller whose grant lacks
`machine_exec`, and the owner's devices; it refuses while the owner has paused
it (`Unavailable`), and a timeout above 600 s (`InvalidArgument`).

## 3. The ops log

Each call, refused ones too, goes to the machine's ops log (its store, the
last 500, newest first) and its audit log: who (client id and the grant's
name), the command, the folder, when it started and ended, how it ended
(`exited` with its exit code, `timed_out`, `refused` or `failed`, with the
reason). The output is kept nowhere. The owner's devices read it with
`MachineAdminService.ListExecLog`; the app gathers every reachable machine's
(`LocalService.ListExecLog`) on its **Ops Log** settings page.

## 4. For agents

- CLI: `aide machine exec <machine> [--workdir DIR] [--timeout 60s] -- <command…>`
  (or `--command "…"`). Exit code 0 when the command exited 0, 5
  (`command_failed`) otherwise, 6 (`timeout`) when it was ended, 3 (`denied`)
  when refused. With `-o json`, the `exec` object ([cli-output.md](cli-output.md) §5).
- MCP: the tool `machine_exec` ([mcp-gateway.md](mcp-gateway.md) §7), the same
  command with the same document. The gateway keeps neither the command nor
  its output.
