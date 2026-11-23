import { expect, test, type Page } from "@playwright/test";
import { launch, navTo, snap } from "./app";

// 工单 #105: a failed session says what happened in words the owner can act on — which agent, what kind of problem,
// what to do — and keeps the agent's raw message folded under 详情, never on the card itself. What can't be told apart
// says so, and that the original is in 详情; it never guesses a cause.

// DSH's words, through ACP, of a model with no key (the owner's 2026-11-23 screenshot).
const noKey =
  'Internal error: turn failed: llm-deepseek: no API key for provider route "deepseek-official"; store DEEPSEEK_API_KEY through the credentials service (the web Models page writes it), or export DEEPSEEK_API_KEY in the launching environment';
const odd = "Internal error: turn failed: tool registry corrupted";

/** Says words to DSH from its bot page, as the owner, in a new conversation; then opens the session it started, failed. */
async function failOnDSH(page: Page, words: string): Promise<void> {
  const roster = page.getByRole("region", { name: "Bot" });
  await roster.getByRole("group", { name: "owner-mac", exact: true }).getByRole("button", { name: /^DSH/ }).click({ timeout: 30_000 });
  const bot = page.getByRole("region", { name: /^DSH/ });
  const fresh = bot.getByRole("button", { name: "开启新对话" });
  if (await fresh.isEnabled()) await fresh.click();
  const box = bot.getByRole("textbox", { name: /^给 DSH/ });
  await box.fill(`fail ${words}`);
  await box.press("Enter");
  await expect(bot.locator(".composer-state")).toContainText("已开新对话", { timeout: 30_000 });
  await bot.locator(".conv-head-t").click();
  await expect(page.getByRole("alert")).toBeVisible({ timeout: 30_000 });
}

test("a missing key is said in words with what to do; DSH's own message only under 详情", async () => {
  const { app, page } = await launch();
  await page.setViewportSize({ width: 1440, height: 900 });
  await failOnDSH(page, noKey);
  const card = page.getByRole("alert");
  await expect(card.locator(".qf-title")).toHaveText("DSH 还没配 DeepSeek 的密钥");
  await expect(card).toContainText("在 DSH 的模型设置里填好 DeepSeek 的密钥。");
  await expect(card).toContainText("你再发一条消息时，会从这里接着做");
  for (const raw of ["DEEPSEEK_API_KEY", "deepseek-official", "-32603", "Internal error"]) await expect(card).not.toContainText(raw);
  await snap(page, "batchj-105-nokey");
  await card.getByRole("button", { name: /详情/ }).click();
  await expect(card.locator(".qf-raw")).toHaveText(`${noKey} (code -32603)`);
  await snap(page, "batchj-105-nokey-details");

  // The overview's 需要你处理 says the same words, not the message.
  await navTo(page, /^概览/);
  const needs = page.getByRole("region", { name: "需要你处理" });
  await expect(needs).toContainText("DSH 还没配 DeepSeek 的密钥");
  await expect(needs).not.toContainText("DEEPSEEK_API_KEY");
  await expect(needs).not.toContainText("tool registry");
  await app.close();
});

test("a failure that can't be told apart says so, and that the original is under 详情", async () => {
  const { app, page } = await launch();
  await page.setViewportSize({ width: 1440, height: 900 });
  await failOnDSH(page, odd);
  const card = page.getByRole("alert");
  await expect(card.locator(".qf-title")).toHaveText("DSH 出了个内部错误");
  await expect(card).toContainText("详情里有原文。");
  await expect(card).not.toContainText("tool registry");
  await snap(page, "batchj-105-unknown");
  await card.getByRole("button", { name: /详情/ }).click();
  await expect(card.locator(".qf-raw")).toHaveText(`${odd} (code -32603)`);
  await snap(page, "batchj-105-unknown-details");
  await app.close();
});
