import { expect, test } from "@playwright/test";
import { launch, snap, navTo } from "./app";

// 任务动态 (work order #27): the work the owner's agents handed out, as it goes. The fixture's agent (bot)
// started every session there: one asks, two failed, one runs, one is done, one archived.

test("the tasks page: what each agent's work is doing, what needs a look first, and a click to the session", async () => {
  const { app, page } = await launch();
  await navTo(page, /^任务动态/);
  const view = page.getByRole("region", { name: "任务动态" });
  const rows = view.getByRole("list", { name: "派出的活" }).getByRole("listitem");
  await expect(rows).toHaveCount(5); // not the archived one
  await expect(view).not.toContainText("归档的");
  // Needing a look first: the question and the two failures, then the one at work, then the one done.
  const titles = await rows.evaluateAll((els) => els.map((e) => e.getAttribute("aria-label")));
  expect(titles.slice(0, 3).sort()).toEqual(["崩溃", "选颜色", "限流"].sort());
  expect(titles.slice(3)).toEqual(["跑很久", "写 hello"]);
  await expect(view.getByLabel("概况")).toHaveText("3 个要看一眼 · 1 个在干活 · 1 个已完成");
  const running = rows.filter({ hasText: "跑很久" });
  await expect(running).toContainText("bot → ");
  await expect(running).toContainText("运行中");
  await expect(rows.filter({ hasText: "选颜色" })).toContainText("等你回答");
  await snap(page, "tasks");
  // A click opens the session.
  await running.getByRole("button").click();
  await expect(page.locator(".ctitle")).toContainText("跑很久");
  await app.close();
});
