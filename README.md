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
  <a href="https://github.com/samir2015dzh/hermesh/releases">Releases</a> ·
  <a href="#get-started">Get started</a> ·
  <a href="#run-your-own-server">Your server</a> ·
  <a href="docs/aide-rename.md">Names from before</a> ·
  <a href="README.zh-CN.md">简体中文</a>
</p>

Aide connects the assistants you talk to in the cloud with Claude Code, Codex and other coding agents on your computers. An assistant hands a coding agent a task, the agent works on your machine, and you watch every session live and answer its questions from your computer or phone. Every agent, machine and device is one you approved, and the server is your own.

The mark is the aide's star: the dot in the middle is you, the ring your orbit, and the small star in its gap your aide, going round you, ready to carry the word.

Aide is a fork of [Hermesh](https://github.com/hermeshai/hermesh), and was called Hermesh until it was renamed. What it keeps under the old name, and why, is in [docs/aide-rename.md](docs/aide-rename.md).

## Get started

### 1. Your server

Aide has no server of its own: each owner runs one ([Run your own server](#run-your-own-server)). Have its address ready.

### 2. The apps

| Platform | Download |
|---|---|
| **Windows** | `Aide-windows-x64-setup.exe` from the [latest release](https://github.com/samir2015dzh/hermesh/releases/latest) |
| **Android** | `Aide-<version>-debug.apk` from the same release |

Open the app and enter your server's address. The Windows app also makes that PC a machine your agents can run coding agents on. The Windows installer is not code-signed yet: click **More info › Run anyway** if SmartScreen asks.

### 3. More machines

On a Linux computer where coding agents should run:

```bash
curl -fsSL https://github.com/samir2015dzh/hermesh/releases/latest/download/install.sh | sh -s -- relay.example.com/o_xxxxxxxx
```

On a Windows PC without the app, in PowerShell:

```powershell
& ([scriptblock]::Create((irm https://github.com/samir2015dzh/hermesh/releases/latest/download/install.ps1))) relay.example.com/o_xxxxxxxx
```

Approve the machine in the app. Make sure Claude Code or Codex is installed and signed in as the same user, run `aided doctor` to check everything, and `aided project add <folder>` to let agents work in a folder. Reference: [docs/hermeshd.md](docs/hermeshd.md).

### 4. Your agents

A CLI agent (Claude Code, Codex, Cursor, your own) installs the `aide` command and asks to join:

```bash
curl -fsSL https://github.com/samir2015dzh/hermesh/releases/latest/download/install.sh | sh -s -- --cli
aide auth login relay.example.com/o_xxxxxxxx --name "Codex on the build box"
```

Approve it in the app, choosing what it may use. Cloud assistants connect over MCP to your server's gateway (below). Reference: [docs/cli.md](docs/cli.md), [docs/agent-messages.md](docs/agent-messages.md).

## Run your own server

The relay (`aide-relay`) is in this repository. You need a Linux host with Docker, a domain pointing at it, and ports 80 and 443 open. It gets its own certificate from Let's Encrypt:

```bash
docker run -d --name aide-relay --restart unless-stopped \
  -p 80:80 -p 443:443 -v aide-relay:/data \
  -e AIDE_RELAY_DOMAIN=relay.example.com -e AIDE_RELAY_MCP=1 \
  ghcr.io/samir2015dzh/aide-relay
docker logs aide-relay   # the one-time claim code
```

Enter the domain and the claim code in the app: the server belongs to the one owner who claims it. `AIDE_RELAY_MCP=1` turns on the MCP gateway for cloud assistants; `docker exec aide-relay aide-relay mcp-url` prints its secret address. Behind a reverse proxy, publish port 8080 and set `AIDE_RELAY_PUBLIC_URL=https://relay.example.com` instead of the domain.

A relay set up before the rename keeps running as it is: it reads the `HERMESH_RELAY_*` variables, its volume and its claim stay, and the image has `hermesh-relay` too. Back up the data volume: it holds the server's identity and your claim.

The relay stores public keys and signed grants, pending pairings, connection metadata and encrypted inbox messages until they are read. It never holds what your coding sessions say, nor the keys to read it.

## For developers

Building, testing and releasing: [docs/development.md](docs/development.md). The design: [docs/technical-design.md](docs/technical-design.md); Windows: [docs/windows.md](docs/windows.md); Android: [docs/android-app.md](docs/android-app.md). The brand's sources are in [assets/brand](assets/brand).

## License

Apache License 2.0, as Hermesh's. See [LICENSE](LICENSE).
