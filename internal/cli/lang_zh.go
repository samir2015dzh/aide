package cli

// The Chinese words (work order #38): the help of every command, the
// common flags, cobra's usage frame, and what commands print as a person
// reads it. Commands, flags, ids, addresses and names stay as they are.

type helpText struct{ short, long string }

var helpZH = map[string]helpText{
	"aide":                   {short: "通过 Aide 在属主的机器上驱动 Claude Code、Codex 等编码 agent"},
	"aide agent":             {short: "本客户端可运行的编码 agent，以及在本机上设置它"},
	"aide agent list":        {short: "列出本客户端在每台机器上可运行的编码 agent，及其提供的模型与推理强度"},
	"aide agent login":       {short: "在本机登录官方 Antigravity ACP 服务"},
	"aide auth":              {short: "把本 agent 登录到属主的服务器、检查登录状态或忘掉登录"},
	"aide auth login":        {short: "登录属主的服务器：本机 Aide 应用已登录时无需输入，否则用账号和密码", long: authLoginLongZH},
	"aide auth logout":       {short: "登出本客户端：本机 Aide 应用吊销它的授权，本机的密钥和授权一并删除", long: authLogoutLongZH},
	"aide auth status":       {short: "检查本客户端的登录：身份、授权与服务器连接", long: authStatusLongZH},
	"aide link":              {short: "在命令之间保持本 agent 到中继和各机器的连接"},
	"aide login":             {short: "登录属主的服务器（同 aide auth login）"},
	"aide machine":           {short: "直接操作属主的机器"},
	"aide machine exec":      {short: "在一台机器上执行一次性 shell 命令，取回输出和退出码"},
	"aide message":           {short: "属主的 agent 与会话之间的消息：你的收件箱，以及你发出消息的去向"},
	"aide message ack":       {short: "确认已处理的消息：邮局忘掉它们，发送方看到已处理"},
	"aide message contacts":  {short: "你能写信给的属主 agent 和机器，及其地址"},
	"aide message hook":      {short: "Claude Code 与 Codex 运行的钩子，让发给某个对话的消息送达；--install 把它加进 agent 的设置"},
	"aide message list":      {short: "显示等你处理的消息（--as：给该名字的；否则给本 agent 自己的），旧的在前；确认（message ack）之前一直保留"},
	"aide message send":      {short: "写信给属主的某个 agent 或某个会话：给会话传话的唯一方式"},
	"aide message status":    {short: "你发出的消息怎么样了：等待、已取走、已交付、已处理、已回复、待放行、被拒、过期或未知"},
	"aide message wake":      {short: "请属主让 Aide 在有消息时唤醒你（例如 Grok Bot 例程的 webhook）；打印一个一次性链接给属主"},
	"aide message watch":     {short: "在 Claude Code 对话运行期间，把发给它的消息随到随投进它的收件 socket"},
	"aide project":           {short: "本客户端可以工作的项目，写作 机器/项目"},
	"aide project list":      {short: "列出本客户端可以工作的项目（机器/项目），及在那里可运行的编码 agent"},
	"aide roster":            {short: "消息能送到的每个对象：属主的 agent，以及你能到达的机器上的会话"},
	"aide session":           {short: "启动和管理编码 agent 会话"},
	"aide session answer":    {short: "回答 agent 提出的问题（它在等）"},
	"aide session archive":   {short: "归档用完的会话：变为只读并释放工作树（分支保留成果）"},
	"aide session diff":      {short: "显示会话的改动（累计，或某一 --turn；仅工作树项目）"},
	"aide session events":    {short: "分页显示会话的事件（消息、工具调用、回合、提问）：默认最新一页"},
	"aide session get":       {short: "显示一个会话：状态与进度；有提问时显示问题；回合结束后显示 agent 的完整结果"},
	"aide session insights":  {short: "复盘一个会话：每回合用时、失败与重试、提问、工具调用、改动与 token，并给出更好的提示词（Markdown）"},
	"aide session interrupt": {short: "停止进行中的回合；会话保留，下一条消息在同一对话里继续"},
	"aide session list":      {short: "列出本客户端在所有可达机器上的会话（不含已归档，除非加 --archived）"},
	"aide session push":      {short: "用机器自己的 git 登录把会话分支推到仓库远端（仅工作树会话；Aide 从不擅自推送）"},
	"aide session rollback":  {short: "把空闲的工作树会话退回第 N 回合结束时，丢弃之后回合的对话与改动"},
	"aide session send":      {short: "给会话发消息；它在工作时，消息并入正在进行的回合"},
	"aide session start":     {short: "在项目或 GitHub 仓库里启动编码 agent 会话（Claude Code、Codex、Gemini、Cursor 等）"},
	"aide session unarchive": {short: "恢复已归档的会话：工作树恢复，重新接收消息"},
	"aide session wait":      {short: "等会话的回合结束，然后像 session get 一样显示它（退出码：最后一回合的结果）"},
	"aide update":            {short: "把本机的 Aide 更新到最新版本"},
	"aide usage":             {short: "显示每台机器上的 Claude 套餐用量（5 小时窗口与周限），用于排期"},
	"aide version":           {short: "显示本 CLI 的版本"},
	"aide work":              {short: "属主机器上的编码 agent 最近几天做了什么：会话、回合、token 与用时，按 agent 和派活方分列"},
	"aide help":              {short: "任一命令的帮助"},
	"aide completion":        {short: "生成指定 shell 的自动补全脚本"},
}

