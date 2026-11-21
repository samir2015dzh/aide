# aided on a machine

`aided` runs on each machine where coding agents work for your agents.
It runs in the background as the user it is installed for.
`aided <command> --help` says more about each command.

## Install

```bash
curl -fsSL https://github.com/samir2015dzh/aide/releases/latest/download/install.sh | sh -s -- relay.example.com
```

- It installs `aided` and `aide` in `~/.local/bin` and runs
  `aided login`: it asks for your account and password (or takes them
  from `$AIDE_ACCOUNT` and `$AIDE_PASSWORD`, the password handed to it on
  stdin), and your server adds the machine to your machines at once, with
  nothing to approve or compare ([accounts.md](accounts.md) §3).
- Aide no longer has Her. Installing over a version that had her removes
  `her`, `her-console/`, `her-pdf/` and `her-computer/` from there (not
  with `--cli`); Her's data in `~/.her` stays.
- As root, it says it installs for root: press Enter, or type another
  user to install for (creating it if needed). From then on, run the
  commands as that user: `su - dev`.
- Your server goes after `sh -s --` (`sh -s -- relay.example.com`), else
  `$AIDE_SERVER`; there is no default. `--name build-box` names the
  machine (default: its host name; taken already, the server gives it
  the next free one, `-2`, `-3`…).
- `aided login [server] --account A` does the same by hand
  (`--password-stdin` reads the password from stdin, `--no-service`
  skips the background service).
- On a first install, it shows the default data directory and available
  space. Press Enter to accept it, paste an absolute path (spaces and
  `~/` work without quoting), or type `q` to cancel. On Synology DSM,
  writable, mounted local storage volumes with at least 2 GiB free are
  also numbered choices; the one with the most free space is recommended
  unless the user's default data directory is already under `/volumeN`.
  Other Linux machines and macOS keep their ordinary platform default.
  A low-space location shows a warning and can still be accepted.
- Choosing another folder keeps a symbolic link at the default data
  directory, so later commands, the service and updates use that choice
  without extra arguments. Existing directories are kept on reinstall;
  this does not migrate an existing installation. If the link's target
  becomes unavailable, restore its disk before running Aide again.
- Without a terminal, it uses the recommended default and prints the
  location. `AIDE_DATA_DIR` overrides the default for that environment;
  `--cli` installs only the agent CLI and skips daemon data selection.
  Colors are disabled for redirected output, `TERM=dumb` or `NO_COLOR`.
- Then it installs the background service. On Linux it is a systemd user
  service with lingering on, so it starts at boot with nobody logged in.
  Without systemd (a container, WSL) run `aided run` yourself, as the
  container's command or with `nohup aided run >/dev/null 2>&1 &`.

## Updates

Aide updates itself: every few hours it looks for a new release and,
once no session is running and Her is not at work, installs it (her and
its console too, where they are) and restarts its service. It only
installs a release signed by the Aide release key, and never goes back
to an older version ([machine-updates.md](machine-updates.md)).

| Command | Does |
|---|---|
| `aided update` | Updates now and restarts the service; asks first when that would stop a session (`--yes` does not ask) |
| `aided update --check` | Says whether a newer version exists |
| `aided update auto off`, `auto on` | Turns automatic updates off or on |

`aide update` does the same on a computer with only the agent CLI. The
install command, run again, also updates a machine logged in.

### On Windows

In PowerShell, as yourself (no administrator):

```powershell
& ([scriptblock]::Create((irm https://github.com/samir2015dzh/aide/releases/latest/download/install.ps1))) relay.example.com
```

- It installs `aided`, `aidedw` and `aide` in
  `%LOCALAPPDATA%\Programs\Aide\bin`, adds that folder to your PATH and
  runs `aided login`, as on Linux (the account and password asked for,
  or from `$env:AIDE_ACCOUNT` and `$env:AIDE_PASSWORD`).
- With a name: the same, ending `<server> -Name build-box`;
  the agents' CLI only: the same with `-Cli`.
- The background service is a Task Scheduler task of yours that starts at
  logon. It runs `aidedw`, `aided` without a console window, which
  starts the daemon again whenever it stops unexpectedly.
- The programs are not code-signed yet ([windows.md](windows.md) §6.4):
  the installer checks them against the signed checksum list.
- Coding agents are looked for where their installers put them (Claude
  Code's own installer, WinGet, npm). Worktrees need Git for Windows.

## Every day

| Command | Does |
|---|---|
| `aided doctor` | Checks the login, the coding agents, the service and the relay, and says what to fix (exit 1 when something is wrong); and how Her is |
| `aided status` | The machine, whether it is connected, the service, updates, Her (installed, which version, on, running) and the projects |
| `aided her` | Talk to Her in this terminal: the Her this machine runs once Her is on for it ([linux.md](linux.md) §8) |
| `aided project add <folder>` | Lets agents work in a folder: a separate worktree per session, or `--in-place`; `--agents claude` limits it |
| `aided project list`, `project remove <name>` | The projects; removing one leaves its folder |
| `aided service status` | Whether the service runs and starts at boot, and where its log is |
| `aided service restart` | Restart it |
| `aided service install`, `service uninstall` | Start the service again; stop it for good (the login and projects stay) |

The log is `~/.local/share/aide/logs/aided.log` on Linux
(`tail -f` it), `~/Library/Application Support/Aide/logs/` on macOS,
`%LOCALAPPDATA%\Aide\logs\` on Windows.

**Coding agents**: install Claude Code or Codex as this user and sign in
once (run `claude` or `codex login`; on a server it prints a link to open
on any device). `aided doctor`, or Check Again in the app, looks again.

**Custom agents**: an ACP agent Aide has not built in is added from
the desktop app (Settings › Connect › Other Agent): its name, command,
arguments, environment variables and, if it needs one, the folder it
starts in. Test Connection starts it once on the machine and does the ACP
handshake (initialize, then a new session). Saved, it is listed with the
built-in agents and takes sessions at once. The machine keeps it in
`agents.json` in its data folder; a variable named like a key, token or
password (or marked secret) is kept apart, in `keys/agent-secrets.json`:
sealed with DPAPI for this Windows user on Windows; elsewhere readable by
this user only, as the machine's own key in the same folder is.

## Removing Aide

On Linux and macOS, as the user Aide was installed for (after a root
install for another user: `su - <user>` first):

```bash
aided uninstall
```

It lists what goes and asks first (`--yes` for scripts):

- the machine, logged out: your devices stop listing it (if the server
  can't be reached, it says so; remove the machine in the app then);
- the background service, stopped and removed;
- the data directory, and where it links to (a Synology volume's
  `@aide-<uid>`): sessions, logs, the worktrees Aide made;
- the agents' CLI key and settings (`~/.config/aide`);
- `aided`, and `aide` beside it (and what a version with Her left there:
  `her`, `her-console/`, `her-pdf/`, `her-computer/`).

Your project folders and repositories stay, and so does `~/.her`, Her's
assistants, conversations and settings: delete it yourself to remove
them. Worktrees with uncommitted changes move to
`~/aide-kept-worktrees`. Lingering, which the installer turned on,
stays: it says how to turn it off. It refuses on a computer that is one
of your devices until another of your devices signs it out (Settings ›
Account), and while `aided run` runs outside the service. After `--cli` only, there is no aided:
`rm -rf ~/.local/bin/aide ~/.config/aide`.

On Windows, in PowerShell:

```powershell
aided service uninstall
Remove-Item -Recurse "$env:LOCALAPPDATA\Programs\Aide", "$env:LOCALAPPDATA\Aide"
```

Then remove the machine in the Aide app, so it is no longer listed.
