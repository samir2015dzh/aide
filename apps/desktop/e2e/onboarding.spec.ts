import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-node";
import { Workspace } from "@aide/proto/identity";
import { LocalService } from "@aide/proto/local";
import { expect, test, type ElectronApplication, type Page } from "@playwright/test";
import { execFileSync, spawn } from "node:child_process";
import fs from "node:fs";
import http from "node:http";
import type { AddressInfo } from "node:net";
import os from "node:os";
import path from "node:path";
import { answerMenu, fixture, gitRepo, launch, pickAnswers, snap, sessionList, navTo, mapHeight } from "./app";

// The first run against owner daemons nobody has set up (the fixture's
// fresh ones, each with a server of its own not set up either): a new
// server set up with its setup code through to the first task, and logging
// in to a server set up already, as one of the owner's devices or as a
// machine only (docs/accounts.md). Every way starts with the server's
// address typed: there is no official service and no default server.

test.describe.configure({ timeout: 180_000 });

// The account every test sets up or logs in with (as the Go harness's).
const account = "owner";
const password = "correct horse battery";

/** The window grew into the main window. */
async function expectMainWindow(app: ElectronApplication, page: Page) {
  // A session on a small display keeps the navigation in its icon rail.
  await expect(page.locator(".side-nav, .rail-nav")).toBeVisible();
  await expect.poll(() => app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0]?.getBounds().width ?? 0)).toBeGreaterThan(900);
}

/** aided's local API on socket, as another of the owner's devices uses it. */
const daemon = (socket: string) =>
  createClient(LocalService, createConnectTransport({ baseUrl: "http://aided", httpVersion: "1.1", nodeOptions: { socketPath: socket } }));

/** Sets up fresh's server from its own daemon, as the owner's first device. */
async function setUp(fresh: (typeof fixture.fresh)[number], label: string, localMachineName = "") {
  const d = daemon(fresh.socket);
  await d.setup({ relayUrl: fresh.relayURL, setupCode: fresh.claimCode, account, password, label, platform: "macos", localMachineName });
  return d;
}

/** An assistant logging in with the agent CLI, in a home of its own: its exit code, and its home for what it runs next. */
function agentLogin(server: string, name: string) {
  const env = { ...process.env, AIDE_HOME: fs.mkdtempSync(path.join(os.tmpdir(), "hmagent-")), AIDE_PASSWORD: password };
  const login = spawn(fixture.cli, ["auth", "login", server, "--account", account, "--name", name], { env });
  let out = "";
  login.stdout.on("data", (b: Buffer) => (out += b));
  login.stderr.on("data", (b: Buffer) => (out += b));
  return { env, exit: new Promise<number | null>((r) => login.on("exit", r)), output: () => out };
}

/** The welcome's Log In, then the server, the account and the password, and Continue. */
async function logIn(page: Page, server: string, pw = password) {
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.getByLabel("服务器").fill(server);
  await page.getByLabel("账号").fill(account);
  await page.getByLabel("密码").fill(pw);
  await page.getByRole("button", { name: "继续" }).click();
  await expect(page.getByRole("heading", { name: "这台电脑在 Aide 里做什么？" })).toBeVisible();
}

/** The welcome's Set Up a New Server, then its address, its setup code, the account and the password, and Continue. */
async function setUpHere(page: Page, fresh: (typeof fixture.fresh)[number]) {
  await page.getByRole("button", { name: "设置新的服务器…" }).click();
  await page.getByLabel("服务器地址").fill(fresh.relayURL);
  await page.getByLabel("设置码").fill(fresh.claimCode);
  await page.getByLabel("账号").fill(account);
  await page.getByLabel("密码（至少 8 位）").fill(password);
  await page.getByRole("button", { name: "继续" }).click();
  await expect(page.getByRole("heading", { name: "这台电脑在 Aide 里做什么？" })).toBeVisible();
}

test("the window opens at its last size, loading in the middle until aided answers", async () => {
  const none = () => path.join(fs.mkdtempSync(path.join(os.tmpdir(), "hmsock-")), "none.sock");
  const width = (app: ElectronApplication) => app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0]?.getBounds().width ?? 0);
  // A first start: the main window's size, as most starts are.
  const profile = fs.mkdtempSync(path.join(os.tmpdir(), "hmdesk-"));
  const { app, page } = await launch(none(), { profile });
  await expect(page.getByText("正在连接后台服务…")).toBeVisible();
  expect(await width(app)).toBeGreaterThan(900);
  await snap(page, "start-01-connecting");
  // The explanation only once it has kept not answering.
  await expect(page.getByText("Aide 的后台服务没有响应", { exact: false })).toBeHidden();
  await expect(page.getByText("Aide 的后台服务没有响应", { exact: false })).toBeVisible({ timeout: 15_000 });
  expect(await width(app)).toBeGreaterThan(900);
  await snap(page, "start-02-not-answering");
  await app.close();

  // The first run makes it compact, and it opens so next time.
  const first = await launch(fixture.fresh[0].socket, { profile });
  await expect(first.page.getByRole("heading", { name: "让 Agent 安全地互相连接" })).toBeVisible();
  await expect.poll(() => width(first.app)).toBe(720);
  await first.app.close();
  const again = await launch(none(), { profile });
  await expect(again.page.getByText("正在连接后台服务…")).toBeVisible();
  expect(await width(again.app)).toBe(720);
  await again.app.close();
});

