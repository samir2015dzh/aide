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
  const list = await sessionList(page);
  await list.waitFor();
  const roster = page.getByRole("region", { name: "Bot" });
  await expect(roster.getByRole("button", { name: /派活/ })).toHaveCount(0);
  await expect(page.locator(".side-h").getByRole("button", { name: "派活" })).toHaveCount(0);
  const here = roster.getByRole("group", { name: /（这台电脑）$/ });
  await here.getByRole("button", { name: /^Gemini CLI/ }).click();
  const bot = page.getByRole("region", { name: /^Gemini CLI/ });
  const work = bot.getByRole("list", { name: "它的任务" });
  await bot.getByRole("textbox", { name: /^写给 Gemini CLI/ }).fill("say 直接派活");
  await bot.getByRole("button", { name: "派活", exact: true }).click();
  await expect(work.getByRole("listitem").filter({ hasText: "say 直接派活" })).toBeVisible({ timeout: 30_000 });

  // A contact, made on its page: a name, an avatar, its project.
  await bot.getByRole("region", { name: "名字与头像" }).getByRole("button", { name: "新建" }).click();
  const form = page.getByRole("dialog", { name: "添加联系人" });
  await form.getByLabel("名字", { exact: true }).fill("前端助手");
  await form.getByRole("group", { name: "头像" }).getByRole("button", { name: "fox" }).click();
  await expect(form.getByRole("group", { name: "机器" }).getByRole("button", { name: "owner-mac" })).toHaveAttribute("aria-pressed", "true");
  await expect(form.getByRole("group", { name: "Agent" }).getByRole("button", { name: "Gemini CLI" })).toHaveAttribute("aria-pressed", "true");
  await expect(form.getByLabel("项目", { exact: true })).toHaveValue("demo");
  await form.getByRole("button", { name: "保存" }).click();

  // The bot is called by it now, in the roster and on its page; writing goes on as it.
  await expect(here.getByRole("button", { name: /^前端助手/ })).toBeVisible();
  const named = page.getByRole("region", { name: "前端助手" });
  await expect(named.getByRole("combobox", { name: "以谁的身份写" })).toHaveValue(/.+/);
  await expect(named.getByText("下一条会按它的设置新开一个任务")).toBeVisible();
  await named.getByRole("textbox", { name: "写给 前端助手 的任务…" }).fill("say 联系人第一句");
  await named.getByRole("button", { name: "派活", exact: true }).click();
  await expect(named.getByRole("list", { name: "它的任务" }).getByRole("listitem").filter({ hasText: "say 联系人第一句" })).toBeVisible({ timeout: 30_000 });
  // Written to again: the same task goes on.
  await expect(named.locator(".bot-compose-via").getByRole("button", { name: "say 联系人第一句" })).toBeVisible({ timeout: 30_000 });
  const tasks = await list.getByRole("listitem").count();
  await named.getByRole("textbox", { name: "写给 前端助手 的任务…" }).fill("say 联系人第二句");
  await named.getByRole("button", { name: "派活", exact: true }).click();
  await named.locator(".bot-compose-via").getByRole("button", { name: "say 联系人第一句" }).click();
  await expect(page.getByRole("log", { name: "对话" })).toContainText("联系人第二句", { timeout: 30_000 });
  await expect(list.getByRole("listitem")).toHaveCount(tasks);

  // Deleted: the contact goes, the bot has its own name again, its task stays.
  await here.getByRole("button", { name: /^前端助手/ }).click();
  await page.getByRole("region", { name: "前端助手" }).getByRole("region", { name: "名字与头像" }).getByRole("button", { name: "编辑" }).click();
  await page.getByRole("dialog", { name: "编辑联系人" }).getByRole("button", { name: "删除联系人" }).click();
  await expect(here.getByRole("button", { name: /^Gemini CLI/ })).toBeVisible();
  await expect(list.getByText("say 联系人第一句")).toBeVisible();
  await app.close();
});
