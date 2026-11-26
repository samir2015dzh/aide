import { expect, test, type ElectronApplication, type Page } from "@playwright/test";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fixture, launch, onWindows, snap, navTo } from "./app";

// Settings › 连接: a guide for each coding agent, as the owner follows it alone,
// and the assistants'. This Mac has DeepSeek Harness (fakeacp), Codex not
// signed in (not on Windows), no Claude Code; another ACP agent is added
// through the template.

async function openSettings(app: ElectronApplication, page: Page, tab?: string): Promise<Page> {
  const [win] = await Promise.all([app.waitForEvent("window"), page.evaluate((t) => window.aide?.openSettings(t), tab)]);
  await win.waitForLoadState();
  return win;
}

async function api<T>(page: Page, method: string, body: object = {}): Promise<T> {
  return page.evaluate(
    async ([m, b]) => {
      const r = await fetch(`app://aide/api/aide.local.v1.LocalService/${m}`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(b) });
      return r.json();
    },
    [method, body] as const,
  );
}

const fake = path.join(path.dirname(fixture.cli), onWindows ? "fakeacp.exe" : "fakeacp");

test("DeepSeek: detected, a test, a test task, and DSH's own sessions; Claude and Codex say what to do", async () => {
  const { app, page } = await launch();
  const settings = await openSettings(app, page, "connect");
  const main = settings.getByRole("main", { name: "连接" });
  await expect(main.getByRole("tab", { name: "DSH", exact: true })).toHaveAttribute("aria-selected", "true");
  await expect(main.getByText("开始之前")).toBeVisible();
  await expect(main.getByRole("status").filter({ hasText: /已在 owner-mac 上检测到/ })).toBeVisible({ timeout: 30_000 });
  await main.getByRole("button", { name: "运行测试" }).click();
  await expect(main.getByText(/^可以用：DSH 在 [\d.]+ 秒内回答了「ready」。$/)).toBeVisible({ timeout: 60_000 });
  await snap(settings, "connect-deepseek");

  await main.getByRole("button", { name: "开始测试任务" }).click();
  await main.getByRole("button", { name: "在 Aide 中打开" }).click();
  await expect
    .poll(async () => (await api<{ sessions?: { agent: string; createdBy: string }[] }>(settings, "ListSessions")).sessions?.some((s) => s.agent === "deepseek" && s.createdBy === "owner"))
    .toBe(true);
  await main.getByRole("button", { name: "打开 DSH 会话" }).click();
  const dshBot = page.getByRole("region", { name: /^DSH/ });
  await dshBot.getByRole("button", { name: /全部对话/ }).click();
  await expect(dshBot.getByRole("region", { name: "它自己的会话" })).toBeVisible();

  await main.getByRole("tab", { name: "Claude Desktop" }).click();
  await expect(main.getByRole("status").filter({ hasText: "在 owner-mac 上没有检测到" })).toBeVisible({ timeout: 30_000 });
  await expect(main.getByText("从 claude.ai/download 装好 Claude Desktop 并登录（或者单独装 Claude Code）。")).toBeVisible();
  await expect(main.getByText(/Code 标签页里也会列出这些任务/)).toBeVisible();
  if (!onWindows) {
    await main.getByRole("tab", { name: "Codex" }).click();
    await expect(main.getByRole("status").filter({ hasText: "在 owner-mac 上找到了，但还没登录" })).toBeVisible({ timeout: 30_000 });
    await expect(main.locator("code").filter({ hasText: /codex" login$/ })).toBeVisible();
    // Not usable yet: one line pointing at the status, no buttons that can't be pressed (工单 #136 P16).
    const verify = main.getByRole("region", { name: "确认能用" });
    await expect(verify).toHaveText(/^确认能用Codex 在 owner-mac 上能用了，才能在这里测试：先按上面「状态」里说的处理。$/);
    await expect(verify.getByRole("button")).toHaveCount(0);
  }
  await app.close();
});

test("another ACP agent: a wrong command says why; saved, it is listed, tested and removed", async () => {
  const { app, page } = await launch();
  const settings = await openSettings(app, page, "connect");
  const main = settings.getByRole("main", { name: "连接" });
  await main.getByRole("tab", { name: "其他 Agent" }).click();
  await expect(main.getByText("还没有。")).toBeVisible();
  await main.getByRole("button", { name: "添加 Agent" }).click();
  await expect(main.getByText("示例：-y @zed-industries/claude-code-acp")).toBeVisible();
  await main.getByLabel("名称", { exact: true }).fill("my-acp");
  await main.getByLabel("命令", { exact: true }).fill("aide-no-such-agent");
  await main.getByRole("button", { name: "测试连接" }).click();
  await expect(main.getByText(/^连接失败：.*aide-no-such-agent not found/)).toBeVisible({ timeout: 60_000 });

  await main.getByLabel("命令", { exact: true }).fill(fake);
  await main.getByLabel("参数", { exact: true }).fill("acp");
  for (const [name, value] of [["FAKE_ACP_STATE", fs.mkdtempSync(path.join(os.tmpdir(), "hmacp-"))], ["MY_API_KEY", "k-1"]]) {
    await main.getByRole("button", { name: "添加变量" }).click();
    await main.getByLabel("变量名").last().fill(name);
    await main.getByLabel("值", { exact: true }).last().fill(value);
  }
  await expect(main.getByRole("checkbox", { name: "机密" }).last()).toBeChecked();
  await main.getByRole("button", { name: "测试连接" }).click();
  await expect(main.getByText("连接成功：它说自己是 Fake ACP 1.2.3，并开了一个会话。")).toBeVisible({ timeout: 60_000 });
  await snap(settings, "connect-custom");
  await main.getByRole("button", { name: "保存" }).click();

  const row = main.getByRole("group", { name: "my-acp" });
  await expect(row).toBeVisible();
  await expect(main.getByRole("status").filter({ hasText: /已在 owner-mac 上检测到/ })).toBeVisible({ timeout: 30_000 });
  await main.getByRole("button", { name: "运行测试" }).click();
  await expect(main.getByText(/^可以用：my-acp 在 [\d.]+ 秒内回答了「ready」。$/)).toBeVisible({ timeout: 60_000 });
  const st = await api<{ localMachineId: string }>(settings, "GetStatus");
  const saved = await api<{ agents?: { name: string; env?: { name: string; value?: string; secret?: boolean }[] }[] }>(settings, "ListMachineCustomAgents", { machineId: st.localMachineId });
  expect(saved.agents?.[0].env).toEqual([{ name: "FAKE_ACP_STATE", value: expect.any(String) }, { name: "MY_API_KEY", secret: true }]);

  await row.getByRole("button", { name: "移除…" }).click();
  await row.getByRole("button", { name: "移除", exact: true }).click();
  await expect(main.getByText("还没有。")).toBeVisible();
  await app.close();
});

test("the guide from the sidebar, and opened at a line: the assistants' shows one that logs in with the account", async () => {
  const { app, page } = await launch();
  // The sidebar's 连接 Agent opens it on its first line.
  const [settings] = await Promise.all([app.waitForEvent("window"), navTo(page, /^连接 DSH、Claude Desktop/)]);
  await settings.waitForLoadState();
  const main = settings.getByRole("main", { name: "连接" });
  await expect(main.getByRole("tab", { name: "DSH", exact: true })).toHaveAttribute("aria-selected", "true");
  for (const name of ["Claude Desktop", "Codex", "Antigravity", "其他 Agent", "助手"]) await expect(main.getByRole("tab", { name })).toHaveAttribute("aria-selected", "false");

  // A link names its line (connect/<line>): the same window, on that line.
  await page.evaluate(() => window.aide?.openSettings("connect/antigravity"));
  await expect(main.getByRole("tab", { name: "Antigravity" })).toHaveAttribute("aria-selected", "true");
  await page.evaluate(() => window.aide?.openSettings("connect/codex"));
  await expect(main.getByRole("tab", { name: "Codex" })).toHaveAttribute("aria-selected", "true");
  await page.evaluate(() => window.aide?.openSettings("connect/assistants"));
  await expect(main.getByRole("tab", { name: "助手" })).toHaveAttribute("aria-selected", "true");
  expect(app.windows()).toHaveLength(2);

  // The assistants': pick one, send it the message; it logs in with the account itself and shows here, nothing to approve.
  const st = await api<{ relayUrl: string; account: string }>(settings, "GetStatus");
  const name = `Helper ${process.pid}`;
  await main.getByRole("radio", { name: "其他 Agent" }).click();
  await main.getByLabel("名称").fill(name);
  await expect(main.locator(".msgbox")).toContainText(`aide auth login ${st.relayUrl} --account ${st.account} --name "${name}"`);
  await expect(main).toContainText(`等待 ${name} 登录…`);
  await snap(settings, "connect-assistants");
  const home = fs.mkdtempSync(path.join(os.tmpdir(), "hmagent-"));
  execFileSync(fixture.cli, ["auth", "login", st.relayUrl, "--account", st.account, "--name", name], { env: { ...process.env, AIDE_HOME: home, AIDE_PASSWORD: "correct horse battery" } });
  await expect(main.getByText(`${name} 已连接`)).toBeVisible({ timeout: 30_000 });
  await expect(main.getByRole("radiogroup", { name: "选择助手" })).toHaveCount(0);
  await snap(settings, "connect-assistants-connected");
  // Signed out again: the other tests' world stays as it was.
  const { clients } = await api<{ clients: { grantId: string; label: string }[] }>(settings, "ListClients");
  await api(settings, "RevokeClient", { grantId: clients.find((c) => c.label === name)!.grantId });
  await app.close();
});

// 工单 #133: a computer with Antigravity's desktop app only has no ACP server for Aide. Its line says so as it is — not
// "signed in" beside "not detected" — and installs the server itself, one button, from the ACP registry (here the
// test's: e2e/desktop_test.go); then it is detected, and what is left is the sign-in, which only its owner can give.
test("Antigravity's line installs its ACP server with one button, and says what is left as it stands", async () => {
  const { app, page } = await launch();
  try {
    const settings = await openSettings(app, page, "connect/antigravity");
    const main = settings.getByRole("main", { name: "连接" });
    await expect(main.getByRole("tab", { name: "Antigravity" })).toHaveAttribute("aria-selected", "true");
    const before = main.getByRole("region", { name: "开始之前" });
    const status = main.getByRole("status");
    await expect(status).toContainText("没有检测到", { timeout: 30_000 });
    await expect(before).toContainText("还没有 Google 官方的 Antigravity ACP 服务");
    await expect(before).toContainText("服务装好后再登录");
    await expect(before).not.toContainText("已完成登录");
    await snap(settings, "connect-133-antigravity-missing");

    const install = main.getByRole("group", { name: "安装 Antigravity ACP 服务" });
    await install.getByRole("button", { name: "安装 Antigravity ACP 服务" }).click();
    await expect(status).toContainText("找到了，但还没登录", { timeout: 60_000 });
    await expect(install).toHaveCount(0);
    await expect(before).toContainText("已装好 Google 官方的 Antigravity ACP 服务");
    await expect(before).toContainText("还没登录：这一步只能你本人完成");
    await expect(main.getByText("aide agent login antigravity").first()).toBeVisible();
    await snap(settings, "connect-133-antigravity-installed");
  } finally {
    await app.close();
  }
});

// 工单 #136: every coding agent Aide knows has a
// row of its own with how it stands — WorkBuddy among them (P09) — and a project list reads as Chinese (P32).
test("the Connect pages list every known coding agent with its state, and name projects as Chinese does", async () => {
  const { app, page } = await launch();
  try {
    // (P07's directions are checked over every text in renderer/src/directions.test.ts.)
    const settings = await openSettings(app, page, "connect/custom");
    const main = settings.getByRole("main", { name: "连接" });
    await expect(main.getByRole("tab", { name: "其他 Agent" })).toHaveAttribute("aria-selected", "true");
    const known = main.getByRole("region", { name: "Aide 认识的其他编码 Agent" });
    const workbuddy = known.getByRole("group", { name: "CodeBuddy Code（WorkBuddy）" });
    await expect(workbuddy).toBeVisible();
    await expect(workbuddy).toContainText(/owner-mac/, { timeout: 30_000 });
    for (const name of ["Gemini CLI", "Cursor", "Qwen Code"]) await expect(known.getByRole("group", { name })).toBeVisible();
    await expect(known.getByRole("group", { name: "DSH" })).toHaveCount(0); // its own line above
    await expect(main).not.toContainText("例如 WorkBuddy");
    await snap(settings, "connect-136-known-agents");

    // DSH's line: its projects named with 、, never ", ".
    await main.getByRole("tab", { name: "DSH", exact: true }).click();
    const verify = main.getByRole("region", { name: "确认能用" });
    await expect(verify).toContainText("可以用", { timeout: 30_000 });
    await expect(verify).not.toContainText(", ");
    // 工单 #137 P17: "detected" once, in the status; the details below say what they add.
    await expect(main.getByRole("status").filter({ hasText: "已在 owner-mac 上检测到" })).toBeVisible();
    await expect(main.getByRole("region", { name: "连接细项" })).not.toContainText("已检测到");

    // 工单 #137 P10: the bot page's head says its state once — the word, then what the line adds, not the word again.
    await page.bringToFront();
    const head = page.getByRole("region", { name: /^DSH/ }).locator(".bot-head");
    await page.getByRole("button", { name: /^DSH/ }).first().click();
    await expect(head).toBeVisible({ timeout: 30_000 });
    const word = (await head.locator(".bot-pill").innerText()).trim();
    expect(((await head.innerText()).match(new RegExp(word, "g")) ?? []).length, `"${word}" in the head`).toBe(1);
  } finally {
    await app.close();
  }
});