test("a new server set up with its setup code: welcome, authorize, set up this Mac, add a project, connect later", async () => {
  const fresh = fixture.fresh[0];
  const { app, page } = await launch(fresh.socket);
  await expect(page.getByRole("heading", { name: "让 Agent 安全地互相连接" })).toBeVisible();
  // Logging in first, then setting a new server up. No official service: no link to its site.
  await expect(page.getByRole("button", { name: "登录", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "设置新的服务器…" })).toBeVisible();
  await expect(page.getByRole("button", { name: "aide.io", exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("heading", { name: "登录你的 Aide" })).toBeVisible();
  // The server is asked for with the account and password, empty: Continue waits for all three.
  await expect(page.getByLabel("服务器")).toHaveValue("");
  await expect(page.getByRole("button", { name: "继续" })).toBeDisabled();
  await page.getByLabel("账号").fill(account);
  await page.getByLabel("密码").fill(password);
  await expect(page.getByRole("button", { name: "继续" })).toBeDisabled();
  await page.getByLabel("服务器").fill("relay.example.com");
  await expect(page.getByRole("button", { name: "继续" })).toBeEnabled();
  await page.getByLabel("服务器").fill("  ");
  await expect(page.getByRole("button", { name: "继续" })).toBeDisabled();
  await page.getByRole("button", { name: "返回" }).click();
  await expect(page.getByText("任务内容端到端加密，服务器看不到内容。")).toBeVisible();
  await expect.poll(() => app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0]?.getBounds().width)).toBe(720);
  await snap(page, "first-01-welcome");

  await page.getByRole("button", { name: "设置新的服务器…" }).click();
  await expect(page.getByRole("heading", { name: "设置新的服务器" })).toBeVisible();
  await expect(page.getByRole("button", { name: "继续" })).toBeDisabled();
  // A password of fewer than 8 characters can't be the account's.
  await page.getByLabel("服务器地址").fill(fresh.relayURL);
  await page.getByLabel("设置码").fill(fresh.claimCode);
  await page.getByLabel("账号").fill(account);
  await page.getByLabel("密码（至少 8 位）").fill("short");
  await expect(page.getByRole("button", { name: "继续" })).toBeDisabled();
  await page.getByLabel("密码（至少 8 位）").fill(password);
  await expect(page.getByRole("button", { name: "继续" })).toBeEnabled();

  // A server reached by its IP address is plain HTTP: said before going on.
  // Not for a domain name, or this Mac.
  const plain = page.getByRole("note").filter({ hasText: "未加密（HTTP）" });
  await page.getByLabel("服务器地址").fill("203.0.113.7:8080");
  await expect(plain).toBeVisible();
  await expect(plain).toContainText("设置码、登录时的密码");
  await page.getByLabel("服务器地址").fill("relay.example.com");
  await expect(plain).toHaveCount(0);
  await page.getByLabel("服务器地址").fill("http://127.0.0.1:8080");
  await expect(plain).toHaveCount(0);

  // A network that blocks the server (a company proxy answering 403):
  // said with what to do, and the way back still works.
  const proxy = http.createServer((_, res) => res.writeHead(403, { "content-type": "text/html" }).end("<html>Blocked by policy</html>"));
  await new Promise<void>((r) => proxy.listen(0, "127.0.0.1", r));
  const blocked = `127.0.0.1:${(proxy.address() as AddressInfo).port}`;
  await page.getByLabel("服务器地址").fill(`http://${blocked}`);
  await page.getByRole("button", { name: "继续" }).click();
  await page.getByRole("button", { name: "继续" }).click();
  const trouble = page.getByRole("alertdialog", { name: "连不上 Aide 服务器" });
  await expect(trouble).toHaveAccessibleDescription(`当前网络可能拦住了 ${blocked}，公司网络和代理常会这样。换个网络（比如手机热点）再试，这台 Mac 上还什么都没改。`);
  await expect(trouble.getByText("403 Forbidden")).toHaveCount(0); // the raw cause, behind 详情
  await trouble.getByRole("button", { name: "详情" }).click();
  await expect(trouble.getByText("403 Forbidden")).toBeVisible();
  await snap(page, "first-03b-unreachable");
  await trouble.getByRole("button", { name: "好" }).click();
  await expect(trouble).toHaveCount(0);
  await expect(page.getByRole("alert")).toHaveCount(0); // nothing left in the footer
  await expect(page.getByRole("button", { name: "继续" })).toBeEnabled();
  proxy.close();
  expect((await daemon(fresh.socket).getStatus({})).initialized).toBe(false);
  await page.getByRole("button", { name: "取消" }).click();

  // Cancel went back to the form, empty again but for the server last typed.
  await expect(page.getByRole("heading", { name: "设置新的服务器" })).toBeVisible();
  await page.getByRole("button", { name: "返回" }).click();
  await setUpHere(page, fresh);
  await snap(page, "first-02-relay");

  // Nothing is set up before 继续 here. Setting a server up makes this computer one of the owner's devices: no switch.
  await expect(page.getByRole("switch", { name: "你的设备" })).toHaveCount(0);
  await expect(page.getByRole("list", { name: "设置步骤" })).toHaveCount(0);
  await expect(page.getByRole("switch", { name: "安全可信地共享本机 agent" })).toBeChecked();
  await expect(page.getByText("不读取 API Key 和 Token", { exact: false }).first()).toBeVisible();
  await expect(page.getByRole("switch", { name: "有事时提醒我" })).toBeChecked();
  await snap(page, "first-03-authorize");
  await page.getByRole("button", { name: "继续" }).click();

  // Set up: this Mac, as its own machine.
  // Opening the page checks the agents (the stand-ins wait for this test): until the first are in, it says it is looking for them.
  const release = (agent: string) => fs.writeFileSync(path.join(fresh.gate!, agent), "");
  const searching = page.getByRole("status").filter({ has: page.getByRole("heading", { name: "正在查找本机上的编码 Agent" }) });
  await expect(searching).toBeVisible({ timeout: 30_000 });
  await expect(searching.locator(".ag-sweep > span")).toHaveCount(16);
  await expect(page.getByRole("button", { name: "稍后设置" })).toBeEnabled();
  await snap(page, "first-04-searching");
  const st = await daemon(fresh.socket).getStatus({});
  expect(st.initialized).toBe(true);
  expect(st.machineOnly).toBe(false);
  release("codex");
  release("gemini");
  release("antigravity");
  // Every coding agent Aide supports: the installed ones with their state, the rest greyed.
  await expect(page.getByRole("heading", { name: "检查和管理可用的 Agent" })).toBeVisible({ timeout: 30_000 });
  await expect(searching).toHaveCount(0);
  await expect(page.getByRole("list", { name: "设置步骤" })).toHaveCount(0); // no way back, so no steps
  const agents = page.getByRole("list", { name: "编码 Agent" });
  const claude = agents.getByRole("listitem", { name: "Claude Code" });
  // Claude Code still being checked shows so while the others are in, not as missing.
  await expect(claude).toContainText("正在检查…");
  await expect(page.getByRole("list", { name: "没有安装的 Agent" }).getByRole("listitem", { name: "Claude Code" })).toHaveCount(0);
  await snap(page, "first-04-checking");
  release("claude");
  await expect(claude).toContainText("2.1.4", { timeout: 30_000 });
  await expect(claude).toContainText("已登录");
  const gemini = agents.getByRole("listitem", { name: "Gemini CLI" });
  await expect(gemini).toContainText("已登录");
  const codex = agents.getByRole("listitem", { name: "Codex" });
  await expect(codex).toContainText("0.48.0");
  await expect(codex).toContainText("还没登录");
  await expect(page.getByRole("list", { name: "没有安装的 Agent" }).getByRole("listitem", { name: "Cursor" })).toBeVisible();
  await expect(agents.getByRole("listitem", { name: "Cursor" })).toHaveCount(0);
  // Each opens its own page, as in settings; Codex's says how to log in. The steps' buttons stay below.
  const agentPage = (name: string) => page.getByRole("region", { name });
  const back = () => page.getByRole("button", { name: "所有 Agent" }).click();
  const antigravity = agents.getByRole("listitem", { name: "Antigravity" });
  await expect(antigravity).toContainText("还没登录");
  await antigravity.getByRole("button", { name: /^Antigravity/ }).click();
  await expect(agentPage("Antigravity").getByText("aide agent login antigravity")).toBeVisible();
  await expect(agentPage("Antigravity").getByRole("button", { name: "在终端运行" })).toBeVisible();
  await back();
  await codex.getByRole("button", { name: /^Codex/ }).click();
  await expect(agentPage("Codex").getByText("codex login")).toBeVisible();
  await expect(agentPage("Codex").getByRole("button", { name: "在终端运行" })).toBeVisible();
  await expect(page.getByRole("button", { name: "下一步：连接你的 Agent" })).toBeDisabled();
  await snap(page, "first-04-agent");
  // Drawn as a text link, it takes the pointing hand, as links do.
  expect(await page.getByRole("button", { name: "所有 Agent" }).evaluate((el) => getComputedStyle(el).cursor)).toBe("pointer");
  await back();
  await page.waitForTimeout(1500); // the agents around the mark have come in
  await expect(page.getByText("绝不获取 Agent 的任何令牌和密钥，只像你自己在命令行里一样使用它们。")).toBeVisible();
  await snap(page, "first-04-prepare");

  // Claude Code gets its first project: a git repository works in its own worktree per session, unless in place.
  const repo = gitRepo("my-app");
  await pickAnswers(app, repo);
  await claude.getByRole("button", { name: /^Claude Code/ }).click();
  await agentPage("Claude Code").getByRole("button", { name: "添加项目…" }).click();
  const project = agentPage("Claude Code").getByRole("group", { name: "my-app" });
  await expect(project).toContainText(repo);
  await expect(project).toContainText("独立 worktree");
  await answerMenu(app, "inPlace");
  await project.getByRole("button", { name: "更多" }).click();
  await expect(project).toContainText("原地");
  await back();
  await expect(claude).toContainText("1 个项目");
  // It is Claude Code's only; the same folder added to Gemini CLI is the same project, for both.
  await expect(gemini).toContainText("还没有项目");
  await gemini.getByRole("button", { name: /^Gemini CLI/ }).click();
  await agentPage("Gemini CLI").getByRole("button", { name: "添加项目…" }).click();
  await expect(agentPage("Gemini CLI").getByRole("group", { name: "my-app" })).toBeVisible();
  await snap(page, "first-05-project");
  // Taken from Claude Code, it stays Gemini CLI's; the machine knows.
  await back();
  await claude.getByRole("button", { name: /^Claude Code/ }).click();
  await answerMenu(app, "remove");
  await project.getByRole("button", { name: "更多" }).click();
  await expect(project).toHaveCount(0);
  await back();
  await expect(claude).toContainText("还没有项目");
  await expect(gemini).toContainText("1 个项目");
  const mine = (await daemon(fresh.socket).listProjects({})).projects;
  expect(mine.map((p) => [p.projectId, p.agents, p.workspace])).toEqual([["my-app", ["gemini"], Workspace.IN_PLACE]]);

  await page.getByRole("button", { name: "下一步：连接你的 Agent" }).click();
  // A guide for each coding agent, DSH's first; the assistants on their own tab.
  await expect(page.getByRole("heading", { name: "连接你的 Agent" })).toBeVisible();
  await expect(page.getByRole("tab", { name: "DSH", exact: true })).toHaveAttribute("aria-selected", "true");
  await page.getByRole("tab", { name: "助手" }).click();
  // One tab: the assistants as icons (Grok Bot chosen), what to send right below; only the name in it differs.
  const kinds = page.getByRole("radiogroup", { name: "选择助手" });
  await expect(kinds.getByRole("radio", { name: "Grok Bot" })).toBeChecked();
  for (const name of ["Grok Bot", "Muse", "ChatGPT", "Claude", "OpenClaw", "其他 Agent"]) await expect(kinds.getByRole("radio", { name })).toBeEnabled();
  const msg = page.locator(".msgbox");
  // It logs in with the server, the account and a password the owner fills in: never the password itself.
  await expect(msg).toContainText(`aide auth login ${fresh.relayURL} --account ${account} --name "Grok Bot"`);
  await expect(msg).toContainText("AIDE_PASSWORD='<密码>'");
  await expect(msg).not.toContainText(password);
  await kinds.getByRole("radio", { name: "Muse" }).click();
  await expect(msg).toContainText('--name "Muse"');
  await kinds.getByRole("radio", { name: "其他 Agent" }).click();
  await page.getByLabel("名称").fill("Jarvis");
  await expect(msg).toContainText('--name "Jarvis"');
  // ChatGPT connects by the server's MCP address, which this test's server (MCP off, and as new as the app)
  // gives none of: no step pastes an address that isn't there, and the note says why (not its age, nor HTTPS).
  await kinds.getByRole("radio", { name: "ChatGPT" }).click();
  await expect(page.getByTestId("mcp-note")).toContainText("服务器上的 MCP 没有开启");
  await expect(page.getByTestId("mcp-note")).not.toContainText("HTTPS");
  await expect(page.getByText("把下面的网址", { exact: false })).toHaveCount(0);
  await expect(page.getByText("添加自定义连接器", { exact: false })).toHaveCount(0);
  await expect(msg).toHaveCount(0);
  await kinds.getByRole("radio", { name: "Grok Bot" }).click();
  await expect(page.getByText("它用账号密码登录后，用自己的密钥与你的电脑建立端到端加密直连，服务器看不到任何内容。")).toBeVisible();
  await expect(page.getByText("等待 Grok Bot 登录…")).toBeVisible();
  await snap(page, "first-06-agent");
  await page.getByRole("button", { name: "稍后再连" }).click();
  await expectMainWindow(app, page);
  await app.close();
});

