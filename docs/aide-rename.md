# Aide, formerly Hermesh: what changed name and what didn't

The product is called Aide since its first release under that name (after v0.2.3). Everything a person reads says
Aide; everything that is persisted, signed or registered on a machine
kept its name, so that nothing set up before the rename had to be set up
again: identities, logins, agents' grants, Her's data, each machine's
configuration. The full mapping, item by item, with the reasons, is the
handover's RENAME-AIDE.md.

## Two names that both work

| Now | Before, still works | How |
|---|---|---|
| `aide` | `hermesh` | the same program: a link (Linux, `install.sh`, updates) or a copy (Windows) |
| `aided` | `hermeshd` | the same; the background service and agents' hooks run the old name |
| `aide-relay` | `hermesh-relay` | in the relay image, a link; releases carry both |
| `AIDE_X` | `HERMESH_X` | every variable a person or a container sets (`internal/brandenv`); the new one wins when both are set |

Updates keep both names: a machine installed before the rename gets
`aide` and `aided` beside its programs with its first update.

## What kept the old name, and why

- **Data**: `%LOCALAPPDATA%\Hermesh`, `~/.local/share/hermesh`,
  `~/.config/hermesh`, Her's `~/.her`. Worktrees, agents' session
  histories and services point into them by absolute path.
- **Windows registrations**: `Hermesh.exe` under `%LOCALAPPDATA%\HermeshApp`
  (Squirrel's package id), the app id `com.hermesh.app`, the logon task
  `Hermesh (<user>)`, `Programs\Hermesh\bin`, the `hermesh:` links (and
  the signer in `Program Files\Hermesh Signer`, until accounts removed it on
  2026-11-20). What Windows
  shows of them (Task Manager, Installed apps, the Start menu's
  `Aide.lnk`) says Aide.
- **Protocol and keys**: the proto packages (`hermesh.*.v1`, the signing
  domain), every `hermesh-…-v1` label signed or derived from, `hermesh://`
  links, the WebSocket subprotocol, the MCP server's name (`hermesh`, its
  tools `mcp__hermesh__*`), the release signing key. Old and new
  programs keep talking to each other.
- **Code**: the Go module `github.com/hermeshai/hermesh`, the npm scope
  `@hermesh`, internal identifiers.
- **Releases**: still at `github.com/samir2015dzh/hermesh/releases`, where
  every installed version looks.
- **Release notes** of the versions before the rename read as they were
  written.

## Android: a new app (owner's instruction of 2026-11-19)

The Android app's id changed with the rename after all: `io.aide.android`
(the debug build `io.aide.android.debug`), and with it the Kotlin package
and the core's Java package. A phone gets it as a new app beside the old
`io.hermesh.android`, which the owner uninstalls by hand: the new app
starts without the old one's key and logs in again.
