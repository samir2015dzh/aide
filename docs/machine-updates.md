# Machines that update themselves

Status: accepted 2026-10-30 with the owner's decisions (§9), and built. The commands are in [aided.md](aided.md), the release asset in [development.md](development.md).

## 1. Goal

A machine installed with `install.sh` (Linux, or a Mac without the app) stays on the latest release without anyone logging in to it, and its owner can update it on demand with one command. Before, the only way was to run the install command again, which on a paired machine stopped the service and started a new pairing, then `aided service restart`.

**Not in scope:** the Mac app, which updates itself with the `aided` it bundles ([desktop-app.md](desktop-app.md) §6.5), and so does the Windows app ([windows.md](windows.md) §6.2); apt/yum packages (§2); updating a machine from the iPhone (§8). A Windows machine that `install.ps1` set up updates as §5.5 says.

## 2. What others do

| Way | Who | Fits |
|---|---|---|
| A distribution repository (apt, yum), updated by `apt upgrade` or unattended-upgrades | Docker, Grafana, Teleport, Tailscale | Programs installed system-wide by root |
| An update command | `tailscale update`, `cloudflared update`, `claude update`, `deno upgrade`, `bun upgrade`, `rustup self update` | Programs installed by a script into a folder their user can write; each refuses when a package manager installed it |
| Updating in the background | Claude Code (on by default), cloudflared (every 24 hours by default; off in containers and for packages), Syncthing release builds (on by default; off in distribution packages), Tailscale (`tailscale set --auto-update`, off until turned on) | Unattended machines, which otherwise stay on the version they were installed with |
| A notice only | `gh` ("A new release of gh is available") | Tools that leave the decision to their user |

The mechanics are the same everywhere: look for a newer version, download it, verify its signature, write it next to the old one and `rename` it over (Unix lets a running program be replaced), restart through the service manager.

Aide is installed per user into `~/.local/bin` and runs as a systemd user service, so it is the second and third row. A package would put it in `/usr/bin` under root, against that model, and add a repository signing key and one repository per distribution family to maintain. Not now.

## 3. Principles

1. **Never under a running session.** Restarting `aided` marks a turn in progress crashed and loses pending questions ([technical-design.md](technical-design.md), restart recovery). An update waits while a session on the machine is queued, preparing, running or waiting for an answer, the guard [windows.md](windows.md) §6.2 uses. Her stops with `aided`, so her assistants at work count too: a turn (an approval waiting is in one), sub-agents, a routine's run, as aided last saw them in her's `bots/changed`.
2. **Only signed releases, never backwards.** What is installed is checked against the release's signed checksum list with the public key built into the program, as `install.sh` does. The version is part of what is signed (§5.1), and an update only goes to a higher one.
3. **One way, two doors.** `aided update`, `aide update` and the background updater are the same code.
4. **The release address is a build-time constant.** Release builds read GitHub Releases; only test builds read another, as for the app's feed (desktop-app.md §6.5).

## 4. What the owner sees

```
$ aided update
Aide 0.1.1-beta.5 → 0.1.2
✓ verified aided and aide
✓ installed in ~/.local/bin
✓ the service restarted on 0.1.2

$ aided update            # while a session runs
Aide 0.1.1-beta.5 → 0.1.2
1 session is running here; restarting the service ends its turn.
Update now? [y/N]

$ aided update --check
Aide 0.1.2 is available (this computer has 0.1.1-beta.5).

$ aided update auto off
Automatic updates are off. `aided update` still updates.
```

| Command | Does |
|---|---|
| `aided update` | Updates `aided` and `aide` in the folder they are in, removes what a version with Her left there, and restarts the service; asks first when a session would be stopped (`--yes` does not ask) |
| `aided update --check` | Says whether a newer version exists; exit 0 either way |
| `aided update auto on`, `auto off`, `auto` | Turns automatic updates on or off, or says which |
| `aide update` | The same, for a computer with only the agent CLI |

- `aided status` adds one line: `updates      automatic · checked 2 hours ago`, or `updates      off · 0.1.2 is available`.
- On a machine logged in, the install command updates and restarts the service; it does not log in again.
- The iPhone's "older than this app" section keeps the install command: it now updates every machine, the ones from before `aided update` too, since the new program does the login step.

## 5. How it works

### 5.1 The release

`scripts/release.sh` adds one small asset, `version` (the version without `v`, one line), to the files the signed `checksums.txt` lists. Nothing else changes; `sha256sum -c checksums.txt` still works. The list also has her for Linux, `her-linux-<arch>`, and its console, `her-console.tar.gz` ([her-server.md](her-server.md) §6.4).

Looking for an update reads three small files from `https://github.com/samir2015dzh/aide/releases/latest/download/`: `checksums.txt`, `checksums.txt.sig` and `version`. The signature must verify with the built-in key, and `version` must match its line in the list. A newer version's programs then come from `…/releases/download/v<version>/`, each checked against the list.

### 5.2 Installing

