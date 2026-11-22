import { expect, test, type ElectronApplication, type Page } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";
import { launch, snap } from "./app";

// 批 D on the desktop, as the owner reads it (工单 #49/#46/#45/#41/#43/#47): this
// Mac's connections in one place, said the same on Connect Agent; every tab of
// Connect Agent with its icon; Hand Work To listing the detected agents; the
// map's links connected by default, not only under the pointer; and what the
// map's motion costs, moving, hidden and with less motion. With AIDE_SCREENS
// the pictures (and the costs, motion-cost.json) go to the delivery record.

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

test("this Mac's connections in one place, and the same rows on Connect Agent; every tab with its icon", async () => {
  const { app, page } = await launch();
  const { machines } = await api<{ machines: { machineId: string; local?: boolean }[] }>(page, "ListMachines");
  const mine = machines.find((m) => m.local)!;
  const settings = await openSettings(app, page, `machines/${mine.machineId}`);
  const card = settings.getByRole("region", { name: "本机连接状态" });
  await expect(card).toBeVisible({ timeout: 30_000 });
  // Each coding agent here in words (the command line Claude Code apart from the app), with when it was checked;
  // personal agents are said to follow the account.
  await expect(card.locator(".conn-row").filter({ hasText: "Claude Code 命令行" })).toContainText("已登录", { timeout: 30_000 });
  await expect(card.locator(".conn-row").filter({ hasText: "Codex" })).toContainText("未登录");
  await expect(card.getByText(/检测$/).first()).toBeVisible({ timeout: 30_000 });
  await expect(card).toContainText("个人 Agent（Muse、Grok Bot 等）跟着账号走");
  await snap(settings, "batchd-machine-connections");

  await settings.close();
  const connect = await openSettings(app, page, "connect");
  const main = connect.getByRole("main", { name: "连接" });
  const tabs = main.getByRole("tab");
  await expect(tabs).toHaveCount(5);
  // No tab without an icon (工单 #45): a logo, or a generic one for 其他 Agent and 助手.
  for (let i = 0; i < 5; i++) await expect(tabs.nth(i).locator("[aria-hidden=true]").first()).toBeVisible();
  // Each tab its own chip: none sits on the row of the one before as if it were its end.
  const boxes = await tabs.evaluateAll((els) => els.map((e) => e.getBoundingClientRect()).map((r) => ({ w: Math.round(r.width) })));
  expect(new Set(boxes.map((b) => b.w)).size).toBe(1);
  await snap(connect, "batchd-connect-tabs");
  await main.getByRole("tab", { name: "Claude Desktop" }).click();
  await expect(main.getByRole("region", { name: "本机连接状态" })).toContainText("Claude Code 命令行", { timeout: 30_000 });
  await snap(connect, "batchd-connect-claude");
  await app.close();
});

test("Hand Work To lists the detected agents by machine; the map's links read as connected without a hover", async () => {
  const { app, page } = await launch();
  const map = page.getByRole("region", { name: "链路" });
  await expect(map.locator(".lm-edge.up").first()).toBeVisible();
  await page.mouse.move(0, 0);
  // Connected, not hovered: the line is drawn in the connected colour, not the grey of what is off.
  const stroke = await map.locator(".lm-edge.up").first().evaluate((el) => getComputedStyle(el).stroke);
  const grey = await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue("--lm-line").trim());
  expect(stroke).not.toBe(grey);
  await snap(page, "batchd-map");

  await page.locator(".side-h").getByRole("button", { name: "派活" }).click();
  const sheet = page.getByRole("dialog", { name: "派活给谁" });
  await expect(sheet.getByRole("group", { name: "owner-mac" })).toBeVisible();
  await expect(sheet.getByRole("option").first()).toBeVisible();
  await snap(page, "batchd-hand-work");
  await app.close();
});

test("what the map's motion costs: moving, the window hidden, and with less motion", async () => {
  const { app, page } = await launch();
  const map = page.getByRole("region", { name: "链路" });
  await expect(map.locator(".lm-comet").first()).toBeAttached();
  // The renderer's CPU over a few seconds, as Electron counts it (percent of one core).
  const cpu = async () => {
    await app.evaluate(({ app: a }) => a.getAppMetrics());
    await page.waitForTimeout(5000);
    return app.evaluate(({ app: a }) => a.getAppMetrics().filter((m) => m.type === "Tab" || m.type === "GPU").reduce((n, m) => n + m.cpu.percentCPUUsage, 0));
  };
  const moving = await cpu();
  // Hidden: every animation stops (motion.ts). Xvfb has no window manager to tell the page it is hidden, so the
  // page is told as the system would tell it.
  const visible = (v: boolean) =>
    page.evaluate((v) => {
      Object.defineProperty(document, "hidden", { value: !v, configurable: true });
      document.dispatchEvent(new Event("visibilitychange"));
    }, v);
  await visible(false);
  await expect.poll(() => page.evaluate(() => document.documentElement.classList.contains("window-hidden"))).toBe(true);
  await expect.poll(() => page.evaluate(() => document.getAnimations().filter((a) => a.playState === "running").length)).toBe(0);
  const hidden = await cpu();
  await visible(true);
  await expect.poll(() => page.evaluate(() => document.getAnimations().filter((a) => a.playState === "running").length)).toBeGreaterThan(0);
  await page.emulateMedia({ reducedMotion: "reduce" });
  const reduced = await cpu();
  const out = { movingPercent: +moving.toFixed(2), hiddenPercent: +hidden.toFixed(2), reducedMotionPercent: +reduced.toFixed(2) };
  console.log("motion cost", JSON.stringify(out));
  if (process.env.AIDE_SCREENS) {
    fs.mkdirSync(process.env.AIDE_SCREENS, { recursive: true });
    fs.writeFileSync(path.join(process.env.AIDE_SCREENS, "motion-cost.json"), JSON.stringify(out, null, 2));
  }
  expect(hidden).toBeLessThanOrEqual(moving + 1);
  await app.close();
});