test("a new server's first assistant: it logs in with the account itself, shows as connected, and its first task opens; links from a hosted center are not ours", async () => {
  const fresh = fixture.fresh[1];
  const { app, page } = await launch(fresh.socket);
  await app.evaluate(({ shell }) => {
    const opened: string[] = [];
    Object.assign(globalThis, { opened });
    shell.openExternal = async (url: string) => void opened.push(url);
  });
  const link = (url: string) => app.evaluate(({ app }, u) => app.emit("open-url", { preventDefault() {} }, u), url);
  const browser = () => app.evaluate(() => (globalThis as unknown as { opened: string[] }).opened);

  // A sign-in's link code, or an account's join link, would come from a hosted center: neither is ours.
  await link(`aide://link?code=${fresh.claimCode}`);
  await link("aide://join?owner=o_" + "a".repeat(24));
  await page.waitForTimeout(500);
  await expect(page.getByRole("heading", { name: "让 Agent 安全地互相连接" })).toBeVisible();
  expect(await browser()).toEqual([]);

  await setUpHere(page, fresh);
  await page.getByRole("button", { name: "继续" }).click();
  await expect(page.getByRole("heading", { name: "检查和管理可用的 Agent" })).toBeVisible({ timeout: 30_000 });
  expect(await browser()).toEqual([]);

  // The first task runs on Gemini CLI (the scripted agent), so the project is its.
  await pickAnswers(app, gitRepo("site"));
  const gemini = page.getByRole("list", { name: "编码 Agent" }).getByRole("listitem", { name: "Gemini CLI" });
  await gemini.getByRole("button", { name: /^Gemini CLI/ }).click({ timeout: 30_000 });
  const geminiPage = page.getByRole("region", { name: "Gemini CLI" });
  await geminiPage.getByRole("button", { name: "添加项目…" }).click();
  await expect(geminiPage.getByRole("group", { name: "site" })).toBeVisible();
  await page.getByRole("button", { name: "下一步：连接你的 Agent" }).click();

  // Muse is sent the message, and logs in with it: no request to approve, it shows here as connected.
  await expect(page.getByRole("heading", { name: "连接你的 Agent" })).toBeVisible();
  await page.getByRole("tab", { name: "助手" }).click();
  await page.getByRole("radio", { name: "Muse" }).click();
  const message = await page.locator(".msgbox").innerText();
  const server = /auth login (\S+) --account owner --name "Muse"/.exec(message)?.[1] ?? "";
  expect(server).toBe(fresh.relayURL);
  await snap(page, "first-08-connect");
  const muse = agentLogin(server, "Muse");
  await expect(page.getByText("Muse 已连接")).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText("它现在可以在你所有机器的项目里开始任务", { exact: false })).toBeVisible();
  await expect(page.getByRole("radiogroup", { name: "选择助手" })).toBeHidden();
  expect(await muse.exit, muse.output()).toBe(0);
  // What it may use: every machine, project and coding agent, until signed out.
  const client = (await daemon(fresh.socket).listClients({})).clients.find((c) => c.label === "Muse");
  expect(client?.spec).toMatchObject({ machineIds: ["*"], projectIds: ["*"], agents: ["*"] });
  await snap(page, "first-09-connected");

  // Then the first task, with the assistant that connected.
  await page.getByRole("button", { name: "进入 Aide" }).click();
  await expect(page.getByRole("heading", { name: "「Muse」已连接" })).toBeVisible({ timeout: 30_000 });
  // A hello that is sure to run: a coding agent it may use, in its project.
  await expect(page.getByText(/^用 Aide 在 site 项目里，让 (Claude Code|Codex|Gemini CLI) 跟我打个招呼。$/)).toBeVisible();
  await expect(page.getByRole("button", { name: "进入 Aide" })).toBeVisible();
  await snap(page, "first-11-first-task");

  // Its first session: the window opens on it.
  execFileSync(fixture.cli, ["session", "start", "--project", "site", "--agent", "gemini", "--title", "检查 README", "--prompt", "say 好的"], { env: muse.env });
  await expectMainWindow(app, page);
  await expect(page.getByText("检查 README").first()).toBeVisible();
  await snap(page, "first-12-main");
  // The overview has no setup checklist.
  await page.getByRole("button", { name: /^概览/ }).click();
  await expect(page.getByRole("heading", { name: "概览", level: 1 })).toBeVisible();
  await expect(page.getByRole("region", { name: "完成设置" })).toHaveCount(0);
  // Once the first session is done, 正在运行 rests: the mark, what runs (nothing), and what will show there.
  const working = page.getByRole("region", { name: "正在运行" });
  await expect(working.getByRole("status")).toContainText("当前没有运行中的任务", { timeout: 30_000 });
  await expect(working.getByRole("status")).toContainText("Agent 开始工作后，任务会显示在这里，链路图也会同步点亮。");
  await expect(working.locator(".hm-mark")).toBeVisible();
  // The sidebar's 进行中 stays, saying it is empty for now; the session is under 已结束.
  const sessions = page.getByRole("list", { name: "任务" });
  await expect(sessions.getByRole("group", { name: "进行中" })).toHaveText(/^进行中\s*暂时没有$/);
  await expect(sessions.getByRole("group", { name: "已结束" })).toContainText("检查 README");
  await snap(page, "first-13-resting");
  // With nothing live, hovering an agent marks what it can reach on the map, which doesn't grow.
  const map = page.getByRole("region", { name: "链路" });
  await page.mouse.move(0, 0);
  const before = (await map.boundingBox())!.height;
  await map.getByRole("button", { name: "Muse", exact: true }).hover();
  await expect(map.locator(".lm-edge.scope").first()).toBeAttached();
  expect((await map.boundingBox())!.height).toBe(before);
  await app.close();
});

