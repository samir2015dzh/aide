# Aide: the rename, and what an installation from before it keeps

The product, its code and its repository are called Aide (owner's ruling of
2026-11-20: the old name is gone from the code). The repository follows no
upstream: it started as a fork and is its own now. The item-by-item mapping
of old names to new ones is in the handover's delivery record of
2026-11-20 and in PR #1.

Nothing set up before the rename has to be set up again, with one
exception (the desktop app, §3): accounts, devices, machines, agents'
grants, Her's data and each machine's sessions keep working. What they
depend on is read under the old name where it was stored, signed or
registered under it. The old name is in three places only:

- `internal/legacy/legacy.go`: for the Go programs (aided, aide, aide-relay,
  the phone's core), each value with why it stays;
- `apps/desktop/src/main/legacy.ts`: for the desktop app;
- `scripts/release.sh` (`LEGACY=`): the release's copies of the programs
  under their old names.

## 1. What keeps working, and how

| What | How an installation from before keeps working |
|---|---|
| Everything signed (root certs, machine and device certs, grants, cards, revocations, erase orders, admin commands, inbox messages) | A message is signed under its type's full name, which keeps the proto packages' old name (`legacy.SignedType`): what was signed before verifies, and what is signed now verifies on either side |
| Sealed and hashed data (notices, inbox messages, the audit log, the MCP gateway's keys and secrets, the old password login, custom agents' secrets on Windows) | Their labels keep their bytes (`legacy.*`) |
| The programs, which services, logon tasks and agents' hooks run by path | Releases carry the programs under both names (the same files): an updater from before downloads the old names. The updater now writes `aided`, `aidedw`, `aide` and keeps an old-named program that is there as a link to the new one (a copy on Windows) |
| The data directory and the CLI's home | The old one is used where it is while it holds an identity (the owner's device, the machine, the CLI's key) and the new one holds none, or while no new one exists: worktrees, sessions and services point into it by absolute path (`paths.kept`, `legacy.ts kept`). A new directory that only something else made (the service's task definition, the CLI's home inside it) does not count. The machine's database is renamed to its new name at its first start |
| The background service (systemd unit, LaunchAgent, logon task) | One installed before keeps its name: installing the service again updates that one, now running `aided`, rather than adding a second; status, restart and uninstall find it |
| Environment variables | `AIDE_X`, or the old prefix's when that is unset (`brandenv`) |
| Sessions' git branches | A session started before goes on on its branch while only that one exists (`aide/<session>` otherwise) |
| The DSH plugin | Its old rendezvous is used while there is no new one, until `aided dsh-bridge prepare` and DSH Desktop install the new plugin (remove the old one there); messages it sent still count as sent from Aide |
| RPC paths (`/aide.*.v1.*/…`) | Not kept: **the server, the machines and the apps update together** (Muse's ruling of 2026-11-20 on PR #1: one owner, upgraded in one go) |
| MCP server and tool names (`aide`, `mcp__aide__*`, `aide_status`) | Agents see the new names at their next start; a connector added in Claude or ChatGPT keeps working (its address and tokens are unchanged) |
| Wake links (`aide://`) | A wake link lasts 30 minutes: none outlives the update |
| Her's browser host (`com.aide.browser`) | Turning Her's Chrome on again (Her's settings) installs the new host |
| Metrics (`aide_*`) | Dashboards that scrape them need the new names |

## 2. Updating

1. Update the server (`aide-relay`; its image is `ghcr.io/samir2015dzh/aide-relay`;
   the data volume is unchanged).
2. Machines update themselves, or run `aided update` (by its old name on
   an installation from before: the same program).
3. Agents' CLIs update with their machine, or reinstall with
   `install.sh --cli`.
4. The desktop app: §3. The phone: the new Android app (`io.aide.android`),
   logged in with the account and password; uninstall the old one.

Between steps 1 and 2 a machine of the old version cannot reach the new
server (the RPC paths changed): update them together.

## 3. The desktop app

Its id and its Windows package changed (`com.aide.app`, `AideApp`), so the
app from before does not update itself to this one: install
`AideSetup.exe`, then uninstall the old app from Installed apps. The new app
finds the old data directory (§1) and is still logged in; its logon task
keeps its name and now runs the new `aidedw`.
