import { expect, test } from "@playwright/test";
import { launch, snap, navTo } from "./app";

// 待放行 (docs/agent-messages.md §15.5): a new agent's message to a session another started waits for the
// owner, who releases it to its session, or discards it. The sidebar shows it while one waits.

test("a held message: listed with why, released to its session", async () => {
  const { app, page } = await launch();
  // In the account menu while one waits (工单 #63: no pages in the sidebar).
  await expect.poll(async () => {
    await page.locator(".side-foot").first().click();
    const n = await page.getByRole("menu", { name: "账号与设置" }).getByRole("menuitem", { name: /放行/ }).count();
    await page.keyboard.press("Escape");
    return n;
  }, { timeout: 45_000 }).toBe(1);
  await navTo(page, /放行/);
  const list = page.getByRole("region", { name: "等你放行的消息" });
  const row = list.getByRole("listitem", { name: "helper" });
  await expect(row).toBeVisible();
  await expect(row).toContainText("刚接入不久的 agent，先经你确认");
  await expect(row).toContainText("say 放行了");
  await snap(page, "held");
  await row.getByRole("button", { name: "放行" }).click();
  await expect(list.getByRole("listitem")).toHaveCount(0);
  await expect(list).toContainText("没有等你放行的消息");
  await app.close();
});
