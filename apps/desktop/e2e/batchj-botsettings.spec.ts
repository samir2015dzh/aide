import { expect, test, type Locator, type Page } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";
import { launch, snap } from "./app";

// 批 J, a bot's page and its settings (工单 #97) and the sidebar's first pages (工单 #102), as the owner meets them. A
// bot's page keeps its state, its conversations and, beside them, its task board; a 设置 button in its head opens its
// settings, a page of their own: every setting there, each once — its name and face among them — and back on its page
// what was changed there shows. The overview and 任务动态 are at the sidebar's top, one click from opening the app, and
// a task's state on 任务动态 is the one its bot's board shows. Played on DSH and Gemini CLI (found on this Mac), Muse (a
// personal agent that logged in once and isn't connected) and the world's agent "bot". With AIDE_SCREENS the pictures
// and the pages' heights go to the delivery record.

const heights: string[] = [];
async function height(name: string, scroller: Locator) {
  const [scroll, client] = await scroller.evaluate((e) => [e.scrollHeight, e.clientHeight]);
  heights.push(`${name}: scrollHeight=${scroll} clientHeight=${client}`);
  if (process.env.AIDE_SCREENS) fs.writeFileSync(path.join(process.env.AIDE_SCREENS, "after-heights.txt"), heights.join("\n") + "\n");
}

/** What a bot's page must not have any more: no setting of any kind (工单 #97). */
async function noSettings(bot: Locator) {
  for (const name of ["名字和头像", "类别", "暂停", "接入凭据", "能用什么", "项目与管理"]) await expect(bot.getByRole("region", { name, exact: true })).toHaveCount(0);
  for (const name of ["名字和头像", "名字和头像…", "从 Aide 里移除…", "断开…", "修改能用的范围", "添加项目…", "暂停", "给它起名字、换头像"]) await expect(bot.getByRole("button", { name, exact: true })).toHaveCount(0);
  await expect(bot.getByRole("radiogroup")).toHaveCount(0);
  await expect(bot.getByRole("switch")).toHaveCount(0);
}

async function openSettings(page: Page, bot: Locator, name: string): Promise<Locator> {
  const button = bot.locator(".bot-head").getByRole("button", { name: "设置", exact: true });
  await expect(button).toBeVisible();
  await button.click();
  const settings = page.getByRole("region", { name: `${name} 的设置` });
  await expect(settings).toBeVisible();
  return settings;
}

