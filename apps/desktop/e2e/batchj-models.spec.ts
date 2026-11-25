import { expect, test, type Locator, type Page } from "@playwright/test";
import { launch, pick, snap } from "./app";

// 批 J, the models a conversation runs on, as the owner meets them on a bot's page. Claude Code (工单 #99): its list
// names each model's specific one, as Claude Code itself listed them on this machine ("Opus 5.5", claude-opus-5-5
// below it), the default too; one chosen runs, and the conversation reads back the model it runs. DSH (工单 #98): the
// box has a model choice as Claude's has; DSH names its models only in a session, so before its first here the list
// says so, and after it lists DSH's own; one chosen starts a new conversation (工单 #67 rule 4, said so) that runs it.

/** The bot's model list, opened: each entry's text (its title, and below it the specific model in full). */
async function modelList(page: Page, chip: Locator): Promise<string[]> {
  await chip.click();
  const items = page.getByRole("menu", { name: "模型" }).getByRole("menuitemradio");
  await expect(items.first()).toBeVisible();
  return items.allInnerTexts().then((all) => all.map((x) => x.replace(/\s+/g, " ").trim()));
}

test("Claude Code: each model in the list is its specific one, the chosen one runs and is read back", async () => {
  const { app, page } = await launch();
  await page.setViewportSize({ width: 1440, height: 900 });
  const roster = page.getByRole("region", { name: "Bot" });
  await roster.getByRole("group", { name: "owner-mac", exact: true }).getByRole("button", { name: /^Claude/ }).click({ timeout: 30_000 });
  const bot = page.getByRole("region", { name: /^Claude/ });
  const chip = bot.getByRole("button", { name: "模型", exact: true });
  // The default names the model it runs; every entry its specific model, its family at most a part of its name.
  await expect(chip).toHaveText("默认 · Sonnet 5.5", { timeout: 30_000 });
  const listed = await modelList(page, chip);
  expect(listed).toEqual(["默认 · Sonnet 5.5 claude-sonnet-5-5", "Sonnet 5.5 claude-sonnet-5-5", "Fable 5.2 claude-fable-5-2", "Opus 5.5 claude-opus-5-5", "Haiku 5.5 claude-haiku-5-5"]);
  for (const family of ["Opus", "Sonnet", "Fable", "Haiku"]) expect(listed).not.toContain(family);
  await snap(page, "batchj-claude-models");
  await page.keyboard.press("Escape");

  // A model other than the default: the conversation runs it, and the box and the session read back the one it runs.
  await pick(chip, /^Opus 5\.5/);
  await expect(chip).toHaveText("Opus 5.5");
  const box = bot.getByRole("textbox", { name: /^给 Claude/ });
  await box.fill("hello");
  await box.press("Enter");
  await expect(bot.locator(".composer-state")).toContainText("已开新对话", { timeout: 30_000 });
  await expect(bot.locator(".conv-head-t")).toBeVisible();
  await expect(chip).toHaveText("Opus 5.5");
  await snap(page, "batchj-claude-ran");
  await bot.locator(".conv-head-t").click();
  await expect(page.getByRole("log", { name: "对话" })).toContainText("running on claude-opus-5-5", { timeout: 30_000 });
  await page.getByRole("button", { name: "会话设置", exact: true }).click();
  await expect(page.getByRole("dialog", { name: "会话设置" }).getByRole("complementary", { name: "上下文" })).toContainText("Opus 5.5 · claude-opus-5-5");
  await page.keyboard.press("Escape");
  // In the conversation (工单 #98 补充): another model there starts a new conversation on it, said so.
  const mbox = page.locator(".mbox");
  const inChat = mbox.getByRole("button", { name: "模型", exact: true });
  await expect(inChat).toHaveText("Opus 5.5");
  await pick(inChat, /^Haiku 5\.5/);
  await expect(mbox.getByRole("note")).toContainText("换了模型：下一条会开新对话");
  await mbox.getByRole("textbox", { name: "发给 Claude Code 的消息" }).fill("again");
  await mbox.getByRole("textbox", { name: "发给 Claude Code 的消息" }).press("Enter");
  await expect(page.getByRole("status").filter({ hasText: "已开新对话：换了模型" })).toBeVisible({ timeout: 30_000 });
  await expect(page.getByRole("log", { name: "对话" })).toContainText("running on claude-haiku-5-5", { timeout: 30_000 });
  await expect(inChat).toHaveText("Haiku 5.5");
  await snap(page, "batchj-claude-in-chat-ran");
  await app.close();
});

