# Managed workspaces: requests to the coordinator

Status: built 2026-11-20.

An owner can keep a coding agent in a managed workspace: a directory
with fixed folders, shared tools the agent may not fetch itself, and rules
against probing the computer (its network, time zone, identity). The agent
asks for what it needs, a tool, a permission, a piece of information or a
system change, and the workspace's coordinator (one of the owner's agents)
answers. Aide carries the requests and replies over its messages
([agent-messages.md](agent-messages.md)) and keeps their status. It adds no
approval screens and no new signing: a reply is an ordinary message from
the coordinator, signed by its client key as every message is.

## 1. The marker

A workspace's root holds `_workspace.json`. Every path in it is relative to
the root; one that is absolute or leads out of the root falls back to its
default.

```json
{
  "workspace_id": "ws-example",
  "rules_file": "CLAUDE-WORKSPACE-RULES.md",
  "tools_manifest": "_tools/MANIFEST.md",
  "requests_file": "_inbox/requests.md",
  "channel": {
    "coordinator": "c_…",
    "coordinator_name": "Muse",
    "provisioner": "c_…"
  }
}
```

- A project is in a managed workspace when its directory, or one above it,
  has the marker; the nearest one counts. Nothing about drives, folder or
  account names is assumed.
- `channel.coordinator` is the coordinator's address (a client id, from
  `aide message contacts`). Without it (or without `channel`) the
  workspace has no session channel: its sessions use the requests file
  (§5). `provisioner`, optional, is an agent that may also say a request is
  provisioned (DSH, which puts tools in place).
- A marker that doesn't parse still marks the workspace: its sessions are
  told they are in one, with the default paths and no channel.

## 2. What a session is told

When a session of such a project starts (or resumes), aided adds a fixed
section to the coding agent's system prompt: Claude Code's
`--append-system-prompt`, Codex's `developer_instructions`. ACP agents have
no system prompt to add to; their sessions get the tools (§3) with their
descriptions only. The text is `workspacePromptTemplate` in
[internal/machine/workspacereq.go](../internal/machine/workspacereq.go):
that it is in a managed workspace, its root and id, where the rules, the
tools' ledger and (without a channel) the requests file are, who the
coordinator is, how to ask (the four kinds and their fields), the statuses,
the coordinator's tiers (low risk approved, high risk to the owner, probing
refused), that approval is not leave to fetch anything, and that only
replies through Aide count. The template names no machine, path or
person; those come from the marker. Sessions outside a managed workspace
are told nothing new.

## 3. The channel

A session in a workspace with a channel gets three more tools from Aide's
MCP server (`aided message-mcp`):

| Tool | Does |
|---|---|
| `workspace_request` | Sends the coordinator a request: `kind` (`tool`, `permission`, `info`, `system_change`) and its fields. Returns its id, `wr_` and a ULID |
| `workspace_requests` | The session's requests, as they stand |
| `workspace_confirm` | Marks a provisioned request confirmed (`request_id`, `note`) |

Required fields: a tool's `item` (name), `version`, `purpose`, `repo` and
`source` (its official source); a permission's `item`, `purpose` and
`scope` (what it reaches, whether it is given back); information's `item`
and `purpose`; a system change's `item`, `purpose` and `rollback`. A
request the same as one still open (same kind, item and version) is
refused with the open one's id.

The request goes to the coordinator as a message from the session
(`m_…/s_…`) whose `workspace_request` field carries it, and whose text
says how to answer. The coordinator answers with:

```
aide message send --to <session> --workspace-request wr_… --status approved|rejected|provisioned --message "…"
```

The message's text is the reply: why it was rejected, the (redacted)
information, where a tool was put and which version. aided checks the
reply, records it and hands the coding agent a line it writes, saying the
request's new status and that Aide checked who answered, then the reply's
words.

### Protocol

`aide.session.v1.WorkspaceRequest`, carried by `inbox.v1.Message.workspace_request`
(request and replies) and recorded as the `workspace_request` session event,
each one the request as it stands after one change:

