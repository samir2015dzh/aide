import { expect, test, type ElectronApplication, type Page } from "@playwright/test";
import { execFile, execFileSync, spawn, type ChildProcess } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fixture, launch, snap, sessionList } from "./app";

// The sheets over the main window: adding a project, connecting an
// assistant, which logs in with the account itself and shows there at once,
// and adding a machine, which shows there once it logged in; then what an
// agent may use, changed from settings (docs/accounts.md).

/** The account and password the fixture's owner was set up with (the Go harness's test account). */
const password = "correct horse battery";

/** What the app menu, the menu bar and notifications send the page (e.g. 文件 › 连接助手…). */
async function command(app: ElectronApplication, page: Page, c: string) {
  await expect((await sessionList(page))).toBeVisible();
  type Main = { aideTest: { mainWindow(): { webContents: { send(channel: string, c: string): void } } } };
  await app.evaluate((_, c) => (globalThis as unknown as Main).aideTest.mainWindow().webContents.send("command", c), c);
}

/** aided's local API, as the page reaches it. */
async function api<T = Record<string, never>>(page: Page, method: string, body: object = {}): Promise<T> {
  return page.evaluate(
    async ([m, b]) => {
      const r = await fetch(`app://aide/api/aide.local.v1.LocalService/${m}`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(b) });
      return r.json();
    },
    [method, body] as const,
  );
}

type Status = { relayUrl: string; account: string; localMachineId: string };
type Client = { grantId: string; grantVersion: string; label: string; expiresAt?: string; spec: { machineIds: string[]; projectIds: string[]; agents: string[] } };

const running = new Set<ChildProcess>();
test.afterEach(() => running.forEach((p) => p.kill())); // a failed test leaves no login waiting

/** Runs an Aide binary in the background with the account's password; exit resolves to its exit code. */
function background(bin: string, args: string[], env: Record<string, string> = {}) {
  const p = spawn(bin, args, { env: { ...process.env, AIDE_PASSWORD: password, ...env }, stdio: ["ignore", "pipe", "pipe"] });
  running.add(p);
  p.on("exit", () => running.delete(p));
  let out = "";
  p.stdout.on("data", (b: Buffer) => (out += b));
  p.stderr.on("data", (b: Buffer) => (out += b));
  const exit = new Promise<number>((resolve) => p.on("exit", (c) => resolve(c ?? -1)));
  return { exit, output: () => out };
}

/** The agent CLI as a new agent on this computer: its own home, so it logs in anew. */
function newAgent() {
  const home = fs.mkdtempSync(path.join(os.tmpdir(), "hmagent-"));
  return {
    login: (st: Status, name: string) => background(fixture.cli, ["auth", "login", st.relayUrl, "--account", st.account, "--name", name], { AIDE_HOME: home }),
    run: (...args: string[]) =>
      new Promise<string>((resolve, reject) =>
        execFile(fixture.cli, args, { env: { ...process.env, AIDE_HOME: home } }, (err, stdout, stderr) => (err ? reject(new Error(stderr || String(err))) : resolve(stdout))),
      ),
  };
}

/** An agent logged in, waited for: its grant. */
async function loggedIn(page: Page, st: Status, name: string): Promise<Client> {
  const login = newAgent().login(st, name);
  expect(await login.exit, login.output()).toBe(0);
  let grant: Client | undefined;
  await expect.poll(async () => (grant = (await api<{ clients: Client[] }>(page, "ListClients")).clients.find((c) => c.label === name))).toBeTruthy();
  return grant!;
}

