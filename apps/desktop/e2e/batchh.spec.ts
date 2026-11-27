import { expect, test } from "@playwright/test";
import { answerMenu, launch, snap } from "./app";

// 批 H on the desktop, as the owner uses it.
// 工单 #63: the sidebar is bots alone — no pages, machines or task list; the rest is in the account menu at its foot.
// 工单 #66/#67: a coding agent's page goes on in its current conversation through one capsule box; "开启新对话"
// starts another; the earlier one is listed, chosen again and gone on in; archived, it is the machine's own archive.
// The bot here is this computer's Gemini CLI, the scripted agent: "say X" answers X.

test("the sidebar is bots alone; pages that left it are in the account menu, devices on the devices page (工单 #63)", async () => {
  const { app, page } = await launch();
  const side = page.getByRole("complementary", { name: /边栏|Sidebar/ }).first();
  const roster = page.getByRole("region", { name: "Bot" });
  await expect(roster).toBeVisible({ timeout: 30_000 });
  // No machine group, no task list; the one navigation is the overview and the task feed at the top (工单 #102).
  await expect(page.locator(".side-nav").getByRole("button")).toHaveText([/^概览/, /^任务动态/]);
  await expect(page.locator(".side-sessions")).toHaveCount(0);
  await expect(roster.getByRole("group", { name: /这台电脑|这台 Mac/ })).toHaveCount(0);
  await expect(roster.locator(".bot-counts")).toHaveCount(0);
  // Pinned as a tile at the top (BotArk's sidebar), from its right-click menu.
  const gemini = roster.getByRole("group", { name: "owner-mac" }).getByRole("button", { name: /^Gemini CLI/ });
  await expect(gemini).toBeVisible({ timeout: 30_000 });
  await answerMenu(app, "pin");
  await gemini.click({ button: "right" });
  await expect(roster.getByRole("list", { name: "置顶" }).getByRole("listitem", { name: /Gemini CLI/ })).toBeVisible();
  await snap(page, "batchh-sidebar");
  // The account menu at the foot: every session, devices, and the rest; the overview and task feed are at the top (工单 #102).
  await side.locator(".side-foot").click();
  const menu = page.getByRole("menu", { name: "账号与设置" });
  for (const item of ["全部任务", "设备", "设置…"]) await expect(menu.getByRole("menuitem", { name: new RegExp(item) })).toBeVisible();
  await snap(page, "batchh-account-menu");
  await menu.getByRole("menuitem", { name: /全部任务/ }).click();
  await expect(page.getByRole("list", { name: "任务" })).toBeVisible();
});