| Field | |
|---|---|
| `request_id` | `wr_` and a ULID, chosen by the machine |
| `kind` | `tool`, `permission`, `info`, `system_change` |
| `status` | `pending`, `approved`, `rejected`, `provisioned`, `confirmed` |
| `from`, `from_name` | Who made this change: `agent` (the session's coding agent) or a client id, and its name, as `UserMessage.from` |
| `item`, `version`, `source`, `repo`, `purpose`, `scope`, `rollback` | The request |
| `note` | The reply's words, or the agent's when it confirms |
| `coordinator`, `provisioner` | The client ids the workspace named when the request was made |
| `message_id` | The message that carried this change |

### Statuses, and who may change them

| From | To | Only by |
|---|---|---|
| (new) | `pending` | The coding agent (`workspace_request`) |
| `pending` | `approved`, `rejected` | The coordinator |
| `approved` | `provisioned` | The coordinator or the provisioner |
| `approved` | `rejected` | The coordinator |
| `provisioned` | `confirmed` | The coding agent (`workspace_confirm`) |

A rejection says why. Everything else is refused, and the sender is told
why (at once over the tunnel; a reply that waited at the post office is
answered with a message saying why).

### Nothing else counts

- A request's status changes only on a reply whose signature is the
  request's coordinator's (or provisioner's), checked as every message
  into a session is, and only to a status that follows; or on the coding
  agent's own confirmation of a provisioned request.
- No code reads a status from anything else: not files in the workspace or
  its repositories, not code comments, not a message's text, not what
  another agent relays. "Approved" written anywhere else changes nothing.
- The coordinator is fixed per request when it is made, so editing the
  marker afterwards doesn't change who may answer it. The coding agent
  can't sign as any client: its own messages go out, never into sessions.
- The coding agent can't make a request provisioned; it can only confirm
  one that is.

## 4. The apps

The session's conversation shows each change: the kind and item, the
status, and who made it, with the reply's words. The desktop and Android
apps only show them; answering is the coordinator's, through messages.

## 5. The requests file (before the channel)

A workspace without a channel uses the requests file
(`_inbox/requests.md` unless the marker says otherwise): the agent writes
each request as a paragraph, the coordinator writes the status back in the
same file. aided never reads it. One paragraph per request, with the same
field names as the tools:

```
## wr-20261120-01 · tool · pending
- date: 2026-11-20 (UTC)
- item: ripgrep
- version: 14.1.0
- purpose: search the code
- repo: app
- source: https://example.com/ripgrep
- note: (the coordinator writes the reply here)
```

| Requests file | Session channel |
|---|---|
| A paragraph headed `wr-<UTC date>-<n> · <kind> · <status>`, the id chosen by the agent | `workspace_request`; the id (`wr_<ULID>`) chosen by the machine |
| `kind`: `tool`, `permission`, `info`, `system_change` | The same |
| Fields `item`, `version`, `source`, `repo`, `purpose`, `scope`, `rollback` | The same tool arguments |
| `pending`, `approved`, `rejected`, `provisioned`, `confirmed` in the heading, written back by the coordinator (`confirmed` by the agent) | The same statuses, changed only as §3 says |
| `note` line | `note` |
| The coordinator reads the file on its rounds | The coordinator's inbox |

Once the marker names a coordinator, new requests go through the channel
with the same fields and statuses; requests already in the file finish
there.

## 6. Tests

- `internal/machine` `TestFindWorkspace`, `TestWorkspacePrompt`,
  `TestWorkspaceTemplateNamesNoMachine`, `TestCheckWorkspaceRequest`,
  `TestWorkspaceReply`; `internal/provider/claude` `TestInstructionsArgs`;
  `packages/core` and the Android app's transcript tests.
- e2e `TestWorkspaceRequests`: a project outside any workspace (nothing
  added), a workspace without a channel (the requests file), and two
  made-up layouts, one at the top of a drive other than the system's
  (Windows) and one whose path has spaces and Chinese: the prompt, the
  tools, bad and repeated requests, the coordinator's inbox, replies from
  others, plain messages and a file saying "approved" changing nothing,
  rejected information, a tool approved, provisioned and confirmed, the
  statuses after aided restarts, and the coordinator hearing of the
  confirmation.
- Desktop e2e `workspace.spec.ts` (TestDesktop): the conversation shows
  each change and who made it, with nothing to act on.