test("adding a project: a folder on this Mac becomes a project", async () => {
  const folder = `site-${process.pid}`;
  const dir = path.join(fs.mkdtempSync(path.join(os.tmpdir(), "hmproj-")), folder);
  fs.mkdirSync(dir);
  fs.writeFileSync(path.join(dir, "README.md"), "# site\n");
  for (const args of [["init", "-q"], ["add", "."], ["-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-qm", "init"]]) execFileSync("git", args, { cwd: dir });

  const { app, page } = await launch();
  await command(app, page, "addProject");
  const sheet = page.getByRole("dialog", { name: "添加项目" });
  // The window behind goes dim and soft, as behind every dialog.
  await expect(page.locator(".sheet-scrim")).toHaveCSS("backdrop-filter", "blur(12px) saturate(1.2)");
  await expect(sheet.getByRole("group", { name: "机器" }).getByRole("button", { name: "owner-mac" })).toHaveAttribute("aria-pressed", "true");
  await expect(sheet.getByRole("button", { name: "选择…" })).toBeVisible(); // this Mac: the folder can be picked
  await sheet.getByLabel("目录").fill(dir); // the native folder dialog can't be driven
  await expect(sheet.getByLabel("项目名")).toHaveValue(folder);
  await expect(sheet.getByRole("group", { name: "工作方式" }).getByRole("button", { name: "独立 worktree" })).toHaveAttribute("aria-pressed", "true");
  await snap(page, "sheet-add-project");
  await sheet.getByRole("button", { name: "添加" }).click();
  await expect(page.getByRole("status").filter({ hasText: `已添加项目「${folder}」` })).toBeVisible();
  await expect(sheet).toBeHidden();

  const st = await api<Status>(page, "GetStatus");
  const { projects } = await api<{ projects: { machineId: string; projectId: string; workspace?: string }[] }>(page, "ListProjects");
  expect(projects).toContainEqual(expect.objectContaining({ machineId: st.localMachineId, projectId: folder, workspace: "WORKSPACE_WORKTREE" }));
  await app.close();
});

test("connecting an assistant: the message to send, it logs in with the account itself and shows as connected; what it may use, changed there", async () => {
  const name = `Agent ${process.pid}`; // its own, also when run again against the same daemon
  const { app, page } = await launch();
  const st = await api<Status>(page, "GetStatus");
  await command(app, page, "newGrant");
  const sheet = page.getByRole("dialog", { name: "连接助手" });
  await expect(sheet).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(sheet).toBeHidden();
  // As in the first run: the assistants in a row, the first one picked, its message below.
  await command(app, page, "newGrant");
  await expect(sheet.getByRole("radio", { name: "Grok Bot" })).toHaveAttribute("aria-checked", "true");
  await expect(sheet).toContainText('--name "Grok Bot"');
  await snap(page, "sheet-new-grant");
  await sheet.getByRole("radio", { name: "其他 Agent" }).click();

  await sheet.getByLabel("名称").fill(name);
  // The server and the account; the password is for the owner to fill in, never in it.
  await expect(sheet.locator(".msgbox")).toContainText(`aide auth login ${st.relayUrl} --account ${st.account} --name "${name}"`);
  await expect(sheet.locator(".msgbox")).not.toContainText(password);
  await sheet.getByRole("button", { name: "复制" }).click();
  await expect(sheet.getByRole("button", { name: "已复制" })).toBeVisible();
  await expect.poll(() => app.evaluate(({ clipboard }) => clipboard.readText())).toContain(`--name "${name}"`);
  await expect(sheet).toContainText(`等待 ${name} 登录…`);
  await snap(page, "sheet-connect-copy");

  // The agent runs the command it was sent: nothing to approve, the sheet says it is connected.
  const agent = newAgent();
  const login = agent.login(st, name);
  await expect(sheet.getByText(`${name} 已连接`)).toBeVisible({ timeout: 30_000 });
  expect(await login.exit, login.output()).toBe(0);
  await expect(sheet.getByRole("button", { name: "完成" })).toBeVisible();
  await snap(page, "sheet-connect-done");
  // Every machine, project and coding agent, until signed out.
  const grant = async () => (await api<{ clients: Client[] }>(page, "ListClients")).clients.find((x) => x.label === name)!;
  expect((await grant()).spec).toMatchObject({ machineIds: ["*"], projectIds: ["*"], agents: ["*"] });

  // What it may use, narrowed from there.
  await sheet.getByRole("button", { name: "调整它能用什么…" }).click();
  const scope = page.getByRole("dialog", { name: `修改「${name}」能用什么` });
  const projects = scope.getByRole("group", { name: "项目" });
  await projects.getByRole("checkbox", { name: "所有项目（包括以后添加的）" }).setChecked(false);
  for (const box of await projects.getByRole("checkbox").all()) {
    const text = (await box.textContent()) ?? "";
    if (!text.includes("所有项目")) await box.setChecked(text.includes("/ demo"));
  }
  await projects.getByRole("button", { name: "好" }).click();
  const agents = scope.getByRole("group", { name: "编码 agent" });
  await agents.getByRole("button", { name: "修改" }).click();
  const allAgents = agents.getByRole("checkbox", { name: "所有编码 agent（包括以后安装的）" });
  await expect(allAgents).toBeChecked();
  await allAgents.setChecked(false);
  await agents.getByRole("checkbox", { name: "Gemini CLI" }).setChecked(true);
  await agents.getByRole("button", { name: "好" }).click();
  await scope.getByRole("group", { name: "有效期" }).getByRole("button", { name: "30 天" }).click();
  await expect(projects).toContainText("owner-mac 上的 demo");
  await expect(agents).toContainText("Gemini CLI");
  await expect(agents).not.toContainText("Codex");
  await snap(page, "sheet-connect-scope");
  await scope.getByRole("button", { name: "保存" }).click();
  await expect(page.getByRole("status").filter({ hasText: `「${name}」已按新设置工作` })).toBeVisible({ timeout: 30_000 });
  await expect(scope).toBeHidden();

  const c = await grant();
  expect(c.spec).toMatchObject({ machineIds: [st.localMachineId], projectIds: ["demo"], agents: ["gemini"] });
  expect((Date.parse(c.expiresAt!) - Date.now()) / 86_400_000).toBeCloseTo(30, 0);
  const list = await agent.run("project", "list");
  expect(list.match(/^owner-mac\/\S+/gm)).toEqual(["owner-mac/demo"]); // the agent sees only what it was given
  await app.close();
});

