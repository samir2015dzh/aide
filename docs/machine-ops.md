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
  may go. Where it starts is limited by the machine's workspace roots,
  when its owner set some (§5).
- Sessions don't need it, and having it changes nothing about sessions.
  Her's grant never has it.

## 2. The call

`MachineOpsService.Exec` on the machine (machine.v1), over the agent's
end-to-end tunnel; the relay only carries it, as it carries sessions.

| Field | |
|---|---|
| `command` | Run by `powershell.exe -NoProfile -NonInteractive -EncodedCommand` on Windows (its output set to UTF-8 first), by `/bin/sh -c` elsewhere, with a new terminal's environment (`shellenv`, as coding agents get) and no stdin |
| `workdir` | `~/…` or a full path; empty: the home folder. A missing folder fails the call; one outside the workspace roots (§5) is refused |
| `timeout_seconds` | Default 60, at most 600. At the end the command and everything it started (its process group; on Windows its job) is ended |

The answer has `exit_code` (-1 when ended), `stdout` and `stderr` (each at
most 1 MiB, as UTF-8; `*_truncated` says what was cut), `timed_out`,
`started_at`, `ended_at`, `shell` and the full `workdir`. On Windows,
PowerShell's own rules give the exit code: `exit N` sets it, and a failing
last command gives 1. On Windows anything the command started in the
background ends with it.

The machine refuses (`PermissionDenied`) a caller whose grant lacks
`machine_exec`, and the owner's devices; it refuses while the owner has paused
it (`Unavailable`), a timeout above 600 s (`InvalidArgument`), and a
`workdir` outside the workspace roots (`PermissionDenied` with an
`ErrorInfo` detail, reason `OUTSIDE_WORKSPACE_ROOTS`, §5).

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
  when refused, 3 (`outside_roots`) when `--workdir` is outside the
  machine's workspace roots (§5). With `-o json`, the `exec` object ([cli-output.md](cli-output.md) §5).
- MCP: the tool `machine_exec` ([mcp-gateway.md](mcp-gateway.md) §7), the same
  command with the same document. The gateway keeps neither the command nor
  its output.

## 5. Workspace roots

> Status: implemented (2026-11-20, borrowed item 15). The machine's fence on
> which folders its owner's devices reach and where agents' commands start.

A machine's **workspace roots** are folders. With roots set, the machine
allows only folders inside one of them for:

| | What is fenced |
|---|---|
| `MachineAdminService.ListFolders` (Add a Project → Browse Other Folders…) | The folder listed. An empty path is the home folder, or the first root when the home folder is outside them; links that lead out are not listed |
| `MachineAdminService.ListRepos` (the repositories Add a Project offers) | Only those inside |
| `AdminCommand.add_project` (the app, the phone, `aided project add`) | The project's folder, as it is checked (its real path) |
| `MachineOpsService.Exec` (`aide machine exec --workdir`, MCP `machine_exec`) | The folder the command starts in. A refusal is in the ops log as `refused` |

What "inside" means: the folder's **real path** is the root or under it,
the roots being kept as real paths too. `..` is resolved before it is
checked, and symbolic links are followed; on Windows the path is the one
Windows opens (`GetFinalPathNameByHandle`), so junctions, short names
(`RUNNER~1`) and another case of the names or the drive letter don't lead
out. Each refusal is `PermissionDenied`, its message naming the folder and
the roots, with an `ErrorInfo` detail of reason `OUTSIDE_WORKSPACE_ROOTS`;
the CLI makes it the kind `outside_roots` (exit 3) with a hint
([cli-output.md](cli-output.md) §2).

The roots fence where things **start**, not what a command does once it
runs: a command started inside a root runs as aided's user and may still
`cd` anywhere that user may go (§1). Nor do they fence the projects of
GitHub repositories (`session start --repo`): those are clones in the
machine's own data folder, which nobody picks.

### 5.1 None set: as before

A machine whose owner set no roots allows any folder, exactly as before
roots existed. That is the default, and machines updated to this version
keep it: nothing has to be configured again, and no folder, project or
command that worked stops working. A default fence (the home folder, say)
would have broken what already worked: projects and `--workdir`s outside
the home folder (`/srv/app`, `D:\code`), and the folder browser's way up
to `/`.

### 5.2 Setting them

On the machine, as its own user (as `aided project`, through the running
service's control socket, or directly when it isn't running):

```
aided roots                       # the roots, and the projects outside them
aided roots set ~/code /srv/app   # replaces the roots set before
aided roots clear                 # none: any folder again
```

Each root must be a folder (`~/…` or a full path); it is kept as its real
path. From afar the command is `AdminCommand.set_workspace_roots`
(identity.v1), which the machine takes **root-signed** only: unlike the
settings (projects, pausing, the name, agent defaults), it is not taken
unsigned from a trusted device, since it fences what those settings reach.

### 5.3 Projects from before

A project added before the roots and outside them **stays usable**:
sessions start in it as before. Setting the roots says which ones are
outside (`projects outside them stay usable: old (/srv/old)`), and so does
`aided roots`, each with how to stop using it (`aided project remove
<name>`). Removing it is the owner's choice; the roots never remove or
disable a project on their own.