test("DSH: a model choice as Claude's, its models as DSH names them here, each once, a custom provider's too; another one, on the bot's page or in the conversation, starts a new conversation on it", async () => {
  const { app, page } = await launch();
  await page.setViewportSize({ width: 1440, height: 900 });
  const roster = page.getByRole("region", { name: "Bot" });
  const open = () => roster.getByRole("group", { name: "owner-mac", exact: true }).getByRole("button", { name: /^DSH/ }).click({ timeout: 30_000 });
  await open();
  const bot = page.getByRole("region", { name: /^DSH/ });
  const chip = bot.getByRole("button", { name: "模型", exact: true });
  // Before its first session here: the choice is there, and says when DSH's models show.
  await expect(chip).toHaveText("默认模型", { timeout: 30_000 });
  expect(await modelList(page, chip)).toEqual(["默认模型", "DSH 只在会话里告诉可用的模型：先用默认模型发出第一条，之后这里列出它在这台机器上的实际模型"]);
  await snap(page, "batchj-dsh-models-before");
  await page.keyboard.press("Escape");
  const box = bot.getByRole("textbox", { name: /^给 DSH/ });
  await box.fill("config");
  await box.press("Enter");
  await expect(bot.locator(".composer-state")).toContainText("已开新对话", { timeout: 30_000 });

  // Now its own, as DSH named them in that session (工单 #98 补充, #117): each model once — the same one by DSH's API and
  // account lines is one entry, named uniformly provider-first ("<provider> · <id>"), both lines said — and the custom
  // provider's (magpie, in a model option of its own) listed too. The conversation runs its default, read back.
  await expect(chip).toHaveText("DeepSeek · deepseek-v4-flash", { timeout: 30_000 });
  const listed = await modelList(page, chip);
  expect(listed).toEqual([
    "默认 · DeepSeek · deepseek-v4-flash deepseek/deepseek-v4-flash · DeepSeek · DeepSeek Account",
    "DeepSeek · deepseek-v4-flash deepseek/deepseek-v4-flash · DeepSeek · DeepSeek Account",
    "DeepSeek · deepseek-v4-pro deepseek/deepseek-v4-pro · DeepSeek",
    "magpie · group/deepseek-v4.1 magpie/group/deepseek-v4.1 · magpie",
  ]);
  expect(new Set(listed.slice(1)).size).toBe(listed.length - 1);
  await snap(page, "batchj-dsh-models");
  await page.getByRole("menuitemradio", { name: /^magpie · group\/deepseek-v4\.1/ }).click();
  await expect(bot.getByRole("note")).toContainText("换了模型：下一条会开新对话");
  await box.fill("config");
  await box.press("Enter");
  await expect(bot.locator(".composer-state")).toContainText("已开新对话：换了模型", { timeout: 30_000 });
  await expect(chip).toHaveText("magpie · group/deepseek-v4.1");
  await snap(page, "batchj-dsh-ran");
  await bot.locator(".conv-head-t").click();
  const log = page.getByRole("log", { name: "对话" });
  await expect(log).toContainText("config: magpie/group/deepseek-v4.1", { timeout: 30_000 });

  // In the conversation itself (工单 #98 补充): its model can be changed there too; the next message starts a new
  // conversation on the one picked, said before and after, which runs it and reads it back.
  const mbox = page.locator(".mbox");
  const inChat = mbox.getByRole("button", { name: "模型", exact: true });
  await expect(inChat).toHaveText("magpie · group/deepseek-v4.1");
  await pick(inChat, /^DeepSeek · deepseek-v4-pro/);
  await expect(mbox.getByRole("note")).toContainText("换了模型：下一条会开新对话");
  await snap(page, "batchj-dsh-in-chat-switch");
  const chatBox = mbox.getByRole("textbox", { name: "发给 DSH 的消息" });
  await chatBox.fill("config");
  await chatBox.press("Enter");
  await expect(page.getByRole("status").filter({ hasText: "已开新对话：换了模型" })).toBeVisible({ timeout: 30_000 });
  await expect(log).toContainText("config: deepseek/deepseek-v4-pro", { timeout: 30_000 });
  await expect(inChat).toHaveText("DeepSeek · deepseek-v4-pro");
  await expect(mbox.getByRole("note")).toHaveCount(0);
  await snap(page, "batchj-dsh-in-chat-ran");
  await page.getByRole("button", { name: "会话设置", exact: true }).click();
  await expect(page.getByRole("dialog", { name: "会话设置" }).getByRole("complementary", { name: "上下文" })).toContainText("DeepSeek · deepseek-v4-pro · deepseek/deepseek-v4-pro · DeepSeek");
  await app.close();
});