1. Find the folder: the running program's own, links resolved. Refuse inside an app bundle ("Aide.app updates itself") and when the folder cannot be written ("installed by another user; run the install command as them").
2. Take a lock in `update/` in the data directory, so a manual and a background update never overlap.
3. For each of `aided` and `aide` present in the folder: download `<name>-<os>-<arch>` next to it as `.<name>.new`, check its hash, make it executable, and run it with `version`: it must start and say the expected version. (Until 2026-11-22 `her` and `her-console/` were updated here too; Aide no longer has Her, and an update now removes them, their `.previous` and her other folders, `her-computer/` and `her-pdf/`, once the programs are in place.)
4. Keep the current program as `.<name>.previous`, then `rename` the new one over it.
5. Restart: `aided update` restarts the service (`service.Restart`) and waits for the control socket to report the new version; the background updater exits instead (§5.3). Her, when on, starts again with the new aided, on the new her.

### 5.3 In the background

- Runs inside `aided run`, on a machine, only when the process is the installed service for its data directory (systemd's `INVOCATION_ID`, launchd's `XPC_SERVICE_NAME`, `service.Supervised`): the service manager is what starts the new version. In a container or under `nohup` it never runs; `aided update --check` still works there.
- Checks 5 to 15 minutes after start, then every 6 hours with up to an hour's jitter, so a release does not reach every machine at once.
- With a newer version found, it waits for the guard (§3.1), looking again every 10 minutes, then installs (§5.2) and exits; systemd (`Restart=always`) and launchd (`KeepAlive`) start the new program.
- launchd keeps a job as it loaded it, and an update replaces only the program. So on a Mac whose service an older version installed as a Background job, the new program corrects the plist when it starts and has launchd load the job again (`service.ReloadOldJob`).
- A failed check or download is logged and tried again at the next check. Nothing is ever shown on a device for it.

### 5.4 State

`update/update.json` in the data directory, written by both the command and the daemon (no database, since the daemon holds the store): whether automatic updates are on (default on), the last check and its result, and the last update (from, to, when). The log says each check that finds something and each update.

### 5.5 On Windows

- **Three programs**, with `.exe`: `aided`, `aidedw` (the windowless one the logon task runs) and `aide`, from `<name>-windows-<arch>.exe`. Each waits beside its program as `.<name>.new.exe`, since Windows runs `.exe` files only.
- **Replaced by renaming.** A running program can't be replaced, only renamed: the old one becomes `.<name>.previous.exe`. The supervisor runs the `aidedw.exe` of its logon, which stays in use until the next logon, so a later update moves the one before it beside it with a number (`.aidedw.previous.exe.<n>`), and removes the others.
- **Restarted by the supervisor.** The logon task's supervisor (`aidedw run --supervise`, windows.md §4.4) starts the daemon with `AIDE_SUPERVISED=1`, which is how the daemon knows it is the installed service. After an update the daemon stops cleanly, and the supervisor, seeing its program's file changed (its time or size), starts the new one instead of stopping too.
- **Not the app's.** The Windows app copies its programs into the same folder; where it is installed (`%LOCALAPPDATA%\AideApp\Update.exe`), aided leaves them to it: `aided update` says so, and the background updater doesn't run.

## 6. Security

- A compromised download server or network can make a machine install only something signed by the release key, and only a version higher than its own; replaying an older signed release does nothing.
- The program that downloads is the one the owner already trusts, running as the same user; no new privilege, no listening port.
- Turning automatic updates off is a local command only, like projects (`ApplyLocal`): whoever has a shell as this user can replace the program anyway.

## 7. Tests

- **Unit** (`internal/update`): signature, version and hash checks, refusing a lower version and an app bundle, the rename and `.previous`, the schedule and the guard with a fake clock.
- **TestWindowsUpdate** (end to end, on `windows-latest`): `install.ps1` installs A from the test's releases and logs in; B published, the machine updates itself and its supervisor starts B; with automatic updates off, `aided update --yes` installs C.
- **TestLinuxUpdate** (`AIDE_DOCKER=1`, end to end): a systemd Ubuntu container logged in as root, a relay on the Mac, and three signed releases A < B < C served by the test over HTTP (test builds name its address and a test key).
  1. The install command installs A and logs the machine in.
  2. B is published while a session runs: the machine stays on A. The session ends: the machine restarts itself on B, and the owner's device sees B in the machine's status, with the same machine ID.
  3. The install command runs again (B): no new login, the service restarts, still connected.
  4. `aided update auto off`, C is published: no update; `aided update --check` names C; `aided update --yes` installs it and the device sees C.
- **TestLinuxHer** (`AIDE_DOCKER=1`, end to end): the same kind of server and releases, carrying her and its console. The install command installs them and logs in; Her, turned on, answers; B, published while Her is at work, waits for her, then replaces aided, her and the console, and Her answers again; `aided uninstall` removes her and leaves `~/.her`.

## 8. Later

- **Update from a device.** An "Update Now" button on the iPhone's machine page, sent end to end like other machine commands.
- **Rollback.** A release that fails to start leaves the machine off until someone logs in and moves `.aided.previous` back. The `version` check (§5.2) catches a broken download, not a broken release. If that is needed, the service would start the previous program after repeated failures.

## 9. The owner's decisions

1. Automatic updates are on by default, and wait for the guard (§3.1).
2. A check every 6 hours, first one 5 to 15 minutes after start (§5.3).
3. They are turned off only on the machine itself (`aided update auto off`), not from a device.
4. `aided update` asks before stopping a session; `--yes` skips the question.
5. The install command on a machine logged in updates without logging in again.
6. Linux trusted devices updated themselves too (there are none since 2026-11-20, [accounts.md](accounts.md)).
7. Not now: apt/yum packages, updating from a device, rollback (§8).