const authLoginLongZH = `登录属主的服务器（docs/accounts.md）。

本机 Aide 应用已登录时无需输入任何东西：不给服务器（无参数、无 $AIDE_SERVER、无
--account），或加 --from-app，本 agent 就经应用登录——应用请服务器为它签发授权，
不再问账号和密码。它仍是独立的 agent，有自己的密钥，在应用的「Agent」里和其他
agent 一样列出、吊销。

否则用属主的账号和密码登录：服务器是参数（relay.example.com），否则取
$AIDE_SERVER，没有默认值；账号取 --account，否则 $AIDE_ACCOUNT，否则询问；密码
在终端询问（或用 --password-stdin 从标准输入读，或取 $AIDE_PASSWORD）。agent 可以
使用所有机器、项目和编码 agent，直到被吊销；属主改密码时它会被登出。

已登录到这台服务器时，它会向服务器确认，属主仍接纳本 agent 就什么也不做；属主
吊销后则作为新 agent 重新登录。要登录另一个属主，先运行 ` + "`aide auth logout`" + `，
除非旧属主已从服务器消失（清空了一切）：那时直接登录新属主。`

const authLogoutLongZH = `登出本客户端。本机 Aide 应用登录在同一属主下时，先由应用吊销本客户端的授权
（登出即终止这个 agent，与应用里的登出按钮相同）；否则请属主在 Aide 应用里吊销。
两种情况下本机的密钥和授权都会删除。应用自身的登录不受影响。`

const authStatusLongZH = `逐步检查本客户端的登录：本机保存的身份、属主的授权、中继连接，以及授权是否为
属主的最新版本。每个失败的步骤都会说明怎么办。agent 能工作时（含警告）退出码 0；
未登录、被吊销或授权过期时 3；中继不可达时 4。`