test("a coding agent's page has no settings; its settings page has each once, and what changes there shows on its page (工单 #97)", async () => {
  const { page } = await launch();
  await page.setViewportSize({ width: 1440, height: 900 });
  const roster = page.getByRole("region", { name: "Bot" });
  const mac = roster.getByRole("group", { name: "owner-mac", exact: true });
  await mac.getByRole("button", { name: /^DSH/ }).click({ timeout: 30_000 });
  const bot = page.getByRole("region", { name: "DSH", exact: true });
  // Its page: its head (face, name, state, the way to its settings), its conversations, its task board.
  await expect(bot.getByRole("heading", { level: 1 })).toHaveText("DSH");
  await expect(bot.getByRole("list", { name: "对话" })).toBeVisible();
  const board = bot.getByRole("complementary", { name: "任务看板" });
  await expect(board).toBeVisible();
  await expect(board).toContainText("DSH 现在没有任务");
  await noSettings(bot);
  await height("dsh-page board", board);
  await snap(page, "after-dsh-page");

  // Its settings: each there once, in one column no wider than 640 px.
  const settings = await openSettings(page, bot, "DSH");
  for (const name of ["连接", "能用什么", "类别", "名字和头像", "项目与管理", "暂停", "从 Aide 里移除…"]) await expect(settings.getByRole("region", { name, exact: true })).toHaveCount(1);
  await expect(settings.getByText("名字和头像", { exact: true })).toHaveCount(1);
  await expect(settings.getByRole("button", { name: /名字和头像|给它起名字/ })).toHaveCount(1);
  await expect(settings.getByRole("region", { name: "能用什么" })).toContainText("owner-mac（它在这台上运行）");
  await expect(settings.getByRole("button", { name: "修改能用的范围" })).toBeVisible();
  await expect(settings.getByRole("button", { name: "添加项目…" })).toBeVisible();
  expect(await settings.locator(".bset-col").evaluate((e) => e.getBoundingClientRect().width)).toBeLessThanOrEqual(640);
  // Its kind as radio rows, the current one marked — not a pop-up button (补充三).
  const kind = settings.getByRole("radiogroup", { name: "类别" });
  await expect(kind.getByRole("radio")).toHaveCount(3);
  await expect(kind.getByRole("radio", { checked: true })).toHaveText(/自动判断 · 本机编码 Agent/);
  await expect(settings.getByRole("button", { name: "类别" })).toHaveCount(0);
  // The Fluent look, on this page alone: 8 px cards with a 1 px stroke; 36 px choices; a 2 px ring with an offset.
  const card = settings.locator(".fgroup").first();
  expect(await card.evaluate((e) => [getComputedStyle(e).borderRadius, getComputedStyle(e).borderTopWidth])).toEqual(["8px", "1px"]);
  const first = kind.getByRole("radio").first();
  expect((await first.boundingBox())!.height).toBeGreaterThanOrEqual(36);
  await first.focus();
  await page.keyboard.press("Shift+Tab");
  await page.keyboard.press("Tab");
  expect(await first.evaluate((e) => [getComputedStyle(e).outlineWidth, getComputedStyle(e).outlineOffset])).toEqual(["2px", "2px"]);
  await height("dsh-settings", settings.locator(".bset"));
  await snap(page, "after-dsh-settings");
  await settings.locator(".bset").evaluate((e) => (e.scrollTop = e.scrollHeight));
  await snap(page, "after-dsh-settings-bottom");
  await page.emulateMedia({ colorScheme: "dark" });
  await settings.locator(".bset").evaluate((e) => (e.scrollTop = 0));
  await snap(page, "after-dsh-settings-dark");
  await page.emulateMedia({ colorScheme: "light" });
  // The rest of the app keeps its own controls: the composer's pop-up buttons aren't Fluent's.
  await expect(page.locator(".picker.fl")).toHaveCount(await settings.locator(".picker.fl").count());

  // Renamed there: called so on its settings, its page and in the roster.
  await settings.getByRole("button", { name: "给它起名字、换头像" }).click();
  const form = page.getByRole("dialog", { name: "添加联系人" });
  await form.getByLabel("名字", { exact: true }).fill("深思");
  await form.getByRole("button", { name: "保存" }).click();
  const renamed = page.getByRole("region", { name: "深思 的设置" });
  await expect(renamed).toBeVisible({ timeout: 30_000 });
  await expect(renamed.getByRole("region", { name: "名字和头像" })).toContainText("深思");
  await expect(mac.getByRole("button", { name: /^深思/ })).toBeVisible();

  // Its kind: filed personal there, it is listed with the personal agents; back to told from what it is, it returns.
  const personal = roster.getByRole("group", { name: "个人 agent", exact: true });
  await renamed.getByRole("radiogroup", { name: "类别" }).getByRole("radio", { name: "个人 Agent" }).click();
  await expect(personal.getByRole("button", { name: /^深思/ })).toBeVisible({ timeout: 30_000 });
  await expect(renamed.getByRole("radio", { checked: true })).toHaveText("个人 Agent");
  await snap(page, "after-dsh-settings-kind");
  await renamed.getByRole("radio", { name: /^自动判断/ }).click();
  await expect(personal.getByRole("button", { name: /^深思/ })).toHaveCount(0, { timeout: 30_000 });

  // Paused there: said in its head there, and on its page once back.
  await renamed.getByRole("button", { name: "暂停", exact: true }).click();
  await expect(renamed.locator(".bot-pill")).toHaveText("已暂停", { timeout: 30_000 });
  await renamed.getByRole("button", { name: "返回 深思" }).click();
  const back = page.getByRole("region", { name: "深思", exact: true });
  await expect(back.getByRole("heading", { level: 1 })).toHaveText("深思");
  await expect(back.locator(".bot-pill")).toHaveText("已暂停");
  await noSettings(back);
  const again = await openSettings(page, back, "深思");
  await again.getByRole("button", { name: "恢复", exact: true }).click();
  await expect(again.locator(".bot-pill")).not.toHaveText("已暂停", { timeout: 30_000 });

  // Removed from Aide there: asked first, then gone from the roster, and the window back on the overview.
  await again.getByRole("button", { name: "从 Aide 里移除…" }).click();
  const sheet = page.getByRole("dialog");
  await expect(sheet).toContainText("把 深思 从 Aide 里移除？");
  await sheet.getByRole("button", { name: "移除", exact: true }).click();
  await expect(mac.getByRole("button", { name: /^深思/ })).toHaveCount(0, { timeout: 30_000 });
  await expect(page.getByRole("heading", { name: "概览", level: 1 })).toBeVisible();
});