test("a Mac with no coding agent: set up later, and an assistant that logs in may use every coding agent, including ones installed later", async () => {
  const fresh = fixture.fresh[8];
  const { app, page } = await launch(fresh.socket);
  await setUpHere(page, fresh);
  await page.getByRole("button", { name: "继续" }).click();

  // None found: Claude Code is among the missing ones; set up later.
  await expect(page.getByRole("heading", { name: "检查和管理可用的 Agent" })).toBeVisible({ timeout: 30_000 });
  await expect(page.getByRole("list", { name: "没有安装的 Agent" }).getByRole("listitem", { name: "Claude Code" })).toBeVisible();
  await expect(page.getByRole("list", { name: "编码 Agent" }).getByRole("listitem")).toHaveCount(0);
  await page.getByRole("button", { name: "稍后设置" }).click();

  await expect(page.getByRole("heading", { name: "连接你的 Agent" })).toBeVisible();
  await page.getByRole("tab", { name: "助手" }).click();
  const login = agentLogin(fresh.relayURL, "Muse");
  await expect(page.getByText("Muse 已连接")).toBeVisible({ timeout: 30_000 });
  expect(await login.exit, login.output()).toBe(0);
  const muse = (await daemon(fresh.socket).listClients({})).clients.find((c) => c.label === "Muse");
  expect(muse?.spec?.agents).toEqual(["*"]);
  await snap(page, "first-13-no-agents");
  await app.close();
});