var flagsZH = map[string]string{
	"output format: text (to read) or json (one document, see docs/cli-output.md); AIDE_OUTPUT=json makes json the default": "输出格式：text（给人读）或 json（一个文档，见 docs/cli-output.md）；AIDE_OUTPUT=json 设为默认",
	asHelp:   "当多个 bot 或对话共用本 agent 时，你在其下的名字：你自己的名字（或对话的键）；回信和动态回到这个名字。默认取 $AIDE_AS（message hook 在 Claude Code 里设置）",
	langHelp: "显示语言：zh-CN、en 或 auto（默认：$AIDE_LANG，否则随系统）；只影响给人读的文字，-o json 不变",
	"name shown to the owner (default: the current name when logging in again, else client@<hostname>; at most 64 bytes)": "给属主看的名字（默认：重新登录时沿用当前名字，否则 client@<主机名>；最多 64 字节）",
	"the owner's account (default: $AIDE_ACCOUNT, else asked for)":                                                        "属主的账号（默认：$AIDE_ACCOUNT，否则询问）",
	"read the password from stdin": "从标准输入读取密码",
	"log in through the Aide app logged in on this computer: no account or password": "经本机已登录的 Aide 应用登录：无需账号和密码",
	"only this machine (name or id)":             "只看这台机器（名字或 id）",
	"machine name or id":                         "机器名或 id",
	"machine name or id (default: all)":          "机器名或 id（默认：全部）",
	"message text":                               "消息正文",
	"read the message from a file (- for stdin)": "从文件读取消息（- 表示标准输入）",
	"also list the DeepSeek Harness sessions Aide did not start (read-only, ids dsh_…)": "同时列出不是 Aide 启动的 DeepSeek Harness 会话（只读，id 为 dsh_…）",
	"list the archived sessions instead":                                                "改为列出已归档的会话",
	"wait until a message arrives":                                                      "等到有消息到达",
	"only say whether a newer version exists":                                           "只告知是否有新版本",
	"update without asking, even when that stops a session":                             "不询问直接更新，即使会停止会话",
	"how many days, today the last of them (1: today)":                                  "多少天，今天为最后一天（1：只看今天）",
	"summary only":    "只显示摘要",
	"a short subject": "简短的主题",
}

const usageZH = `用法：{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [命令]{{end}}{{if gt (len .Aliases) 0}}

别名：
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

示例：
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}

可用命令：{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

选项：
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

全局选项：
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableSubCommands}}

运行 "{{.CommandPath}} [命令] --help" 查看命令的详细说明。{{end}}
`

