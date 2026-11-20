<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/logo-dark.svg">
    <img src="docs/assets/logo-light.svg" alt="Aide" height="44">
  </picture>
</p>

<h1 align="center">你的 agent 的副官</h1>

<p align="center">
  云端助手和你自己机器上的编码 agent，经由你自己运行的端到端加密网络互相传令、交接工作。
</p>

<p align="center">
  <a href="https://github.com/samir2015dzh/aide/releases">发布页</a> ·
  <a href="#开始使用">开始使用</a> ·
  <a href="#运行你自己的服务器">你的服务器</a> ·
  <a href="docs/aide-rename.md">改名前的名字</a> ·
  <a href="README.md">English</a>
</p>

Aide 把你在云端对话的助手，和你电脑上的 Claude Code、Codex 等编码 agent 连在一起。助手把任务交给编码 agent，agent 在你的机器上干活，每个会话你都能在电脑或手机上实时看到，并回答它的提问。每个 agent、机器和设备都经过你的批准，服务器也是你自己的。

图标叫「副官星」：中间的实点是你，外圈是你的轨道，缺口里的小星是你的副官，绕着你转，随时传令。

Aide 是 [Hermesh](https://github.com/hermeshai/hermesh) 的分支，改名前就叫 Hermesh。哪些地方保留了旧名、为什么保留，见 [docs/aide-rename.md](docs/aide-rename.md)。

## 开始使用

### 1. 你的服务器

Aide 没有官方服务器：每个使用者自己运行一台（见[运行你自己的服务器](#运行你自己的服务器)）。先准备好它的地址。

### 2. 应用

| 平台 | 下载 |
|---|---|
| **Windows** | [最新发布](https://github.com/samir2015dzh/aide/releases/latest)里的 `Aide-windows-x64-setup.exe` |
| **Android** | 同一个发布里的 `Aide-<版本>-debug.apk` |

打开应用，填你服务器的地址。Windows 应用同时把这台电脑变成能跑编码 agent 的机器。Windows 安装包还没有代码签名：SmartScreen 拦的话，点「更多信息 › 仍要运行」。

### 3. 更多机器

在要跑编码 agent 的 Linux 电脑上：

```bash
curl -fsSL https://github.com/samir2015dzh/aide/releases/latest/download/install.sh | sh -s -- relay.example.com/o_xxxxxxxx
```

没装应用的 Windows 电脑，在 PowerShell 里：

```powershell
& ([scriptblock]::Create((irm https://github.com/samir2015dzh/aide/releases/latest/download/install.ps1))) relay.example.com/o_xxxxxxxx
```

在应用里批准这台机器。确认 Claude Code 或 Codex 已经以同一个用户装好并登录，用 `aided doctor` 检查一遍，用 `aided project add <文件夹>` 让 agent 在某个文件夹里干活。参考：[docs/hermeshd.md](docs/hermeshd.md)。

### 4. 你的 agent

命令行 agent（Claude Code、Codex、Cursor 或你自己的）安装 `aide` 命令并申请加入：

```bash
curl -fsSL https://github.com/samir2015dzh/aide/releases/latest/download/install.sh | sh -s -- --cli
aide auth login relay.example.com/o_xxxxxxxx --name "构建机上的 Codex"
```

在应用里批准，并选好它能用什么。云端助手通过 MCP 连到你服务器的网关（见下）。参考：[docs/cli.md](docs/cli.md)、[docs/agent-messages.md](docs/agent-messages.md)。

## 运行你自己的服务器

中转服务器（`aide-relay`）就在这个仓库里。需要一台装了 Docker 的 Linux 主机、一个指向它的域名，以及开放的 80、443 端口。证书由它自己向 Let's Encrypt 申请：

```bash
docker run -d --name aide-relay --restart unless-stopped \
  -p 80:80 -p 443:443 -v aide-relay:/data \
  -e AIDE_RELAY_DOMAIN=relay.example.com -e AIDE_RELAY_MCP=1 \
  ghcr.io/samir2015dzh/aide-relay
docker logs aide-relay   # 一次性的认领码
```

在应用里填域名和认领码：服务器归第一个认领它的人。`AIDE_RELAY_MCP=1` 打开给云端助手用的 MCP 网关，`docker exec aide-relay aide-relay mcp-url` 打印它的秘密地址。放在反向代理后面时，发布 8080 端口，并用 `AIDE_RELAY_PUBLIC_URL=https://relay.example.com` 代替域名。

改名前部署的中转照常运行：它读 `HERMESH_RELAY_*` 变量，数据卷和认领都不变，镜像里也还有 `hermesh-relay`。记得备份数据卷：服务器的身份和你的认领都在里面。

中转只存公钥和签名授权、待处理的配对、连接元数据，以及未读的加密消息。它从来拿不到编码会话的内容，也没有读取这些内容的钥匙。

## 开发

构建、测试与发布：[docs/development.md](docs/development.md)。设计：[docs/technical-design.md](docs/technical-design.md)；Windows：[docs/windows.md](docs/windows.md)；Android：[docs/android-app.md](docs/android-app.md)。品牌源文件在 [assets/brand](assets/brand)。

## 许可

Apache License 2.0，与 Hermesh 相同。见 [LICENSE](LICENSE)。
