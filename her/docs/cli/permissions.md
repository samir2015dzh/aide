# Permissions

**her runs actions without asking.** There are no action approvals: no permission modes, no questions before a command, a file change, a tool from an MCP server, a browser step or a desktop app. A call runs unless something below refuses it, and nothing ever waits for an answer.

**her has no operating-system sandbox.** A command it runs can do what you can do. Run it where that is what you want, and use the refusals below for what must never happen.

## What refuses an action

- **Deny rules** you write. They see every command a line may run: behind `sudo`, `env` or `xargs`, inside `sh -c`, `eval`, `trap` and `find -exec`, under an absolute path and past git's global options.
- **Deleting a critical path**: the file system root, a top-level directory, the home directory, or the workspace and the folders that hold it.
- **Another bot's memory**: only the bot itself writes it.
- **The browser and desktop settings**: `[browser.sites]` entries set to `deny`, denied domains, cloud metadata addresses, scripts while `browser.script_enabled` is off, and apps set to `deny` under `[desktop.apps]`.

A refused call comes back to the model as `Tool call denied: …`, and the task goes on.

## Deny rules

```toml
[[rules]]
prefix = ["git", "push"]
decision = "deny"
reason = "Pushing is shared"

[[rules]]
prefix = ["mcp__mail__send"]
decision = "deny"
```

`prefix` matches a command's first words, or the full name of an MCP tool. `decision` is `deny`, the only one there is. Rules that said `allow` or `ask`, and the old `permission_mode`, `approval_mode` and `mcp_servers.<name>.approval` settings, are ignored with a warning.

## Trusting a project

A project's `.mcp.json`, and the settings in its `.her/config.toml` that decide what runs, are ignored until you trust the project. In a conversation her asks once, showing what the servers would run. `her trust` trusts the folder; `her trust --remove` takes that back.

`AGENTS.md` and a project's Skills are always read. A repository can use them to try to influence the model, so read what you clone.

## An agent running her

`her config set` and `unset` refuse settings about what the agent may do (rules, tools, MCP servers, the browser and desktop settings) while `AI_AGENT` is set, and say to run them in a terminal. This is one more step, not a boundary.
