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
  // The counts at the top, ready over all, the unknown apart when there are any (工单 #56).
  await expect(roster.locator(".bot-counts")).toHaveText(/^可用 \d+( · 未知 \d+)? · 共 \d+$/);
  // Personal agents on the account (the world's grant "bot"), coding agents under their machine.
  const personal = roster.getByRole("group", { name: "个人 agent · 跟账号走" });
  await expect(personal.getByRole("button", { name: /bot/ })).toBeVisible();
  const here = roster.getByRole("group", { name: /（这台电脑）$/ });
  const claude = here.getByRole("button", { name: /^Claude Code/ });
  await expect(claude).toContainText("已登录", { timeout: 30_000 });
  await expect(here.getByRole("button", { name: /^Codex/ })).toContainText("未登录");
  await snap(page, "batchg-roster");

  await claude.click();
  const bot = page.getByRole("region", { name: /^Claude Code/ });
  await expect(bot.getByRole("heading", { level: 1 })).toHaveText(/^Claude Code/);
  // Who it is, where, and how it stands, in one line at the top.
  await expect(bot.locator(".bot-head")).toContainText("这台电脑");
  await expect(bot.locator(".bot-pill")).toHaveText("已登录");
  // Its work in the middle, with the box to hand it work.
  await expect(bot.getByRole("list", { name: "它的任务" })).toBeVisible();
  await expect(bot.getByRole("textbox", { name: /写给 Claude Code/ })).toBeVisible();
  // Its workbench: its connection (the command line as a part of it), its projects and what the machine page managed.
  const bench = bot.getByRole("complementary", { name: "工作台" });
  await expect(bench).toContainText("命令行");
  await expect(bench.getByRole("button", { name: "添加项目…" })).toBeVisible();
  await expect(bench.getByRole("button", { name: "暂停" })).toBeVisible();
  await expect(bench.getByRole("button", { name: "从列表中隐藏" })).toBeVisible();
  await snap(page, "batchg-bot-page");

  // A personal agent's page: its grant and kind in its workbench, its chat to come with rooms.
  await personal.getByRole("button", { name: /bot/ }).click();
  const p = page.getByRole("region", { name: "bot" });
  await expect(p.locator(".bot-head")).toContainText("跟账号走");
  await expect(p.getByRole("complementary", { name: "工作台" }).getByRole("combobox", { name: "类别" })).toBeVisible();
  await snap(page, "batchg-personal-page");
});

test("a bot's page says of it what the roster says, and why work can't go to it now (工单 #56)", async () => {
  const { page } = await launch();
  const roster = page.getByRole("region", { name: "Bot" });
  // No Hand Work To page beside the roster: the roster is whom to, the bot's page is where to write.
  await expect(page.locator(".side-h").getByRole("button", { name: "派活" })).toHaveCount(0);
  const here = roster.getByRole("group", { name: /（这台电脑）$/ });
  const codex = here.getByRole("button", { name: /^Codex/ });
  await expect(codex).toContainText("未登录", { timeout: 30_000 });
  await codex.click();
  const bot = page.getByRole("region", { name: "Codex" });
  await expect(bot.locator(".bot-pill")).toHaveText("未登录");
  // Not ready: said so in the machine page's words, and the box can't send.
  await expect(bot.locator(".bot-compose-why")).toContainText("未登录：现在派不了");
  await expect(bot.locator(".bot-compose-why")).toContainText("Codex 装了但没登录");
  await snap(page, "batchg-dispatch");
});