test("adding a machine: its system, then the app or a command (Linux: the command only), with the server and the account; it shows once it logged in", async () => {
  const name = `build-${process.pid}`;
  const { app, page } = await launch();
  const st = await api<Status>(page, "GetStatus");
  await command(app, page, "addMachine");
  const sheet = page.getByRole("dialog");
  await expect(sheet.getByRole("heading", { name: "添加机器" })).toBeVisible();
  const how = sheet.getByRole("group", { name: "安装方式" });
  const viaApp = how.getByRole("button", { name: "安装应用" });
  const viaCommand = how.getByRole("button", { name: "终端命令" });
  const install = `curl -fsSL https://github.com/samir2015dzh/aide/releases/latest/download/install.sh | AIDE_ACCOUNT='${st.account}' sh -s -- ${st.relayUrl}`;

  // A Mac, by default with the app: opened there, then logged in to this server with the account, as a machine only.
  await expect(sheet.getByRole("button", { name: "Mac", exact: true })).toHaveAttribute("aria-pressed", "true");
  await expect(viaApp).toHaveAttribute("aria-pressed", "true");
  await expect(sheet).toContainText("在那台 Mac 上打开github.com/samir2015dzh/aide/releases/latest");
  await expect(sheet).toContainText(`服务器填${st.relayUrl}`);
  await expect(sheet).toContainText(`账号填${st.account}`);
  await expect(sheet).not.toContainText("install.sh");
  await snap(page, "sheet-add-machine");
  await viaCommand.click();
  await expect(sheet).toContainText("终端 · 用你平时写代码的用户");
  await expect(sheet).toContainText(install);
  await expect(sheet).toContainText("命令会在那台机器上问你的密码：密码不会出现在命令里。");

  // A Windows PC: the app, or install.ps1 in PowerShell.
  await sheet.getByRole("button", { name: "Windows" }).click();
  await expect(sheet).toContainText("PowerShell · 用你平时写代码的用户");
  await expect(sheet).toContainText(`$env:AIDE_ACCOUNT='${st.account}'; & ([scriptblock]::Create((irm https://github.com/samir2015dzh/aide/releases/latest/download/install.ps1))) ${st.relayUrl}`);
  await snap(page, "sheet-add-machine-windows");
  await viaApp.click();
  await expect(sheet).toContainText("在那台电脑上打开github.com/samir2015dzh/aide/releases/latest");

  // Linux has no app: the command, whatever was picked before.
  await sheet.getByRole("button", { name: "Linux" }).click();
  await expect(viaApp).toBeDisabled();
  await expect(viaApp).toContainText("暂无");
  await expect(viaCommand).toHaveAttribute("aria-pressed", "true");
  await expect(sheet).toContainText(install);
  await snap(page, "sheet-add-machine-linux");

  // Copy puts the whole command on the clipboard.
  await sheet.getByRole("button", { name: "拷贝" }).click();
  await expect.poll(() => app.evaluate(({ clipboard }) => clipboard.readText())).toBe(install);
  await expect(sheet).toContainText("等它登录…");

  // The machine logs in with the account and password (aided login, as the install script runs it), and runs (its
  // background service would): the sheet says so, by the name it gave itself.
  const aided = path.join(path.dirname(fixture.cli), "aided");
  const data = fs.mkdtempSync(path.join(os.tmpdir(), "hmmachine-"));
  const login = background(aided, ["login", st.relayUrl, "--account", st.account, "--name", name, "--no-service", "--data-dir", data]);
  expect(await login.exit, login.output()).toBe(0);
  background(aided, ["run", "--data-dir", data]); // stopped after the test
  await expect(sheet).toContainText(`「${name}」已添加`, { timeout: 30_000 });
  await snap(page, "sheet-add-machine-added");
  await sheet.getByRole("button", { name: "完成" }).click();
  await expect(sheet).toBeHidden();
  const { machines } = await api<{ machines: { machineId: string; machineName: string }[] }>(page, "ListMachines");
  const m = machines.find((x) => x.machineName === name);
  expect(m).toBeTruthy();
  await api(page, "RemoveMachine", { machineId: m!.machineId }); // the other tests' world stays as it was
  await app.close();
});