test("logging in to a server set up already: a wrong password said, then this computer one of the owner's devices; signed out from the other, it can log in again", async () => {
  // The owner's first device, and this computer logging in to its server.
  const first = await setUp(fixture.fresh[2], "first-mac");
  const server = fixture.fresh[2].relayURL;
  const { app, page } = await launch(fixture.fresh[3].socket);
  await logIn(page, server, "not the password");
  // One of the owner's devices unless turned off; sharing its agents and reminders on.
  await expect(page.getByRole("switch", { name: "你的设备" })).toBeChecked();
  await expect(page.getByText("在这里查看和管理你的所有机器、agent 和任务。")).toBeVisible();
  await expect(page.getByRole("switch", { name: "安全可信地共享本机 agent" })).toBeChecked();
  await expect(page.getByRole("switch", { name: "有事时提醒我" })).toBeChecked();
  await page.getByRole("button", { name: "继续" }).click();
  const trouble = page.getByRole("alertdialog", { name: "没能登录" });
  await expect(trouble).toBeVisible({ timeout: 30_000 });
  await snap(page, "first-login-wrong-password");
  await trouble.getByRole("button", { name: "好" }).click();
  expect((await daemon(fixture.fresh[3].socket).getStatus({})).initialized).toBe(false);

  // Back to the form: the server kept, the password typed again.
  await page.getByRole("button", { name: "取消" }).click();
  await expect(page.getByRole("heading", { name: "登录你的 Aide" })).toBeVisible();
  await expect(page.getByLabel("服务器")).toHaveValue(server);
  await page.getByLabel("账号").fill(account);
  await page.getByLabel("密码").fill(password);
  await page.getByRole("button", { name: "继续" }).click();
  await page.getByRole("button", { name: "继续" }).click();

  // One of the owner's devices now, and (as allowed) a machine too: no other device approved it, nothing to compare.
  await expect(page.getByRole("heading", { name: "检查和管理可用的 Agent" })).toBeVisible({ timeout: 30_000 });
  const devices = async () => (await first.listDevices({})).devices;
  expect((await devices()).map((d) => d.label)).toHaveLength(2);
  const joined = (await devices()).find((d) => !d.thisDevice)!;
  expect(joined.machineId).not.toBe("");
  const st = await daemon(fixture.fresh[3].socket).getStatus({});
  expect(st.ownerId).toBe((await first.getStatus({})).ownerId);
  expect(st.machineOnly).toBe(false);
  await page.getByRole("button", { name: "稍后设置" }).click();
  await expect(page.getByRole("heading", { name: "连接你的 Agent" })).toBeVisible();
  await page.getByRole("button", { name: "进入 Aide" }).click();
  await expectMainWindow(app, page);

  // The first device signs it out: this one says so, and can log in again.
  await first.removeDevice({ keyId: joined.keyId });
  await expect(page.getByRole("heading", { name: "这台电脑已被登出" })).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText("你在另一台设备上把它登出了，或者账号已被重置。重新登录即可再次使用。")).toBeVisible();
  await snap(page, "first-14-removed");
  await page.getByRole("button", { name: "重新登录" }).click();
  await expect(page.getByRole("heading", { name: "让 Agent 安全地互相连接" })).toBeVisible();
  await app.close();
});

