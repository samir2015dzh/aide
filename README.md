<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/logo-dark.svg">
    <img src="docs/assets/logo-light.svg" alt="Aide" height="44">
  </picture>
</p>

<h1 align="center">Your agents' aide</h1>

<p align="center">
  Cloud assistants and the coding agents on your own machines, passing work to each other over an end-to-end encrypted network that you run yourself.
</p>

<p align="center">
  <a href="https://github.com/samir2015dzh/aide/releases">Releases</a> ·
  <a href="#get-started">Get started</a> ·
  <a href="#run-your-own-server">Your server</a> ·
  <a href="docs/aide-rename.md">Names from before</a> ·
  <a href="README.zh-CN.md">简体中文</a>
</p>

Aide connects the assistants you talk to in the cloud with Claude Code, Codex and other coding agents on your computers. An assistant hands a coding agent a task, the agent works on your machine, and you watch every session live and answer its questions from your computer or phone. Every agent, machine and device logs in with your account and password, and the server that keeps them is your own.

The mark is the aide's star: the dot in the middle is you, the ring your orbit, and the small star in its gap your aide, going round you, ready to carry the word.

Aide started as a fork of an open-source project and is now its own: it follows no upstream. How data and installations made before the rename keep working is in [docs/aide-rename.md](docs/aide-rename.md).

## Get started

### 1. Your server

Aide has no server of its own: each owner runs one ([Run your own server](#run-your-own-server)). Set it up once with your first device; it keeps your account and password, and everything else logs in with them.

### 2. The apps

| Platform | Download |
|---|---|
| **Windows** | `Aide-windows-x64-setup.exe` from the [latest release](https://github.com/samir2015dzh/aide/releases/latest) |
| **Android** | `Aide-<version>-debug.apk` from the same release |

Open the app and choose **Log In**: your server, account and password. The first time, choose **Set Up a New Server** instead, with the setup code the server printed (below). The Windows app also makes that PC a machine your agents can run coding agents on. The Windows installer is not code-signed yet: click **More info › Run anyway** if SmartScreen asks.

### 3. More machines

On a Linux computer where coding agents should run:

```bash
curl -fsSL https://github.com/samir2015dzh/aide/releases/latest/download/install.sh | sh -s -- relay.example.com
```

On a Windows PC without the app, in PowerShell:

```powershell
& ([scriptblock]::Create((irm https://github.com/samir2015dzh/aide/releases/latest/download/install.ps1))) relay.example.com
```

It asks for your account and password (or takes them from `AIDE_ACCOUNT` and `AIDE_PASSWORD`), logs the machine in and starts its background service: it is one of your machines at once. Make sure Claude Code or Codex is installed and signed in as the same user, run `aided doctor` to check everything, and `aided project add <folder>` to let agents work in a folder. Reference: [docs/aided.md](docs/aided.md).

### 4. Your agents

A CLI agent (Claude Code, Codex, Cursor, your own) installs the `aide` command and logs in:

```bash
curl -fsSL https://github.com/samir2015dzh/aide/releases/latest/download/install.sh | sh -s -- --cli
aide auth login relay.example.com --account me --name "Codex on the build box"
```

It asks for the password (or reads it with `--password-stdin`, or from `AIDE_PASSWORD`) and is signed in at once: it may use every machine, project and coding agent until you revoke it in the app. Cloud assistants connect over MCP to your server's gateway (below), and log in on its page with the same account and password. Reference: [docs/cli.md](docs/cli.md), [docs/agent-messages.md](docs/agent-messages.md).

## Run your own server

The relay (`aide-relay`) is in this repository. You need a Linux host with Docker, a domain pointing at it, and ports 80 and 443 open. It gets its own certificate from Let's Encrypt:

```bash
docker run -d --name aide-relay --restart unless-stopped \
  -p 80:80 -p 443:443 -v aide-relay:/data \
  -e AIDE_RELAY_DOMAIN=relay.example.com -e AIDE_RELAY_MCP=1 \
  ghcr.io/samir2015dzh/aide-relay
docker logs aide-relay   # the one-time setup code, valid for an hour
```

In the app, choose **Set Up a New Server** and enter the domain, the setup code, and the account and password the server will keep (`docker exec aide-relay aide-relay setup-code` prints the code again; once its hour is over the code is refused, and that command prints a new one; `AIDE_RELAY_SETUP_CODE_TTL=24h`, or `--setup-code-ttl`, sets another time, `0` none). The server belongs to the one owner who sets it up. A forgotten password is set again on the server: `docker exec -i aide-relay aide-relay password --account me` reads the new one on stdin; restart the server after. `AIDE_RELAY_MCP=1` turns on the MCP gateway for cloud assistants; `docker exec aide-relay aide-relay mcp-url` prints its secret address. Behind a reverse proxy, publish port 8080 and set `AIDE_RELAY_PUBLIC_URL=https://relay.example.com` instead of the domain.

A relay set up before the rename keeps running as it is: it reads the `AIDE_RELAY_*` variables, its volume and its owner stay, and the image has `aide-relay` too. One set up before accounts with the old password login turns it into the account at the first login with that password ([docs/accounts.md](docs/accounts.md) §5). Back up the data volume: it holds the server's key and your account.

The relay stores public keys and signed grants, your account (the password as an Argon2id hash), a key of its own that signs in whoever logs in, connection metadata and encrypted inbox messages until they are read. Sessions are end-to-end encrypted between your machines and whoever reaches them, so it never sees what your coding sessions say. But it is the authority: whoever has your password, or a copy of the server's data, can sign themselves in as you ([docs/accounts.md](docs/accounts.md) §7). Serve it over HTTPS and keep the volume safe.

## For developers

Building, testing and releasing: [docs/development.md](docs/development.md). The design: [docs/technical-design.md](docs/technical-design.md); Windows: [docs/windows.md](docs/windows.md); Android: [docs/android-app.md](docs/android-app.md). The brand's sources are in [assets/brand](assets/brand).

## License

Apache License 2.0, as Aide's. See [LICENSE](LICENSE).