test("a hosted agent shows its app's icon only when it logs in from the app's own domain", async () => {
  // A connection to the hosted MCP gateway is named "Name (host)"; any app can call itself Muse.
  const muse = `Muse (a${process.pid}.meta.ai)`;
  const impostor = `Muse (meta.ai.p${process.pid}.example)`;
  const { app, page } = await launch();
  const st = await api<Status>(page, "GetStatus");
  for (const name of [muse, impostor]) await loggedIn(page, st, name);

  const [settings] = await Promise.all([app.waitForEvent("window"), page.evaluate(() => window.aide?.openSettings("grants"))]);
  await settings.waitForLoadState();
  const grants = settings.getByRole("list", { name: "授权" }).getByRole("button");
  // The app by its own name, with its logo on its face; the impostor keeps where it logged in, and no app's logo: its
  // character alone (工单 #83).
  await expect(grants.filter({ has: settings.locator(".fn", { hasText: /^Muse$/ }) }).first().locator(".bav-app img")).toBeVisible();
  await expect(grants.filter({ hasText: impostor }).locator(".sand-grok-bot-mark svg")).toBeVisible();
  await expect(grants.filter({ hasText: impostor }).locator(".bav-app")).toHaveCount(0);
  await app.close();
});

test("renaming an agent from settings keeps what it may use and until when; a known app's name brings its icon", async () => {
  const name = `Renamer ${process.pid}`;
  const { app, page } = await launch();
  const st = await api<Status>(page, "GetStatus");
  const before = await loggedIn(page, st, name);
  const clients = async () => (await api<{ clients: Client[] }>(page, "ListClients")).clients;

  const [settings] = await Promise.all([app.waitForEvent("window"), page.evaluate(() => window.aide?.openSettings("grants"))]);
  await settings.waitForLoadState();
  await settings.getByRole("list", { name: "授权" }).getByRole("button", { name: new RegExp(name) }).click();
  const panel = settings.getByRole("main", { name: "授权" });
  const sheet = settings.getByRole("dialog", { name: "名字和头像" });
  // Kept as it is: nothing to sign.
  await panel.getByRole("button", { name: "名字和头像…" }).click();
  await sheet.getByRole("button", { name: "存储" }).click();
  await expect(sheet).toBeHidden();
  expect((await clients()).find((c) => c.grantId === before.grantId)!.grantVersion).toBe(before.grantVersion);

  await panel.getByRole("button", { name: "名字和头像…" }).click();
  await expect(sheet.getByRole("textbox", { name: "名字" })).toHaveValue(name);
  await sheet.getByRole("textbox", { name: "名字" }).fill("Grok Bot");
  await sheet.getByRole("button", { name: "存储" }).click(); // the server signs it for this device
  await expect(sheet).toBeHidden();
  await expect(panel.locator(".dhero b")).toHaveText("Grok Bot");
  await expect(panel.locator(".dhero .bav-app img")).toBeVisible(); // Grok Bot's logo on its face
  const after = (await clients()).find((c) => c.grantId === before.grantId)!;
  expect(after.label).toBe("Grok Bot");
  expect(after.expiresAt).toEqual(before.expiresAt);
  expect(after.spec).toMatchObject({ ...before.spec, label: "Grok Bot" });
  await snap(settings, "settings-grant-renamed");
  await app.close();
});