test("a work Mac logs in as a machine only (你的设备 off): its own projects and sessions, an assistant's card; then one of the owner's devices by logging in again", async () => {
  const home = await setUp(fixture.fresh[4], "home-mac");
  const server = fixture.fresh[4].relayURL;
  const workMac = daemon(fixture.fresh[5].socket);
  const { app, page } = await launch(fixture.fresh[5].socket);
  await logIn(page, server);

  // Not one of the owner's devices: then sharing its agents is what it is for.
  await page.getByRole("switch", { name: "你的设备" }).click();
  await expect(page.getByRole("switch", { name: "你的设备" })).not.toBeChecked();
  await expect(page.getByText("关闭：这台电脑只作为执行机器，只运行任务，看不到你的其他机器。适合公司的电脑。")).toBeVisible();
  await expect(page.getByRole("switch", { name: "安全可信地共享本机 agent" })).toHaveCount(0);
  await expect(page.getByText("必需")).toBeVisible();
  await snap(page, "machine-01-authorize");
  await page.getByRole("button", { name: "继续" }).click();

  // The main window at once: its sessions in the sidebar, and the overview, its links this Mac's own; nothing to approve.
  await expectMainWindow(app, page);
  const st = await workMac.getStatus({});
  expect(st.machineOnly).toBe(true);
  expect(st.initialized).toBe(false);
  expect((await home.listDevices({})).devices.map((d) => d.label)).toEqual(["home-mac"]);
  await expect(page.getByRole("heading", { name: "概览" })).toBeVisible({ timeout: 30_000 });
  const map = page.getByRole("region", { name: "链路" });
  await expect(map).toContainText("这台 Mac");
  // Online: connected to the relay, which the agent's session needs.
  await expect(map).toContainText("接新任务", { timeout: 30_000 });
  await expect(map.getByRole("button", { name: "+ 连接更多" })).toHaveCount(0);
  await expect(page.getByRole("region", { name: "需要你处理" })).toHaveCount(0);

  // Its settings, from the account menu, are this Mac's: its coding agents,
  // their models and projects, and its account; no access to grant, no other machine.
  await page.getByRole("button", { name: "账号与设置" }).click();
  await expect(page.getByRole("menu", { name: "账号与设置" }).getByRole("menuitem")).toHaveText([/^设置…/, "账号…"]);
  const [settings] = await Promise.all([app.waitForEvent("window"), page.getByRole("menuitem", { name: /^设置/ }).click()]);
  await settings.waitForLoadState();
  const nav = settings.getByRole("navigation", { name: "设置" });
  await expect(nav.getByRole("button")).toHaveText(["通用", "这台 Mac", "账号"]);
  await nav.getByRole("button", { name: "这台 Mac" }).click();
  const panel = settings.getByRole("main", { name: "这台 Mac" });
  await expect(panel.getByRole("switch", { name: "安全可信地共享本机 agent" })).toHaveCount(0);
  await expect(panel.getByRole("button", { name: "添加机器…" })).toHaveCount(0);
  await expect(panel.getByRole("button", { name: "移除…" })).toHaveCount(0);
  await snap(settings, "machine-05-settings");
  const agents = panel.getByRole("list", { name: "编码 Agent" });
  const codex = agents.getByRole("listitem", { name: "Codex" });
  await codex.getByRole("button", { name: /^Codex/ }).click({ timeout: 30_000 });
  const model = panel.getByRole("region", { name: "Codex" }).getByRole("group", { name: "模型" });
  await expect(model).toContainText("跟随 Codex（GPT-Fake-Big）", { timeout: 30_000 });
  await answerMenu(app, "gpt-fake-small");
  await model.getByRole("button").click();
  await expect(model).toHaveText("模型GPT-Fake-Small");
  const codexDefaults = async () => (await workMac.listMachines({})).machines[0]?.status?.agents.find((a) => a.name === "codex")?.defaults?.model;
  await expect.poll(codexDefaults).toBe("gpt-fake-small");
  await snap(settings, "machine-06-agent");
  // ‹ goes back to this Mac, which says what is set; Gemini CLI gets a project.
  await settings.getByRole("button", { name: "返回这台 Mac" }).click();
  await expect(codex).toContainText("GPT-Fake-Small");
  await expect(settings.getByRole("button", { name: /^返回/ })).toHaveCount(0);
  await agents.getByRole("listitem", { name: "Gemini CLI" }).getByRole("button", { name: /^Gemini CLI/ }).click();
  await pickAnswers(app, gitRepo("billing"));
  await panel.getByRole("region", { name: "Gemini CLI" }).getByRole("button", { name: "添加项目…" }).click();
  await expect(panel.getByRole("region", { name: "Gemini CLI" }).getByRole("group", { name: "billing" })).toBeVisible({ timeout: 30_000 });
  await expect(map.getByRole("button", { name: "billing" })).toBeVisible({ timeout: 30_000 });
  // Its account: whose it is and where it connects; from here it can leave, or become one of the owner's devices.
  await nav.getByRole("button", { name: "账号" }).click();
  const accountPanel = settings.getByRole("main", { name: "账号" });
  await expect(accountPanel).toContainText("这台电脑是你账号下的执行机器：它运行 Agent 的任务，看不到你的其他机器。");
  await expect(accountPanel.getByRole("list", { name: "你的设备" })).toHaveCount(0);
  await expect(accountPanel.getByRole("button", { name: "离开你的 Aide…" })).toBeVisible();
  await snap(settings, "machine-07-account");
  await settings.close();

  // An assistant of the owner logs in: the map has it, with no task yet.
  const grok = agentLogin(server, "Grok Bot");
  expect(await grok.exit, grok.output()).toBe(0);
  const agent = map.getByRole("button", { name: "Grok Bot", exact: true });
  await expect(agent).toContainText("尚无任务", { timeout: 30_000 });
  // Nothing has run here yet: 正在运行 rests, with no prompt to send.
  const working = page.getByRole("region", { name: "正在运行" });
  await expect(working.getByRole("status")).toContainText("当前没有运行中的任务");
  await expect(working).not.toContainText("发一句试试");
  // No session at all: the sidebar says what will show there (a machine alone has no Her to ask).
  await expect(page.getByRole("list", { name: "任务" })).toHaveText(/^还没有任务\s*Agent 连接后，发起的任务会显示在这里。$/);
  await snap(page, "machine-03-window");

  // It runs a session there: the work Mac lists it, the map runs it from the
  // agent to the project, and the agent's card says what it may do here.
  execFileSync(fixture.cli, ["session", "start", "--project", "billing", "--agent", "gemini", "--title", "修账单导出", "--prompt", "shell sleep 1800"], { env: grok.env });
  const row = (await sessionList(page)).getByRole("listitem").filter({ hasText: "修账单导出" });
  await expect(row).toBeVisible({ timeout: 30_000 });
  await navTo(page, /^概览/);
  await expect(agent).not.toContainText("尚无任务", { timeout: 30_000 });
  await expect(page.getByRole("button", { name: "管理授权…" })).toHaveCount(0);
  // Resting on it shows its card, over the map, which keeps its height.
  const height = await mapHeight(map);
  await agent.hover();
  const card = page.getByRole("dialog", { name: "Grok Bot" });
  await expect(card).toBeVisible();
  expect((await map.boundingBox())!.height).toBe(height);
  await page.mouse.move(0, 0);
  await expect(card).toHaveCount(0);
  // Clicked, it stays, and 正在干活 keeps only its sessions.
  await agent.click();
  await expect(card).toContainText("个人 agent · 自己保管密钥");
  await expect(card).toContainText("授权给了它完全权限");
  await expect(card).toContainText("在这里能用所有项目");
  await expect(card).toContainText("编码 agent所有编码 agent");
  await expect(card).toContainText("有效期不会到期");
  await expect(card).toContainText("在这台 Mac今天 1 个任务 · 最近一次");
  await expect(page.getByRole("status").filter({ hasText: "Grok Bot 发起的任务" })).toBeVisible();
  await snap(page, "machine-08-card");
  await page.keyboard.press("Escape");
  await expect(card).toHaveCount(0);

  await (await sessionList(page)).getByRole("listitem").filter({ hasText: "修账单导出" }).click();
  await expect(page.getByRole("heading", { name: "修账单导出" })).toBeVisible();
  await expect(page.getByRole("log", { name: "对话" })).toContainText("sleep 1800", { timeout: 30_000 });
  await snap(page, "machine-04-session");
  await page.getByRole("button", { name: /打断/ }).click();
  await expect(page.getByRole("status")).toContainText("已打断「修账单导出」");

  // One of the owner's devices too: the account and password again, on the server it uses; its machine stays.
  const machineId = st.localMachineId;
  const [again] = await Promise.all([app.waitForEvent("window"), page.evaluate(() => window.aide?.openSettings("account"))]);
  await again.waitForLoadState();
  await Promise.all([again.waitForEvent("close"), again.getByRole("main", { name: "账号" }).getByRole("button", { name: "让这台电脑也成为你的设备…" }).click()]);
  await expect(page.getByRole("heading", { name: "登录你的 Aide" })).toBeVisible({ timeout: 30_000 });
  await expect(page.getByLabel("服务器")).toHaveCount(0); // the server it uses
  await page.getByLabel("账号").fill(account);
  await page.getByLabel("密码").fill(password);
  await snap(page, "machine-09-upgrade");
  await page.getByRole("button", { name: "继续" }).click();
  // Only this Mac is left to look at: Done, not the connect step.
  await expect(page.getByRole("heading", { name: "检查和管理可用的 Agent" })).toBeVisible({ timeout: 30_000 });
  await expect(page.getByRole("button", { name: "完成" })).toBeVisible();
  await expect(page.getByRole("button", { name: "下一步：连接你的 Agent" })).toHaveCount(0);
  const upgraded = await workMac.getStatus({});
  expect(upgraded).toMatchObject({ initialized: true, machineOnly: false, localMachineId: machineId });
  await expect.poll(async () => (await home.listDevices({})).devices.find((d) => !d.thisDevice)?.machineId, { timeout: 30_000 }).toBe(machineId);
  await page.getByRole("button", { name: "稍后设置" }).click();
  await expectMainWindow(app, page);
  await page.getByRole("button", { name: "账号与设置" }).click();
  await expect(page.getByRole("menu", { name: "账号与设置" }).getByRole("menuitem", { name: /^添加机器/ })).toBeVisible();
  await app.close();
});

