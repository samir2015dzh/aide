import { expect, test, type ElectronApplication, type Page } from "@playwright/test";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fixture, launch, onWindows, snap } from "./app";

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
  await expect(main.getByRole("status").filter({ hasText: "已在 owner-mac 上检测到 · 版本 1.2.3" })).toBeVisible({ timeout: 30_000 });
  await main.getByRole("button", { name: "运行测试" }).click();
  await expect(main.getByText(/^可以用：DSH 在 [\d.]+ 秒内回答了「ready」。$/)).toBeVisible({ timeout: 60_000 });
  await snap(settings, "connect-deepseek");

  await main.getByRole("button", { name: "开始测试任务" }).click();
  await main.getByRole("button", { name: "在 Aide 中打开" }).click();
  await expect
    .poll(async () => (await api<{ sessions?: { agent: string; createdBy: string }[] }>(settings, "ListSessions")).sessions?.some((s) => s.agent === "deepseek" && s.createdBy === "owner"))
    .toBe(true);
  await main.getByRole("button", { name: "打开 DSH 会话" }).click();
  await expect(page.getByRole("navigation", { name: "DSH 会话" })).toBeVisible();

  await main.getByRole("tab", { name: "Claude Desktop" }).click();
  await expect(main.getByRole("status").filter({ hasText: "在 owner-mac 上没有检测到" })).toBeVisible({ timeout: 30_000 });
  await expect(main.getByText("从 claude.ai/download 装好 Claude Desktop 并登录（或者单独装 Claude Code）。")).toBeVisible();
  await expect(main.getByText(/Code 标签页里也会列出这些任务/)).toBeVisible();
  if (!onWindows) {
    await main.getByRole("tab", { name: "Codex" }).click();
    await expect(main.getByRole("status").filter({ hasText: "在 owner-mac 上找到了，但还没登录" })).toBeVisible({ timeout: 30_000 });
    await expect(main.locator("code").filter({ hasText: /codex" login$/ })).toBeVisible();
    await expect(main.getByRole("button", { name: "运行测试" })).toBeDisabled();
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
  await expect(main.getByRole("status").filter({ hasText: "已在 owner-mac 上检测到 · 版本 1.2.3" })).toBeVisible({ timeout: 30_000 });
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
  const [settings] = await Promise.all([app.waitForEvent("window"), page.locator(".side-nav").getByRole("button", { name: "连接 Agent" }).click()]);
  await settings.waitForLoadState();
  const main = settings.getByRole("main", { name: "连接" });
  await expect(main.getByRole("tab", { name: "DSH", exact: true })).toHaveAttribute("aria-selected", "true");
  for (const name of ["Claude Desktop", "Codex", "其他 Agent", "助手"]) await expect(main.getByRole("tab", { name })).toHaveAttribute("aria-selected", "false");

  // A link names its line (connect/<line>): the same window, on that line.
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
