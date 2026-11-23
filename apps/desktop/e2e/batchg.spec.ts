import { expect, test } from "@playwright/test";
import { launch, snap } from "./app";

// 批 G on the desktop, as the owner reads it (工单 #56/#57): the app opens on the roster in the sidebar — the personal
// agents on the account, this computer's coding agents under it, a row each with one name and one state; a click
// opens the bot's own page, its work in the middle with a box to hand it work, its workbench beside it (its
// connection, its projects, pause and hiding, its names and faces); and a bot's page says of it what the roster says,
// in the same words — there is no Hand Work To page beside the roster. With AIDE_SCREENS the pictures go to the delivery record.

test("the roster is the first thing in the sidebar, and a bot opens its own page", async () => {
  const { page } = await launch();
  const roster = page.getByRole("region", { name: "Bot" });
  await expect(roster).toBeVisible({ timeout: 30_000 });
  // Personal agents on the account (the world's grant "bot"); coding agents in a group per machine, headed by its name (工单 #75).
  const personal = roster.getByRole("group", { name: "个人 agent" });
  await expect(personal.getByRole("button", { name: /bot/ })).toBeVisible();
  const coding = roster.getByRole("group", { name: "owner-mac" });
  const claude = coding.getByRole("button", { name: /^Claude Code/ });
  await expect(claude).toContainText("已登录", { timeout: 30_000 });
  await expect(coding.getByRole("button", { name: /^Codex/ })).toContainText("未登录");
  await snap(page, "batchg-roster");

  await claude.click();
  const bot = page.getByRole("region", { name: /^Claude Code/ });
  await expect(bot.getByRole("heading", { level: 1 })).toHaveText(/^Claude Code/);
  await expect(bot.locator(".bot-pill")).toHaveText("已登录");
  // Its conversations in the middle, with the box to write to it.
  await expect(bot.getByRole("list", { name: "对话" })).toBeVisible();
  await expect(bot.getByRole("textbox", { name: "给 Claude Code 发消息" })).toBeVisible();
  // Beside: its state in one line, its names and faces, its projects and how it runs; no device page link (工单 #63).
  const bench = bot.getByRole("complementary", { name: "工作台" });
  await expect(bench.getByRole("button", { name: "添加项目…" })).toBeVisible();
  await expect(bench.getByRole("button", { name: "暂停" })).toBeVisible();
  await expect(bench.getByRole("button", { name: "机器页" })).toHaveCount(0);
  await snap(page, "batchg-bot-page");

  // A personal agent's page: its grant and kind in its workbench (the app's own picker, 工单 #68).
  await personal.getByRole("button", { name: /bot/ }).click();
  const p = page.getByRole("region", { name: "bot" });
  await expect(p.getByRole("complementary", { name: "工作台" }).getByRole("button", { name: "类别" })).toBeVisible();
  await snap(page, "batchg-personal-page");
});

test("a bot's page says of it what the roster says, and why work can't go to it now (工单 #56)", async () => {
  const { page } = await launch();
  const roster = page.getByRole("region", { name: "Bot" });
  const codex = roster.getByRole("group", { name: "owner-mac" }).getByRole("button", { name: /^Codex/ });
  await expect(codex).toContainText("未登录", { timeout: 30_000 });
  await codex.click();
  const bot = page.getByRole("region", { name: "Codex" });
  await expect(bot.locator(".bot-pill")).toHaveText("未登录");
  // Not ready: said so in the machine page's words over the box, which can't send; the fix is on the devices page.
  await expect(bot.locator(".composer-why")).toContainText("未登录：现在派不了");
  await expect(bot.locator(".composer-why")).toContainText("Codex 装了但没登录");
  await expect(bot.getByRole("button", { name: "去设备页处理" })).toBeVisible();
  await snap(page, "batchg-dispatch");
});