test("Settings › Account: a new device logging in shows in Add Device; a new password signs the other devices and agents out", async () => {
  const home = await setUp(fixture.fresh[6], "home-mac");
  const server = fixture.fresh[6].relayURL;
  const { app, page } = await launch(fixture.fresh[6].socket);
  await expectMainWindow(app, page);
  const [settings] = await Promise.all([app.waitForEvent("window"), page.evaluate(() => window.aide?.openSettings("account"))]);
  await settings.waitForLoadState();
  const panel = settings.getByRole("main", { name: "账号与设备" });
  const devices = panel.getByRole("list", { name: "你的设备" });
  await expect(devices.getByRole("listitem")).toHaveCount(1);
  await expect(devices).toContainText("home-mac 这台 Mac");
  await expect(devices.getByRole("button", { name: "登出" })).toHaveCount(0); // never itself

  // Add Device says where and with what; another Mac logs in, and the sheet says so.
  await devices.getByRole("button", { name: "添加设备…" }).click();
  const sheet = settings.getByRole("dialog");
  await expect(sheet.getByRole("heading", { name: "添加设备" })).toBeVisible();
  await expect(sheet).toContainText(server);
  await expect(sheet).toContainText(account);
  await expect(sheet).toContainText("等新设备登录…");
  const second = daemon(fixture.fresh[7].socket);
  await second.logIn({ relayUrl: server, account, password, label: "second-mac", platform: "macos" });
  await expect(sheet).toContainText("「second-mac」已登录", { timeout: 30_000 });
  await snap(settings, "settings-device-added");
  await sheet.getByRole("button", { name: "完成" }).click();
  await expect(sheet).toBeHidden();
  await expect(devices.getByRole("listitem")).toHaveCount(2);
  await expect(devices.getByRole("listitem").filter({ hasText: "second-mac" }).getByRole("button", { name: "登出" })).toBeVisible();
  // An agent logged in too.
  const agent = agentLogin(server, "Grok Bot");
  expect(await agent.exit, agent.output()).toBe(0);

  // The account and its password, changed with the current one; the new one typed twice.
  await expect(panel).toContainText(`账号 ${account}`);
  await expect(panel).toContainText(`新的电脑、手机、机器或 agent 输入服务器 ${server}、这个账号和密码即可登录，不需要批准。`);
  await panel.getByRole("button", { name: "修改账号或密码…" }).click();
  await expect(panel.getByLabel("账号", { exact: true })).toHaveValue(account);
  await expect(panel.getByText("改密码会登出你的其他设备和所有 agent", { exact: false })).toBeVisible();
  const save = panel.getByRole("button", { name: "保存" });
  await expect(save).toBeDisabled(); // the current password first
  const next = "a newer battery staple";
  await panel.getByLabel("当前密码").fill(password);
  await panel.getByLabel("新密码（不改就留空）").fill(next);
  await panel.getByLabel("再输一次").fill(next + "!");
  await save.click();
  await expect(panel.getByText("两次输入的新密码不一致。")).toBeVisible();
  await panel.getByLabel("再输一次").fill(next);
  await snap(settings, "settings-change-password");
  await save.click();
  // The other device and the agent signed out, not this one nor its machine.
  await expect(settings.getByRole("status").filter({ hasText: "密码已修改，已登出 2 个设备和 agent" })).toBeVisible({ timeout: 30_000 });
  await expect.poll(async () => (await second.getStatus({})).removed, { timeout: 30_000 }).toBe(true);
  await expect(devices.getByRole("listitem")).toHaveCount(1);
  expect((await home.getStatus({})).removed).toBe(false);
  // The old password logs nothing in any more; the new one does.
  await second.leave({});
  await expect(second.logIn({ relayUrl: server, account, password, label: "second-mac", platform: "macos" })).rejects.toThrow();
  await second.logIn({ relayUrl: server, account, password: next, label: "second-mac", platform: "macos" });
  await expect(devices.getByRole("listitem")).toHaveCount(2, { timeout: 30_000 });
  await app.close();
});

