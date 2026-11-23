import { expect, test, type ElectronApplication, type Page } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";
import { fixture, launch, onWindows, snap } from "./app";

// 批 J, 工单 #108, as the owner met it: the Claude desktop app signed in on this Mac, Claude Code's command line (what
// Aide's tasks run with) not. Its bot says so — not signed in, never "signed in" on the app's word — with the way to sign
// it in from Aide right there (its machine runs `claude auth login`; a browser would open on it); signed in, a task runs;
// its sign-in gone, it says not signed in again. A command line that says it is signed in but whose session fails "Not
// logged in" is signed out at once, and the failure's card points to the same entry. One that says nothing clear is
// unconfirmed. The stand-in Claude Code (signInClaude, e2e/batchj_login_test.go) keeps its sign-in in fixture.claudeDir.

test.skip(onWindows, "its stand-in Claude Code is a shell script");
// In order: each step is the owner's next one on the same Mac.
test.describe.configure({ mode: "serial", timeout: 180_000 });

const marker = (name: string, on: boolean) => {
  const file = path.join(fixture.claudeDir!, name);
  if (on) fs.writeFileSync(file, "");
  else fs.rmSync(file, { force: true });
};

async function recheck(page: Page) {
  await page.evaluate(async () => {
    await fetch("app://aide/api/aide.local.v1.LocalService/CheckAgents", { method: "POST", headers: { "content-type": "application/json" }, body: "{}" });
  });
}

async function openClaude(page: Page) {
  await page.setViewportSize({ width: 1440, height: 900 });
  const roster = page.getByRole("region", { name: "Bot" });
  await roster.getByRole("group", { name: "owner-mac", exact: true }).getByRole("button", { name: /^Claude/ }).click({ timeout: 30_000 });
  return page.getByRole("region", { name: /^Claude/ });
}

async function openSettings(app: ElectronApplication, page: Page, tab: string): Promise<Page> {
  const [win] = await Promise.all([app.waitForEvent("window"), page.evaluate((t) => window.aide?.openSettings(t), tab)]);
  await win.waitForLoadState();
  return win;
}

test("signed out while the app is signed in: said so, signed in from its page, a task runs, signed out again", async () => {
  for (const m of ["signed-in", "claims-in", "vague"]) marker(m, false);
  const { app, page } = await launch();
  await recheck(page);
  const bot = await openClaude(page);
  const head = bot.locator(".bot-head");
  await expect(head.locator(".bot-head-l1")).toContainText("未登录", { timeout: 30_000 });
  await expect(head).toContainText("Desktop 的登录不会续给它");
  const login = head.getByRole("button", { name: "在这台电脑上登录" });
  await expect(login).toBeVisible();
  await expect(head).not.toContainText("在终端");
  await snap(page, "batchj-login-1-signed-out");

  // The machine's own list says the two apart: the app signed in, the command line not.
  const { machines } = await page.evaluate(async () => (await fetch("app://aide/api/aide.local.v1.LocalService/ListMachines", { method: "POST", headers: { "content-type": "application/json" }, body: "{}" })).json());
  const settings = await openSettings(app, page, `machines/${machines.find((m: { local?: boolean }) => m.local).machineId}`);
  const list = settings.getByRole("list", { name: "编码 Agent" });
  await expect(list.getByRole("group", { name: "Claude Desktop" })).toContainText("已登录", { timeout: 30_000 });
  await expect(list.getByRole("group", { name: "Claude Code 命令行" })).toContainText("未登录");
  await snap(settings, "batchj-login-2-machine-rows");
  await settings.close();

  // Its settings' 连接 has the same entry.
  await head.getByRole("button", { name: "设置", exact: true }).click();
  const set = page.getByRole("region", { name: /的设置$/ });
  await expect(set.getByRole("region", { name: "连接", exact: true }).getByRole("button", { name: "在这台电脑上登录" })).toBeVisible();
  await snap(page, "batchj-login-3-settings");
  await set.locator(".own-back").click();

  // Signed in from here: waiting for the browser, then signed in, by the machine's own check.
  await bot.locator(".bot-head").getByRole("button", { name: "在这台电脑上登录" }).click();
  await expect(bot.locator(".bot-head").getByRole("button", { name: "取消登录" })).toBeVisible();
  await snap(page, "batchj-login-4a-waiting");
  await expect(bot.locator(".bot-head-l1")).toContainText("已登录", { timeout: 30_000 });
  await expect(bot.locator(".bot-head").getByRole("button", { name: "在这台电脑上登录" })).toHaveCount(0);
  await snap(page, "batchj-login-4-signed-in");

  // A task runs.
  const box = bot.getByRole("textbox", { name: /^给 Claude/ });
  await box.fill("fix the header");
  await box.press("Enter");
  await expect(bot.locator(".composer-state")).toContainText("已开新对话", { timeout: 30_000 });
  await bot.locator(".conv-head-t").click();
  await expect(page.getByRole("log", { name: "对话" })).toContainText("done, signed in", { timeout: 30_000 });
  await snap(page, "batchj-login-5-task-ran");

  // Its sign-in gone: at the next check, not signed in again, the entry back.
  marker("signed-in", false);
  await recheck(page);
  const again = await openClaude(page);
  await expect(again.locator(".bot-head-l1")).toContainText("未登录", { timeout: 30_000 });
  await expect(again.locator(".bot-head").getByRole("button", { name: "在这台电脑上登录" })).toBeVisible();
  await snap(page, "batchj-login-6-signed-out-again");
  await app.close();
});

