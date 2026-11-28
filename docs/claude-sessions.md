# The Claude app's sessions in Aide

The owner sees in Aide the Claude Code sessions the Claude desktop app has
on each of their machines, the ones Aide did not start: listed by their
title in the app, with the GitHub repository their folder is a checkout of
when the checkout says so, and opened read only — or gone on with in Aide (§5). Work order: the handover
repository's "Claude Desktop / Claude Code 既有会话在 Aide 内的可见性"
(inventory: `reports/2026-11-20-forensics-visibility-loginstate.md` §3,
grade A; the owner's ruling on what is shown, 2026-11-20).

## 1. Where they are read from

- **The list**: the app's own index, one record a session,
  `<index>/<account>/<workspace>/local_<id>.json`: `title`, `cwd` (else
  `originCwd`, else the first `gitAnchors[].gitRoot`), `createdAt` and
  `lastActivityAt` (Unix ms), `isArchived`, `model`,
  `postTurnSummary.status_category`. The index folders
  (`claude-code-sessions`) are found as the app's own Claude Code is found
  (`internal/provider/claude`: `desktopIndexes`): on macOS under
  `~/Library/Application Support/Claude` (and `Claude-3p`); on Windows in
  any folder whose name begins with Claude in `%APPDATA%`, `%LOCALAPPDATA%`
  and an MSIX package's `LocalCache\{Roaming,Local}`, at most two folders
  down (such as `Claude-Data\claude_<package id>`): by that rule, never by
  names seen on one computer.
  - A session archived in `archived-sessions.idx` beside the records is
    archived; one with a `deleted_<id>` beside it is not listed; one listed
    under two accounts is listed once, as its newest record says.
  - Those Aide started (it lists them in the app too) are left out: by
    Claude Code's id (the session's `provider_ref`), and by folder (one of
    Aide's worktrees).
- **The conversation**: Claude Code's transcript,
  `<config>/projects/<folder>/<cliSessionId>.jsonl`, config being
  `$CLAUDE_CONFIG_DIR` or `~/.claude`; the folder's name is not worked
  out, every project folder is looked in. Plain JSON lines: `user` and
  `assistant` lines give the owner's prompts (not what Claude Code adds:
  `isMeta` lines, command output, reminders), Claude's text, tool calls and
  results (cut at 16 KiB); a subagent's lines (`isSidechain`) are left out.
  A session with no transcript on that machine is listed with an error
  saying so.
- **The repository**: "owner/repo" from the `origin` remote of the git
  checkout the session's folder is in (its `.git` folder, or a worktree's
  `.git` file and its `commondir`), when that url is on github.com; empty
  otherwise (no checkout, no origin, another host). Never guessed from the
  conversation; the app itself keeps no repository of its own
  (`git-worktrees.json` `originUrls.url` is empty on the owner's computer).

- **Its address**: `c_…/<conversation key>`, the conversation under this
  computer's CLI agent, once that agent's SessionStart hook reported it
  (docs/agent-messages.md §15.4); empty before. `ClaudeSession.address`.

Nothing in the app's or Claude Code's folders is written.

## 2. The API

- `MachineAdminService.ListClaudeSessions` / `ReadClaudeSession` (owner
  only): the sessions, and the index folders read; one session and a page
  of its conversation (as `ReadDSHSession` pages).
- `LocalService.ListClaudeSessions` gathers them over the machines; a
  machine that can't be reached keeps its sessions as last read, `lost`
  (as DSH's: docs/dsh-takeover.md §6). `ReadClaudeSession` reads one.

## 3. The desktop app

"Claude 会话" in the sidebar (where Claude Code is on a machine), and a
link from Claude Code's Agent page: the list by last activity (title, the
app's word after the last turn, machine, repository or folder; archived on
request), and a session's conversation, read only, with where a message to
it goes ("可寄信：c_…/…") or that it takes none ("不可寄信"). With none, the page
says which index folders were read, or that no app data was found.

Android: not yet (the work order asked for the desktop app).

## 4. Aide's own sessions in the app (工单 #131)

A Claude Code session Aide runs is Claude Code's own: its transcript is in
`<config>/projects/<folder>/<id>.jsonl`, where the app's Claude Code keeps
its own, and nothing of Aide's stands in for it. The app lists only those
it has a record of, so:

- As a session starts, Aide writes its record in the app's index
  (`internal/provider/claude/desktop.go`). The app reads its index only as
  it starts, and its Microsoft Store (MSIX) build — the one on the owner's
  PC — is said to pass over records written from outside, so this alone is
  not counted on.
- **Open in Claude**, in a session's settings (on the computer that runs
  it): the app's own import link, `claude://resume?session=<Claude Code's
  session id>`. The app writes the record itself (`local_<id>`, the key it
  de-duplicates on) and opens the session; it is in its list from then on,
  and the owner goes on with it there. Only once Aide is done with the turn:
  the app runs a process of its own on the session, and one still running
  would be run twice (anthropics/claude-code#111489). The session's id is
  not in what lists show (`machine.BasicSession`): the page reads the full
  session when asked. The app's main process opens no other `claude://`
  link (`shared/claudeDesktop.ts`).

## 5. Going on with one in Aide (工单 #181)

- `MachineAdminService.ContinueClaudeSession` (owner only; the owner's
  `LocalService.ContinueClaudeSession` on any of their machines) starts a
  session of Aide's that resumes one of the app's sessions: `claude --resume`
  with Claude Code's own id, in the session's own folder (worked in place:
  never a worktree of Aide's, never removed), the owner's message its first
  turn. Claude Code resumes into the same session id (it makes a new one
  only with `--fork-session`), so what follows is in the transcript the app
  shows: the two read the same.
- The project it is kept under: the one named, else the one whose folder
  holds the session's folder (the nearest), else one whose repository the
  folder is a worktree of (git's common dir). None: refused with
  `claude_no_project` and the folder; the page offers to add that folder as
  a project and go on. A folder gone, or no transcript of it here: refused
  with `claude_folder_gone` / `claude_no_transcript`, saying to go on in
  Claude or start a new task in Aide.
- The session it goes on in is kept (`claude-continued/<Claude Code's id>`):
  going on again opens it, and the app's session stays listed, saying so
  (`ClaudeSession.continued_in`) — its earlier conversation is only there.
- Going on with it in Claude at the same time runs it twice: the page says
  not to.