test("erasing everything: what it erases, the server's address typed, then this Mac's first run says what was done", async () => {
  const fresh = fixture.fresh[9];
  const mac = await setUp(fresh, "erase-mac", "erase-mac");
  const host = new URL(fresh.relayURL).host;
  const profile = fs.mkdtempSync(path.join(os.tmpdir(), "hmdesk-"));
  const { app, page } = await launch(fresh.socket, { profile });
  await expectMainWindow(app, page);
  const [settings] = await Promise.all([app.waitForEvent("window"), page.evaluate(() => window.aide?.openSettings("account"))]);
  await settings.waitForLoadState();
  await settings.getByRole("main", { name: "账号与设备" }).getByRole("button", { name: "全部清空…" }).click();

  // What it erases, by name (this Mac's machine is this Mac), and what stays.
  const sheet = settings.getByRole("dialog", { name: "全部清空 Aide？" });
  await expect(sheet).toContainText("erase-mac 这台 Mac");
  await expect(sheet).toContainText(`自己的服务器 · ${host}`);
  await expect(sheet).not.toContainText("解除配对");
  await expect(sheet).toContainText("有未提交改动的 worktree：挪到一边保留");
  // Escape doesn't close it; the button waits for the server's address.
  await settings.keyboard.press("Escape");
  await expect(sheet).toBeVisible();
  const erase = sheet.getByRole("button", { name: "全部清空", exact: true });
  await expect(erase).toBeDisabled();
  await sheet.getByLabel(/清空后不能撤销/).fill(host.slice(0, -1));
  await expect(erase).toBeDisabled();
  await sheet.getByLabel(/清空后不能撤销/).fill(` ${host.toUpperCase()} `);
  await snap(settings, "settings-erase");
  // This Mac is the only one, so it erases itself at once.
  await Promise.all([settings.waitForEvent("close"), erase.click()]);

  // The first run, saying once what was done.
  await expect(page.getByRole("heading", { name: "让 Agent 安全地互相连接" })).toBeVisible({ timeout: 30_000 });
  const note = page.getByRole("status").filter({ hasText: "Aide 已全部清空" });
  await expect(note).toBeVisible();
  await snap(page, "first-erased");
  const status = await mac.getStatus({});
  expect(status.initialized).toBe(false);
  expect(status.erased?.eraseId).toBeTruthy();
  await note.getByRole("button", { name: "关闭" }).click();
  await expect(note).toBeHidden();
  await app.close();

  // Closed, it stays closed.
  const again = await launch(fresh.socket, { profile });
  await expect(again.page.getByRole("heading", { name: "让 Agent 安全地互相连接" })).toBeVisible();
  await expect(again.page.getByText("Aide 已全部清空")).toHaveCount(0);
  await again.app.close();
});

test("another device's machine is marked in 设置 › 机器 and signed out with the device in 账号与设备", async () => {
  // This Mac's owner, and a PC that logs in as one of the owner's devices with its own machine.
  const first = await setUp(fixture.fresh[10], "first-mac");
  const pc = daemon(fixture.fresh[11].socket);
  await pc.logIn({ relayUrl: fixture.fresh[10].relayURL, account, password, label: "work-pc", platform: "windows", localMachineName: "work-pc" });
  await expect.poll(async () => (await first.listDevices({})).devices.find((d) => d.label === "work-pc")?.machineId ?? "", { timeout: 30_000 }).not.toBe("");

  const { app, page } = await launch(fixture.fresh[10].socket);
  const [settings] = await Promise.all([app.waitForEvent("window"), page.evaluate(() => window.aide?.openSettings("machines"))]);
  const row = settings.getByRole("list", { name: "机器" }).getByRole("button", { name: /^work-pc/ });
  await expect(row).toContainText("你的设备");
  await row.click();
  // Its machine is removed with the device, in one place only: removing
  // just the machine would leave the device logged in.
  await expect(settings.locator(".set-acts").getByRole("button", { name: "移除…" })).toHaveCount(0);
  await expect(settings.getByText("这台机器也是你的设备「work-pc」。要登出它，到账号与设备。")).toBeVisible();
  await snap(settings, "settings-machine-device");
  await settings.locator(".set-note").getByRole("button", { name: "账号与设备" }).click();
  const devices = settings.getByRole("list", { name: "你的设备" });
  const pcRow = devices.getByRole("listitem").filter({ hasText: "work-pc" });
  await expect(pcRow).toContainText("也在执行任务");
  await pcRow.getByRole("button", { name: "登出" }).click();
  const dialog = settings.getByRole("dialog", { name: "登出设备「work-pc」？" });
  await expect(dialog).toContainText("它会马上被登出：服务器断开它，你的机器也不再接受它的连接；它自己的执行机器也一并移除。");
  await dialog.getByRole("button", { name: "移除" }).click();
  await expect(settings.getByRole("status").filter({ hasText: "已移除「work-pc」" })).toBeVisible({ timeout: 30_000 });
  await expect.poll(async () => (await first.listDevices({})).devices.map((d) => d.label), { timeout: 30_000 }).toEqual(["first-mac"]);
  await expect.poll(async () => (await pc.getStatus({})).removed, { timeout: 30_000 }).toBe(true);
  expect((await first.listMachines({})).machines.map((m) => m.machineName)).not.toContain("work-pc");
  await app.close();
});