test("a personal agent's page has no grant card; its settings have its grant, its name and face once, and disconnecting it (工单 #97 补充)", async () => {
  const { page } = await launch();
  await page.setViewportSize({ width: 1440, height: 900 });
  const roster = page.getByRole("region", { name: "Bot" });
  await roster.getByRole("button", { name: /^Muse/ }).click({ timeout: 30_000 });
  const bot = page.getByRole("region", { name: "Muse", exact: true });
  await expect(bot.getByRole("complementary", { name: "任务看板" })).toBeVisible();
  await noSettings(bot);
  await height("muse-page board", bot.getByRole("complementary", { name: "任务看板" }));
  await snap(page, "after-muse-page");

  const settings = await openSettings(page, bot, "Muse");
  const grant = settings.getByRole("region", { name: "接入凭据" });
  await expect(grant).toContainText("能用什么");
  await expect(grant).toContainText("有效期");
  await expect(grant.getByRole("radiogroup", { name: "类别" })).toBeVisible();
  await expect(grant.getByRole("switch", { name: "执行命令" })).toBeVisible();
  await expect(settings.getByRole("button", { name: /名字和头像/ })).toHaveCount(1);
  await expect(grant.getByRole("button", { name: "断开…" })).toBeVisible();
  await height("muse-settings", settings.locator(".bset"));
  await snap(page, "after-muse-settings");
  await page.emulateMedia({ colorScheme: "dark" });
  await snap(page, "after-muse-settings-dark");
  await page.emulateMedia({ colorScheme: "light" });

  // Its exec switch, the Fluent way: on, then off again.
  const exec = grant.getByRole("switch", { name: "执行命令" });
  await exec.click();
  await expect(exec).toHaveAttribute("aria-checked", "true", { timeout: 30_000 });
  await snap(page, "after-muse-settings-switch-on");
  await exec.click();
  await expect(exec).toHaveAttribute("aria-checked", "false", { timeout: 30_000 });

  // Renamed there: called so in the roster, and on its page once back.
  await settings.getByRole("button", { name: "名字和头像…" }).click();
  const look = page.getByRole("dialog", { name: "名字和头像" });
  await look.getByRole("textbox").fill("缪斯");
  await look.getByRole("button", { name: "存储" }).click();
  await expect(roster.getByRole("button", { name: /^缪斯/ })).toBeVisible({ timeout: 30_000 });
  const renamed = page.getByRole("region", { name: "缪斯 的设置" });
  await renamed.getByRole("button", { name: "返回 缪斯" }).click();
  await expect(page.getByRole("region", { name: "缪斯", exact: true }).getByRole("heading", { level: 1 })).toHaveText("缪斯");

  // Disconnected there (its access revoked, 工单 #82's way): gone from the roster.
  const again = await openSettings(page, page.getByRole("region", { name: "缪斯", exact: true }), "缪斯");
  await again.getByRole("button", { name: "断开…" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "断开", exact: true }).click();
  await expect(roster.getByRole("button", { name: /^缪斯/ })).toHaveCount(0, { timeout: 30_000 });
  await expect(page.getByRole("heading", { name: "概览", level: 1 })).toBeVisible();
});

