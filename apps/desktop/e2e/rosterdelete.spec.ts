import { expect, test } from "@playwright/test";
import { answerMenu, launch, snap } from "./app";

// 工单 #82: the owner deletes a bot from the roster, from its right-click menu, told first what it does. Muse here is a
// personal agent that logged in once and isn't connected: its access is revoked and the server forgets it, and it
// leaves the roster at once; the other bots stay.

test("deleting a personal agent that isn't connected: asked first, then gone from the roster (工单 #82)", async () => {
  const { app, page } = await launch();
  const roster = page.getByRole("region", { name: "Bot" });
  const muse = roster.getByRole("button", { name: /^Muse/ });
  await expect(muse).toBeVisible({ timeout: 30_000 });
  await expect(roster.getByRole("button", { name: /^bot/ })).toBeVisible();

  // Cancel first: nothing happens.
  await answerMenu(app, "remove");
  await muse.click({ button: "right" });
  const sheet = page.getByRole("dialog");
  await expect(sheet.getByRole("heading", { name: "删除 Muse？" })).toBeVisible();
  await expect(sheet).toContainText("现在没连着");
  await expect(sheet).toContainText("吊销它的授权");
  await snap(page, "rosterdelete-confirm");
  await sheet.getByRole("button", { name: "取消" }).click();
  await expect(sheet).toHaveCount(0);
  await expect(muse).toBeVisible();

  // Then delete it.
  await muse.click({ button: "right" });
  await sheet.getByRole("button", { name: "删除", exact: true }).click();
  await expect(page.getByText("已删除 Muse")).toBeVisible({ timeout: 30_000 });
  await expect(muse).toHaveCount(0, { timeout: 30_000 });
  await expect(roster.getByRole("button", { name: /^bot/ })).toBeVisible();
});
