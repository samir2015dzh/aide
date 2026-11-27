import { expect, test, type ElectronApplication, type Page } from "@playwright/test";
import { launch, pick, snap } from "./app";

// 工单 #180, as the owner met it on a real machine: no row or chip that does nothing when clicked.
// A: every agent the "not installed" list shows opens something: its Connect guide, or in place what it is, which
// command the machine looks for, its official page and its official ACP registry entry.
// C: on a Claude Code bot's page, with the model left at its default, reasoning effort and mode both open, and a new
// conversation runs with what was chosen (read back from the session).

async function openSettings(app: ElectronApplication, page: Page, tab?: string): Promise<Page> {
  const [win] = await Promise.all([app.waitForEvent("window"), page.evaluate((t) => window.aide?.openSettings(t), tab)]);
  await win.waitForLoadState();
  return win;
}

test("A: each not-installed agent opens something: its guide, or in place what it is and where to install it", async () => {
  const { app, page } = await launch();
  const settings = await openSettings(app, page, "machines");
  await settings.setViewportSize({ width: 1280, height: 900 });
  const panel = settings.getByRole("main", { name: "机器" });
  await panel.getByRole("list", { name: "机器" }).getByRole("button", { name: /^owner-mac/ }).click();
  const missing = panel.getByRole("list", { name: "没有安装的 Agent" });
  await expect(missing.getByRole("listitem").first()).toBeVisible({ timeout: 30_000 });
  // Every row is a button.
  const rows = missing.getByRole("listitem");
  const n = await rows.count();
  expect(n).toBeGreaterThan(3);
  for (let i = 0; i < n; i++) await expect(rows.nth(i).getByRole("button")).toHaveCount(1);
  // One with no guide opens in place: what it is, the command, its registry entry (the embedded snapshot here), its site.
  const cursor = missing.getByRole("listitem", { name: "Cursor" }).getByRole("button");
  await cursor.click();
  await expect(cursor).toHaveAttribute("aria-expanded", "true");
  const info = panel.getByRole("region", { name: "Cursor" });
  await expect(info).toContainText("Cursor 的编码 Agent 命令行");
  await expect(info).toContainText("cursor-agent");
  await expect(info).toContainText("只装 Cursor 桌面应用不够");
  await expect(info.getByRole("button", { name: /^官网/ })).toBeVisible();
  await expect(info.getByRole("button", { name: "重新检查" })).toBeVisible();
  await expect(info.getByText(/ACP 注册表/)).toBeVisible({ timeout: 30_000 });
  await expect(info.getByText("正在查官方 ACP 注册表…")).toHaveCount(0, { timeout: 30_000 });
  await info.scrollIntoViewIfNeeded();
  await snap(settings, "180-a-missing-open");
  // Clicked again, it closes; another opens in its place.
  await cursor.click();
  await expect(info).toHaveCount(0);
  await missing.getByRole("listitem", { name: "Goose" }).getByRole("button").click();
  await expect(panel.getByRole("region", { name: "Goose" })).toContainText("goose");
  await app.close();
});

test("C: Claude Code with the default model: effort and mode open, and a new conversation runs with them", async () => {
  const { app, page } = await launch();
  await page.setViewportSize({ width: 1440, height: 900 });
  const roster = page.getByRole("region", { name: "Bot" });
  await roster.getByRole("group", { name: "owner-mac", exact: true }).getByRole("button", { name: /^Claude/ }).click({ timeout: 30_000 });
  const bot = page.getByRole("region", { name: /^Claude/ });
  const model = bot.getByRole("button", { name: "模型", exact: true });
  await expect(model.locator(".picker-v")).toHaveText("默认 · Sonnet 5.5", { timeout: 30_000 });
  // The default model's efforts, not a dead chip.
  const effort = bot.getByRole("button", { name: "推理强度", exact: true });
  await effort.click();
  const efforts = page.getByRole("menu", { name: "推理强度" }).getByRole("menuitemradio");
  await expect(efforts).toHaveText(["默认强度", "低", "中", "高", "超高", "最高"]);
  await page.keyboard.press("Escape");
  // Its modes: the three presets, and in place why its own ask-each-time is not one of them.
  const mode = bot.getByRole("button", { name: "模式", exact: true });
  await mode.click();
  const modes = page.getByRole("menu", { name: "模式" }).getByRole("menuitemradio");
  await expect(modes).toHaveText(["默认 · 完全权限", "仅可查看", "工作区内修改", "完全权限", /逐项询问/]);
  await expect(modes.last()).toBeDisabled();
  await snap(page, "180-c-modes");
  await page.keyboard.press("Escape");
  await pick(effort, "高");
  await pick(mode, "工作区内修改");
  await expect(effort.locator(".picker-v")).toHaveText("高");
  await expect(mode.locator(".picker-v")).toHaveText("工作区内修改");
  await snap(page, "180-c-chosen");
  const box = bot.getByRole("textbox", { name: /^给 Claude/ });
  await box.fill("hello");
  await box.press("Enter");
  await expect(bot.locator(".composer-state")).toContainText("已开新对话", { timeout: 30_000 });
  // The session runs with them: read back from the machine.
  const s = await page.evaluate(async () => {
    const r = await fetch("app://aide/api/aide.local.v1.LocalService/ListSessions", { method: "POST", headers: { "content-type": "application/json" }, body: "{}" });
    const all = ((await r.json()).sessions ?? []) as { agent: string; mode?: string; effort?: string; createdAt?: string }[];
    return all.filter((x) => x.agent === "claude").sort((a, b) => (a.createdAt ?? "").localeCompare(b.createdAt ?? "")).pop();
  });
  expect(s?.mode).toBe("workspace-write");
  expect(s?.effort).toBe("high");
  await app.close();
});