test("the overview and 任务动态 are first in the sidebar; a task's state there is the one on its bots' boards (工单 #102)", async () => {
  const { page } = await launch(undefined, { start: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  const nav = page.getByRole("navigation", { name: "导航" });
  const overview = nav.getByRole("button", { name: /^概览/ });
  const tasks = nav.getByRole("button", { name: /^任务动态/ });
  await expect(overview).toBeVisible({ timeout: 30_000 });
  await expect(tasks).toBeVisible();
  // At the top, above the roster, in no menu or folding group.
  const roster = page.getByRole("region", { name: "Bot" });
  expect((await tasks.boundingBox())!.y).toBeLessThan((await roster.boundingBox())!.y);
  expect(await nav.evaluate((e) => !!e.closest("[role=menu], [role=group]"))).toBe(false);
  await snap(page, "after-sidebar");
  // The account menu no longer has them.
  await page.getByRole("button", { name: "账号与设置" }).click();
  const menu = page.getByRole("menu", { name: "账号与设置" });
  await expect(menu.getByRole("menuitem", { name: /^全部任务/ })).toBeVisible();
  await expect(menu.getByRole("menuitem", { name: /^(概览|任务动态)/ })).toHaveCount(0);
  await snap(page, "after-sidebar-account-menu");
  await page.keyboard.press("Escape");

  // One click each.
  await tasks.click();
  await expect(page.getByRole("heading", { name: "任务动态", level: 1 })).toBeVisible();
  await expect(tasks).toHaveAttribute("aria-current", "true");
  const feed = page.getByRole("list", { name: "派出的活" });
  const row = feed.getByRole("listitem", { name: "选颜色" });
  await expect(row).toBeVisible({ timeout: 30_000 });
  const state = (await row.locator(".task-state").innerText()).trim();
  expect(state).not.toBe("");
  await snap(page, "after-tasks");
  await overview.click();
  await expect(page.getByRole("heading", { name: "概览", level: 1 })).toBeVisible();

  // The same task on the board of the bot that handed it out (bot) and of the one doing it (Gemini CLI): the same state.
  for (const [name, re] of [["bot", /^bot/], ["Gemini CLI", /^Gemini CLI/]] as const) {
    await roster.getByRole("button", { name: re }).first().click();
    const board = page.getByRole("region", { name, exact: true }).getByRole("complementary", { name: "任务看板" });
    const same = board.getByRole("listitem", { name: "选颜色" });
    await expect(same).toBeVisible();
    await expect(same.locator(".task-state")).toHaveText(state);
    // Each of its groups as the feed ranks it: waiting / needs a look first.
    await expect(board.getByRole("region").first()).toHaveAccessibleName("等待 / 要看一眼");
    if (name === "bot") await height("bot-page board", board);
    await snap(page, `after-${name === "bot" ? "bot" : "gemini"}-page`);
  }
  // Every row on 任务动态 is on one of the boards: Gemini CLI's (the agent doing all the world's work) has them all.
  await tasks.click();
  const all = await feed.getByRole("listitem").count();
  await roster.getByRole("button", { name: /^Gemini CLI/ }).first().click();
  await expect(page.getByRole("region", { name: "Gemini CLI", exact: true }).getByRole("complementary", { name: "任务看板" }).getByRole("listitem")).toHaveCount(all);
});