test("a coding agent's conversations: on in the current one, a new one, back to the first (工单 #66, #67)", async () => {
  const { page } = await launch();
  const roster = page.getByRole("region", { name: "Bot" });
  await roster.getByRole("group", { name: "owner-mac" }).getByRole("button", { name: /^Gemini CLI/ }).click({ timeout: 30_000 });
  const bot = page.getByRole("region", { name: /^Gemini CLI/ });
  const composer = bot.locator("form.composer");
  const box = composer.getByRole("textbox", { name: "给 Gemini CLI 发消息" });
  const send = composer.getByRole("button", { name: "发送" });
  const stream = bot.locator(".bot-stream");
  const current = stream.locator(".conv-current-card");
  const allBtn = bot.locator(".conv-all-btn");
  const flyout = bot.locator(".conv-flyout");
  // The box (工单 #92): + at its foot on the left, the project as a chip under it, the send button lit only with something to send.
  await expect(composer.getByRole("button", { name: "更多" })).toBeVisible();
  await expect(composer.getByRole("button", { name: "项目" })).toBeVisible();
  await expect(send).toBeDisabled();

  // Conversations are folded into the Fluent "全部对话" flyout (#107 §0.5c #4A); open it to count existing rows.
  await allBtn.click();
  await expect(flyout).toBeVisible();
  const flyRows = flyout.getByRole("listitem");
  await expect(flyRows.first()).toBeVisible({ timeout: 30_000 });
  const n = await flyRows.count();
  await flyout.getByRole("button", { name: "关闭对话列表" }).click();
  await expect(flyout).toHaveCount(0);

  // A new conversation first (rule 2): the next message starts one, said at the top.
  if (await bot.locator(".conv-new").isEnabled()) {
    await bot.locator(".conv-new").click();
    await expect(bot.locator(".conv-head")).toContainText("下一条会开新对话");
  } else await expect(bot.locator(".conv-head")).toContainText("写第一条");
  await box.fill("say 第一条");
  await expect(send).toBeEnabled();
  await box.press("Enter");
  await expect(bot.locator(".composer-state")).toHaveText("已开新对话", { timeout: 30_000 });
  // It is the current one, by its title and when it started (rule 3).
  await expect(current).toHaveCount(1, { timeout: 30_000 });
  // While its turn runs it is "正在聊"; done, "最近对话" (#135 P03): which one depends on how fast the agent answers.
  await expect(bot.locator(".conv-head")).toContainText(/正在聊：|最近对话：/);
  const firstTitle = (await current.locator(".conv-current-top b").textContent())!;
  // The next message goes on in it (rule 1): no new conversation.
  await expect(current).not.toContainText("进行中", { timeout: 60_000 });
  // Its turn done, the head says so, by its title (审计 B: the either-or above alone would pass whatever it said).
  await expect(bot.locator(".conv-head")).toContainText(`最近对话：${firstTitle}`);
  await expect(bot.locator(".conv-head")).not.toContainText("正在聊：");
  await box.fill("say 第二条");
  await box.press("Enter");
  await expect(bot.locator(".composer-state")).toContainText("已发到", { timeout: 30_000 });
  await allBtn.click();
  await expect(flyRows).toHaveCount(n + 1, { timeout: 30_000 });
  await flyout.getByRole("button", { name: "关闭对话列表" }).click();
  await snap(page, "batchh-conversation");

  // Another new one: the first stays listed in the "全部对话" flyout.
  await bot.locator(".conv-new").click();
  await box.fill("say 另起一条");
  await box.press("Enter");
  await expect(current).not.toContainText(firstTitle, { timeout: 30_000 });

  // Back to the first (rule 5): chosen from "全部对话" flyout, it is current again, and the next message goes on in it.
  await expect(current).not.toContainText("进行中", { timeout: 60_000 });
  await allBtn.click();
  await expect(flyRows).toHaveCount(n + 2, { timeout: 30_000 });
  await flyRows.filter({ hasText: firstTitle }).locator(".conv-main").click();
  await expect(flyout).toHaveCount(0);
  await expect(current).toContainText(firstTitle);
  await box.fill("say 回到第一条");
  await box.press("Enter");
  await expect(bot.locator(".composer-state")).toContainText(`已发到「${firstTitle}」`, { timeout: 30_000 });
  await allBtn.click();
  await expect(flyRows).toHaveCount(n + 2);
  await flyout.getByRole("button", { name: "关闭对话列表" }).click();

  // Archived (rule 6): the machine's own archive, apart in its group; going on in it unarchives it first.
  await expect(current).not.toContainText("进行中", { timeout: 60_000 });
  await current.getByRole("button", { name: "归档" }).click();
  await expect(bot.locator(".composer-state")).toContainText("已归档", { timeout: 30_000 });
  await allBtn.click();
  await expect(flyRows).toHaveCount(n + 1, { timeout: 30_000 });
  await expect(flyout.getByRole("button", { name: /^已归档（\d+）/ })).toBeVisible();
  await snap(page, "batchh-archived");
});

test("deleting a room says what becomes of its members' sessions, and lets the owner choose (工单 #73)", async () => {
  const { page } = await launch();
  const roster = page.getByRole("region", { name: "Bot" });
  const rooms = roster.getByRole("group", { name: "房间" });
  await rooms.getByRole("button", { name: "新建房间" }).click({ timeout: 30_000 });
  const sheet = page.getByRole("dialog");
  await sheet.getByRole("textbox", { name: "房间名" }).fill("要删的房间");
  await sheet.getByRole("checkbox", { name: "Gemini CLI" }).click();
  await sheet.getByRole("button", { name: "建房间" }).click();
  const room = page.getByRole("region", { name: "要删的房间" });
  await room.getByRole("group", { name: "点名" }).getByRole("button", { name: "@Gemini CLI" }).click();
  await room.getByRole("textbox", { name: "在房间里说…" }).fill("say 在");
  await room.getByRole("button", { name: "发送" }).click();
  await expect(room.getByRole("log", { name: "时间线" }).getByText("Gemini CLI 已回复")).toBeVisible({ timeout: 60_000 });

  await room.getByRole("button", { name: "删除房间…" }).click();
  const del = page.getByRole("dialog");
  await expect(del.getByRole("radio", { name: /只删房间/ })).toHaveAttribute("aria-checked", "true");
  await expect(del.getByRole("radio", { name: /连同成员会话一起删/ })).toBeVisible();
  await expect(del).toContainText("受影响的会话（1）");
  await expect(del).toContainText("Gemini CLI");
  await snap(page, "batchh-delete-room");
  await del.getByRole("radio", { name: /连同成员会话一起删/ }).click();
  await del.getByRole("button", { name: "删除房间和会话" }).click();
  await expect(page.getByRole("status").filter({ hasText: "已删除房间「要删的房间」和它的成员会话" })).toBeVisible({ timeout: 30_000 });
  await expect(rooms.getByRole("button", { name: /要删的房间/ })).toHaveCount(0, { timeout: 30_000 });
});