// catalogZH: what commands print, in Chinese. ${n} is what the n-th
// %-verb matched.
var catalogZH = []zhf{
	// Logging in.
	{"Logged in to %s through the Aide app on this computer, as %q: no password needed. Run `aide project list` to see where it can work.",
		"已经由本机 Aide 应用登录到 ${1}，名字为 ${2}，无需密码。运行 `aide project list` 查看它可以在哪里工作。"},
	{"Logged in to %s as %q. Run `aide project list` to see where it can work.", "已登录到 ${1}，名字为 ${2}。运行 `aide project list` 查看它可以在哪里工作。"},
	{"Already logged in to owner %s as %q. Run `aide auth status` for details.", "已经以 ${2} 登录到属主 ${1}。运行 `aide auth status` 查看详情。"},
	{"The previous owner %s is gone from its server (it erased everything); logging in to the new one.", "之前的属主 ${1} 已从服务器消失（清空了一切）；改为登录新属主。"},
	{"The grant is no longer valid (%v); logging in again as a new client.", "授权已失效（${1}）；作为新客户端重新登录。"},
	{"The owner no longer admits this client (%s); logging in again as a new client.", "属主已不再接纳本客户端（${1}）；作为新客户端重新登录。"},
	{"Account: ", "账号："},
	{"Update now? [y/N] ", "现在更新？[y/N] "},
	{". Roll back? [y/N] ", "。确定退回？[y/N] "},
	{"Password: ", "密码："},
	// Logging out.
	{"Logged out: the Aide app revoked client %q (%s), and its key and grant here are forgotten.", "已登出：Aide 应用已吊销客户端 ${1}（${2}），本机的密钥和授权已删除。"},
	{"Forgot this client's key and grant. Owner %s still lists client %q (%s) until they revoke it in the Aide app or the console.",
		"已删除本客户端的密钥和授权。属主 ${1} 在 Aide 应用或控制台吊销之前，仍会列出客户端 ${2}（${3}）。"},
	{"Not logged in: nothing to forget.", "未登录：没有可删除的。"},
	{"Removed the login files in %s.", "已删除 ${1} 中的登录文件。"},
	{"warning: the Aide app could not revoke this client: %s", "警告：Aide 应用未能吊销本客户端：${1}"},
	// auth status.
	{"aide %s · config %s", "aide ${1} · 配置目录 ${2}"},
	{"skipped (not logged in)", "已跳过（未登录）"},
	{"skipped (no identity)", "已跳过（无身份）"},
	{"skipped (the relay refused this client)", "已跳过（中继拒绝了本客户端）"},
	{"skipped (relay unreachable)", "已跳过（中继不可达）"},
	{"run `aide auth login`: with the Aide app logged in on this computer there is nothing to type; elsewhere `aide auth login <your server> --account <account>` logs in with the owner's account and password",
		"运行 `aide auth login`：本机 Aide 应用已登录时无需输入；在其他电脑上用 `aide auth login <你的服务器> --account <账号>` 以属主的账号和密码登录"},
	{"cannot read the login stored in %s: %v", "无法读取 ${1} 中保存的登录：${2}"},
	{"run `aide auth logout`, then `aide auth login <server>`", "运行 `aide auth logout`，再运行 `aide auth login <服务器>`"},
	{"run `aide auth login %s` to ask the owner again", "运行 `aide auth login ${1}` 重新向属主申请"},
	{"; the key file is readable by others", "；密钥文件可被他人读取"},
	{"refused by the relay: %s", "被中继拒绝：${1}"},
	{"%s is unreachable: %v", "${1} 不可达：${2}"},
	{"check the network; if the owner moved the relay, run `aide auth login <new address>`", "检查网络；如果属主迁移了中继，运行 `aide auth login <新地址>`"},
	{"revoked by the owner", "已被属主吊销"},
	{"could not check for a newer grant: %v", "无法检查是否有更新的授权：${1}"},
	{"updated v%d → v%d", "已更新 v${1} → v${2}"},
	{"up to date", "已是最新"},
	{"check this computer's clock", "检查本机时钟"},
	{"ask the owner to renew it in the Aide app", "请属主在 Aide 应用里续期"},
	{" · doesn't expire", " · 不过期"},
	{" · expires in %s", " · ${1}后过期"},
	{" · expired %s ago", " · 已过期 ${1}"},
	{"client %q (%s), owner %s", "客户端 ${1}（${2}），属主 ${3}"},
	{"all machines, all projects", "全部机器、全部项目"},
	{" · all agents", " · 全部 agent"},
	{" · sees own sessions", " · 只看自己的会话"},
	{" · sees all sessions", " · 看全部会话"},
	{"less than a minute", "不到一分钟"},
	{"%d days", "${1} 天"},
	{"%d hours", "${1} 小时"},
	{"%d minutes", "${1} 分钟"},
	{"1 day", "1 天"},
	{"1 hour", "1 小时"},
	{"1 minute", "1 分钟"},
	// auth status's steps, padded as it pads them (%-11s): a Chinese
	// character takes two columns.
	{" identity    ", " 身份        "},
	{" grant       ", " 授权        "},
	{" relay       ", " 中继        "},
	{" grant sync  ", " 授权同步    "},
	{"help for %s", "${1} 的帮助"},
	{"(default %s)", "（默认 ${1}）"},
	{"not logged in", "未登录"},
	// Errors, as execute prints them.
	{"error: not logged in yet", "错误：尚未登录"},
	{"error: ", "错误："},
	{"this client's access has been revoked by the owner", "本客户端的访问已被属主吊销"},
	{"which server? give your server's address (relay.example.com) or a connection address (relay.example.com/o_…), or set $AIDE_SERVER",
		"哪台服务器？请给出服务器地址（relay.example.com）或连接地址（relay.example.com/o_…），或设置 $AIDE_SERVER"},
	{"the Aide app on this computer is not running, or not logged in", "本机的 Aide 应用没有运行，或没有登录"},
	{"open the Aide app and log in, or log in with the account: `aide auth login <your server> --account <account>`",
		"打开 Aide 应用并登录，或用账号登录：`aide auth login <你的服务器> --account <账号>`"},
	{"logging in through the Aide app: %s", "经 Aide 应用登录：${1}"},
	{"logging in: %v", "登录：${1}"},
	{"no terminal to ask for the password: use --password-stdin or $AIDE_PASSWORD", "没有终端可以询问密码：请用 --password-stdin 或 $AIDE_PASSWORD"},
	{"no terminal to ask for it: use --account or $AIDE_ACCOUNT", "没有终端可以询问：请用 --account 或 $AIDE_ACCOUNT"},
	{"no password given", "没有输入密码"},
	{"cannot reach the server to check the current login: %v", "无法连接服务器检查当前登录：${1}"},
	{"already logged in to owner %s (%s)", "已经登录到属主 ${1}（${2}）"},
	{"run `aide auth logout` first to log in to another owner", "要登录另一个属主，先运行 `aide auth logout`"},
	{"unknown command %q for %q", "${2} 没有命令 ${1}"},
	// Lists and sessions.
	{"no sessions", "没有会话"},
	{"no DSH native sessions", "没有 DSH 原生会话"},
	{"no coding agent this client may run", "没有本客户端可运行的编码 agent"},
	{"no projects this client can work in: ask the owner to add one, or to grant this client one, in the Aide app", "没有本客户端可以工作的项目：请属主在 Aide 应用里添加一个，或给本客户端授权一个"},
	{"LOST: last known, as this client last read them; their machines cannot be read now", "失联：以下是本客户端上次读到的状态；它们所在的机器现在读不到"},
	{"offline: %s (%s); its sessions are not listed", "离线：${1}（${2}）；它的会话没有列出"},
	{"offline: %s (%s); its work is not counted", "离线：${1}（${2}）；它的工作没有计入"},
	{"%s is paused on %s", "${1} 在 ${2} 上已暂停"},
	{"%s to %s: %d sessions, %d done, %d turns (%d failed), %d tokens in, %d out, %s busy", "${1} 至 ${2}：${3} 个会话，${4} 个完成，${5} 个回合（${6} 个失败），输入 ${7} token，输出 ${8}，忙碌 ${9}"},
	{"session %s · DSH native, %s · %s · %s · %s", "会话 ${1} · DSH 原生，${2} · ${3} · ${4} · ${5}"},
	{"session %s is %s; waiting for its turn to finish", "会话 ${1} 状态为 ${2}；等它的回合结束"},
	{"session %s in %s/%s (GitHub %s, branch %s)", "会话 ${1}，在 ${2}/${3}（GitHub ${4}，分支 ${5}）"},
	{"session %s in %s/%s", "会话 ${1}，在 ${2}/${3}"},
	{"sent to %s on %s (DSH takes it after the turn running there)", "已发给 ${2} 上的 ${1}（DSH 在当前回合结束后接收）"},
	{"last activity: %s", "最近活动：${1}"},
	{"title: %s", "标题：${1}"},
	{"model: %s · permission: %s", "模型：${1} · 权限：${2}"},
	{"→ its conversation: `aide session events %s`", "→ 它的对话：`aide session events ${1}`"},
	{"→ it runs on; check it with `aide session get %s` (status, then the full result once idle)", "→ 它继续在跑；用 `aide session get ${1}` 查看（先看状态，空闲后看完整结果）"},
	{"changes: %s changed, +%d -%d (`aide session diff %s`)", "改动：${1} 个文件，+${2} -${3}（`aide session diff ${4}`）"},
	{"failure: %s: %s", "失败：${1}：${2}"},
	{"note: `aide session send` is now `aide message send --to %s`", "注意：`aide session send` 现在是 `aide message send --to ${1}`"},
	{"pushed %s to the remote origin", "已把 ${1} 推到远端 origin"},
	{"open a pull request: %s", "开一个 PR：${1}"},
	{"(%s cut at 1 MiB)", "（${1} 超过 1 MiB 已截断）"},
	{"── turn %d ──", "── 第 ${1} 回合 ──"},
	{"✓ turn %d done", "✓ 第 ${1} 回合完成"},
	{"✗ turn %d failed", "✗ 第 ${1} 回合失败"},
	{"  ↳ no answer: the agent continues on its own", "  ↳ 没有回答：agent 自行继续"},
	{"  ↳ no longer asked", "  ↳ 不再询问"},
	{"  ↳ answered by %s: %s", "  ↳ ${1} 回答：${2}"},
	{"  ↳ declined by %s", "  ↳ ${1} 拒绝回答"},
	{"↺ rolled back to turn %d by %s: turns %d to %d discarded (were at %s)", "↺ ${2} 退回到第 ${1} 回合：第 ${3} 至 ${4} 回合已丢弃（原在 ${5}）"},
	// Messages.
	{"No messages.", "没有消息。"},
	{"More are waiting: acknowledge these, then list again.", "还有更多：先确认这些，再列一次。"},
	{"Acknowledged %d (%s).", "已确认 ${1} 条（${2}）。"},
	{"%s → %s (%s): waiting in its inbox", "${1} → ${2}（${3}）：在它的收件箱里等着"},
	{"held for your owner: message %s: it matches the owner's information boundary: %s", "已为属主暂扣：消息 ${1}：命中属主的信息边界：${2}"},
	{"held for your owner: message %s: %s", "已为属主暂扣：消息 ${1}：${2}"},
	{"warning: refusing message %s: %v", "警告：拒收消息 ${1}：${2}"},
	{"Tell your owner:", "告诉你的属主："},
	{"The hooks are already in %s.", "钩子已在 ${1} 里。"},
	{"Added to %s, for %s: %s", "已加入 ${1}，用于 ${2}：${3}"},
	{"They take effect in the next Claude Code conversation.", "从下一个 Claude Code 对话起生效。"},
	{"Codex runs a new hook once its user trusts it: /hooks in Codex.", "Codex 要在用户信任后才运行新钩子：在 Codex 里用 /hooks。"},
	// Machines and agents.
	{"Follow the Antigravity sign-in link below. Press Ctrl+C to cancel.", "按下面的 Antigravity 登录链接操作。按 Ctrl+C 取消。"},
	{"Antigravity ACP signed in. Aide can now start its sessions.", "Antigravity ACP 已登录。Aide 现在可以启动它的会话了。"},
	{"warning: the relay cannot be reached (%v): only this computer's own machine can be used, as last listed", "警告：中继不可达（${1}）：只能使用本机自己的机器（按上次的列表）"},
	{"warning: registering this agent's inbox:", "警告：登记本 agent 的收件箱："},
	{"warning: telling the owner this agent is erasing:", "警告：通知属主本 agent 正在清除："},
	// Everyday lines.
	{"%d message(s) wait in your inbox: aide message list", "收件箱里有 ${1} 条消息等你处理：aide message list"},
	{"no machine to read Claude usage on", "没有可以读取 Claude 用量的机器"},
	{"offline: cannot be reached: ", "离线：无法连接："},
	{"offline: last seen %s ago", "离线：最后在线于 ${1}前"},
	{"offline: never seen", "离线：从未上线"},
	{"no Claude Code", "没有 Claude Code"},
	{"read-fail: ", "读取失败："},
	{"last refresh: ", "上次刷新："},
	{"no windows", "没有用量窗口"},
}
