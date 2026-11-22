import { expect, test } from "@playwright/test";
import { launch, snap, navTo } from "./app";

// Claude 会话 (docs/claude-sessions.md): the Claude Code sessions the Claude desktop app has on the
// owner's machines, listed by their title in the app, with the GitHub repository their folder's own
// origin names, and opened read only: the conversation as Claude Code wrote it.

test("the Claude app's sessions: listed by title with their repository, opened read only", async () => {
  const { app, page } = await launch();
  await navTo(page, /^Claude 会话/);
  const list = page.getByRole("navigation", { name: "Claude 会话" });
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
  await expect(page.getByRole("textbox")).toHaveCount(0);
  await expect(page.getByRole("switch")).toHaveCount(0);
  await snap(page, "claude-sessions");
  await app.close();
});
