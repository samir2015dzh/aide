import { expect, test } from "@playwright/test";
import { launch, sessionList } from "./app";

// Handing work out (工单 #41, docs/personas.md, as 批 G has it): there is no Hand Work To page beside the roster — a
// bot is picked in the roster and written to on its own page, with no contact made first. A contact is the bot's own
// name, avatar and presets, made on its page: the bot is then called by it in the roster, the first message starts a
// task with its project and agent, the next goes on in the same task. Deleting the contact keeps its task and gives
// the bot its own name back.
test("handing work to a detected agent from its page; a contact made there, written to twice, deleted", async () => {
  const { app, page } = await launch();
  await page.setViewportSize({ width: 1440, height: 800 });
  const roster = page.getByRole("region", { name: "Bot" });
  await expect(roster.getByRole("button", { name: /派活/ })).toHaveCount(0);
  const here = roster.getByRole("group", { name: "owner-mac" });
  await here.getByRole("button", { name: /^Gemini CLI/ }).click({ timeout: 30_000 });
  const bot = page.getByRole("region", { name: /^Gemini CLI/ });
  const convs = bot.getByRole("list", { name: "对话" });
  await bot.getByRole("textbox", { name: "给 Gemini CLI 发消息" }).fill("say 直接派活");
  await bot.getByRole("textbox", { name: "给 Gemini CLI 发消息" }).press("Enter");
  await expect(convs.getByRole("listitem").filter({ hasText: "say 直接派活" })).toBeVisible({ timeout: 30_000 });

  // A contact, made on its page (工单 #65: a short button, no sentence in its place): a name, an avatar, its project.
  await bot.getByRole("region", { name: "名字与头像" }).getByRole("button", { name: "给它起名字、换头像" }).click();
  const form = page.getByRole("dialog", { name: "添加联系人" });
  await form.getByLabel("名字", { exact: true }).fill("前端助手");
  await form.getByRole("group", { name: "头像" }).getByRole("button", { name: "fox" }).click();
  await expect(form.getByRole("group", { name: "机器" }).getByRole("button", { name: "owner-mac" })).toHaveAttribute("aria-pressed", "true");
  await expect(form.getByRole("group", { name: "Agent" }).getByRole("button", { name: "Gemini CLI" })).toHaveAttribute("aria-pressed", "true");
  await expect(form.getByRole("button", { name: "项目", exact: true })).toHaveText("demo");
  await form.getByRole("button", { name: "保存" }).click();

  // The bot is called by it now, in the roster and on its page; writing goes on in its current conversation (工单 #67).
  await expect(here.getByRole("button", { name: /^前端助手/ })).toBeVisible();
  const named = page.getByRole("region", { name: "前端助手" });
  const box = named.getByRole("textbox", { name: "给 前端助手 发消息" });
  const rows = named.getByRole("list", { name: "对话" }).getByRole("listitem");
  const count = await rows.count();
  await expect(named.locator(".conv-row.cur")).not.toContainText("进行中", { timeout: 60_000 });
  await box.fill("say 联系人第一句");
  await box.press("Enter");
  await expect(named.locator(".composer-state")).toContainText("已发到", { timeout: 30_000 });
  await expect(rows).toHaveCount(count);
  await named.locator(".conv-head-t").click();
  await expect(page.getByRole("log", { name: "对话" })).toContainText("联系人第一句", { timeout: 30_000 });

  // Its name changed alone (工单 #78): saving waits for a change, and a new name is one.
  await here.getByRole("button", { name: /^前端助手/ }).click();
  await page.getByRole("region", { name: "前端助手" }).getByRole("region", { name: "名字与头像" }).getByRole("button", { name: "修改「前端助手」" }).click();
  const edit = page.getByRole("dialog", { name: "编辑联系人" });
  await expect(edit.getByRole("button", { name: "保存" })).toBeDisabled();
  await edit.getByLabel("名字", { exact: true }).fill("前端助手二号");
  await expect(edit.getByRole("button", { name: "保存" })).toBeEnabled();
  await edit.getByRole("button", { name: "保存" }).click();
  await expect(here.getByRole("button", { name: /^前端助手二号/ })).toBeVisible();

  // Deleted, from its pencil: the contact goes, the bot has its own name again, its conversation stays.
  await here.getByRole("button", { name: /^前端助手二号/ }).click();
  await page.getByRole("region", { name: "前端助手二号" }).getByRole("region", { name: "名字与头像" }).getByRole("button", { name: "修改「前端助手二号」" }).click();
  await page.getByRole("dialog", { name: "编辑联系人" }).getByRole("button", { name: "删除联系人" }).click();
  await expect(here.getByRole("button", { name: /^Gemini CLI/ })).toBeVisible();
  const list = await sessionList(page);
  await expect(list.getByText("say 直接派活")).toBeVisible();
  await app.close();
});