test("it says signed in, its session fails Not logged in: signed out at once, the card points to signing in", async () => {
  for (const m of ["signed-in", "vague"]) marker(m, false);
  marker("claims-in", true);
  const { app, page } = await launch();
  await recheck(page);
  const bot = await openClaude(page);
  await expect(bot.locator(".bot-head-l1")).toContainText("已登录", { timeout: 30_000 });
  // A new conversation: the last one's Claude Code is still running, signed in.
  if (await bot.locator(".conv-new").isEnabled()) await bot.locator(".conv-new").click();
  const box = bot.getByRole("textbox", { name: /^给 Claude/ });
  await box.fill("fix the footer");
  await box.press("Enter");
  // No check runs (it would still say signed in): the session's own failure signs it out.
  await expect(bot.locator(".bot-head-l1")).toContainText("未登录", { timeout: 30_000 });
  await bot.locator(".conv-head-t").click();
  const card = page.locator(".qcard").filter({ hasText: "Claude Code 未登录" });
  await expect(card).toContainText("Claude Code 未登录", { timeout: 30_000 });
  await expect(card).toContainText("Claude Desktop 的登录不会续给它");
  await expect(card).not.toContainText("运行一次对应的 CLI");
  await expect(card.getByRole("button", { name: "在这台电脑上登录" })).toBeVisible();
  await snap(page, "batchj-login-7-failed-card");
  marker("claims-in", false);
  await card.getByRole("button", { name: "在这台电脑上登录" }).click();
  await expect(card.getByRole("button", { name: "在这台电脑上登录" })).toHaveCount(0, { timeout: 30_000 });
  const back = await openClaude(page);
  await expect(back.locator(".bot-head-l1")).toContainText("已登录", { timeout: 30_000 });
  await app.close();
});

test("it says nothing clear: unconfirmed, never the app's sign-in, and the entry is there", async () => {
  for (const m of ["signed-in", "claims-in"]) marker(m, false);
  marker("vague", true);
  const { app, page } = await launch();
  await recheck(page);
  const bot = await openClaude(page);
  await expect(bot.locator(".bot-head-l1")).toContainText("未确认", { timeout: 30_000 });
  await expect(bot.locator(".bot-head")).toContainText("不拿 Claude Desktop 的登录代替它");
  await expect(bot.locator(".bot-head").getByRole("button", { name: "在这台电脑上登录" })).toBeVisible();
  await snap(page, "batchj-login-8-unconfirmed");
  marker("vague", false);
  await app.close();
});