test("changing what an agent may use, and renewing it, from settings", async () => {
  const name = `Changer ${process.pid}`;
  const { app, page } = await launch();
  const st = await api<Status>(page, "GetStatus");
  await loggedIn(page, st, name);
  const grant = async () => (await api<{ clients: Client[] }>(page, "ListClients")).clients.find((c) => c.label === name)!;

  const [settings] = await Promise.all([app.waitForEvent("window"), page.evaluate(() => window.aide?.openSettings("grants"))]);
  await settings.waitForLoadState();
  await settings.getByRole("list", { name: "授权" }).getByRole("button", { name: new RegExp(name) }).click();
  const panel = settings.getByRole("main", { name: "授权" });
  await panel.getByRole("button", { name: "修改范围…" }).click();
  const sheet = settings.getByRole("dialog", { name: `修改「${name}」能用什么` });
  // As it logged in: everything, long-lived.
  await expect(sheet.getByRole("group", { name: "项目" })).toContainText("所有项目（包括以后添加的）");
  await expect(sheet.getByRole("group", { name: "有效期" }).getByRole("button", { name: "长期" })).toHaveAttribute("aria-pressed", "true");
  await sheet.getByRole("checkbox", { name: "所有项目（包括以后添加的）" }).setChecked(false);
  for (const box of await sheet.getByRole("group", { name: "项目" }).getByRole("checkbox").all()) {
    const text = (await box.textContent()) ?? "";
    if (!text.includes("所有项目")) await box.setChecked(text.includes("/ demo"));
  }
  await sheet.getByRole("group", { name: "有效期" }).getByRole("button", { name: "30 天" }).click();
  await snap(settings, "sheet-scope");
  await sheet.getByRole("button", { name: "保存" }).click();
  await expect(settings.getByRole("status").filter({ hasText: `「${name}」已按新设置工作` })).toBeVisible();
  await expect(sheet).toBeHidden();
  expect((await grant()).spec).toMatchObject({ machineIds: [st.localMachineId], projectIds: ["demo"], agents: ["*"] });

  await panel.getByRole("button", { name: "续期…" }).click();
  const renew = settings.getByRole("dialog", { name: `续期「${name}」的授权` });
  await renew.getByRole("button", { name: "90 天" }).click();
  await renew.getByRole("button", { name: "保存" }).click();
  await expect(renew).toBeHidden();
  await expect.poll(async () => Math.round((Date.parse((await grant()).expiresAt!) - Date.now()) / 86_400_000)).toBe(90);
  await app.close();
});
