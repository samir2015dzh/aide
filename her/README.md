# her

The agent kernel of Her, the agent built into Aide. It is a Rust workspace of its own, apart from the Go code in the rest of this repository.

An AI agent for your terminal, your editor and your programs. It reads and changes code, runs commands, searches the web, opens workbooks and PDFs, and works with the model you choose.

## Install

her is not released yet. Build it in this folder, on macOS, Linux or Windows:

```bash
cargo build --release -p her-cli
```

The program is `target/release/her`. The install script and `her upgrade` in [docs/cli/quickstart.md](docs/cli/quickstart.md) point at a release that does not exist yet.

Tests: `cargo test --workspace`. The document tests need PDFium: point `HER_PDFIUM` at `libpdfium.dylib` or `libpdfium.so`.

## Thirty seconds

```bash
her login
```

```bash
her "explain what this repository does"
```

`her login` connects a model: an API key, a Token Plan, a ChatGPT subscription, a local Ollama or a compatible service of your own. Nothing is assumed: until you set one up, her says so.

## Three ways in

**In the terminal.** `her` starts a conversation that stays in your terminal's own scrollback. It asks before changing anything until you tell it otherwise.

**In scripts.** One task, one result:

```bash
her exec --json "summarize the changes on this branch" | jq -r .final_output
```

Exit codes say what happened: 0 done, 1 failed, 2 bad usage or no model, 130 interrupted. See [scripting](docs/cli/scripting.md).

**From other programs.** `her acp` serves editors over the Agent Client Protocol ([acp.md](docs/cli/acp.md)). `her serve` speaks JSON-RPC ([json-rpc.md](docs/cli/json-rpc.md)); [examples/rpc](examples/rpc) has a complete client in a page of Python.

## What you should know

- **her runs actions without asking, and there is no operating-system sandbox.** There are no action approvals; deny rules and a few built-in refusals are what stops an action: [permissions](docs/cli/permissions.md).
- **A repository can try to influence the model.** `AGENTS.md` and a project's Skills are always read.
- **No telemetry.** Every network access is listed in [privacy](docs/cli/privacy.md).

## For coding agents

If you are a coding agent setting her up or calling it:

- Run a task with `her exec --json "TASK"`. The answer is in `final_output`; `exit_code`, `error` and `tool_calls` say how it went. `her exec --resume last "NEXT"` continues.
- `her status --json`, `her doctor --json`, `her models --json` and `her tools --json` look at the environment and change nothing.
- Failures under `--json` are JSON on stdout: `{"error": {"kind", "message", "help"}, "exit_code": 2}`.
- her asks nobody before an action: every tool call runs unless a deny rule or a built-in refusal stops it (`Tool call denied: …`). Do not work around a refusal.
- A key goes through standard input, never an argument: `printf %s "$KEY" | her login deepseek --key-stdin --model deepseek/deepseek-flash`.
- `her config set` refuses settings about what the agent may do (rules, tools, MCP servers) while `AI_AGENT` is set. They are for the person.
- The documentation index is [docs/cli/llms.txt](docs/cli/llms.txt).

## Documentation

[Quickstart](docs/cli/quickstart.md) · [Models](docs/cli/models.md) · [Tools](docs/cli/tools.md) · [Permissions](docs/cli/permissions.md) · [Scripting](docs/cli/scripting.md) · [ACP](docs/cli/acp.md) · [JSON-RPC](docs/cli/json-rpc.md) · [Privacy](docs/cli/privacy.md) · [Troubleshooting](docs/cli/troubleshooting.md)
