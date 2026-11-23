import { expect, test } from "@playwright/test";
import { launch, snap } from "./app";

// Claude Code's own sessions (docs/claude-sessions.md), on its bot page (工单 #76): the Claude Code sessions the
// Claude desktop app has on the owner's machines, listed by their title in the app, with the GitHub repository their
// folder's own origin names, and opened read only there: the conversation as Claude Code wrote it.

test("the Claude app's sessions: listed by title with their repository, opened read only", async () => {
  const { app, page } = await launch();
  await page.getByRole("region", { name: "Bot" }).getByRole("button", { name: /^Claude Code/ }).first().click({ timeout: 30_000 });
  const bot = page.getByRole("region", { name: /^Claude Code/ });
  const list = bot.getByRole("region", { name: "它自己的会话" });
  const row = list.getByRole("button", { name: /改首页/ });
  await expect(row).toBeVisible({ timeout: 30_000 });
  await expect(row).toContainText("octo/site");
  await expect(row).toContainText("已完成");
  await row.click();
  await expect(page.locator(".dsh-msg.user").filter({ hasText: "首页标题改一下" })).toBeVisible();
  await expect(page.getByText("改好了。")).toBeVisible();
  // No aide hooks reported its conversation on this machine: it takes no messages, and says so.
  await expect(page.getByText(/不可寄信/)).toBeVisible();
  // Read only: nothing to write in, no takeover.
  const opened = page.locator(".own-session");
  await expect(opened.getByRole("textbox")).toHaveCount(0);
  await expect(opened.getByRole("switch")).toHaveCount(0);
  // Back on its page: the conversations again, its own sessions under them.
  await opened.getByRole("button", { name: "返回对话" }).click();
  await expect(bot.getByRole("list", { name: "对话" })).toBeVisible();
  await snap(page, "claude-sessions");
  await app.close();
});
