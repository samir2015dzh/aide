import { expect, test } from "@playwright/test";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { launch, snap, sessionList, navTo } from "./app";

// The owner's agents at work, on the fixture's world: this Mac with sessions in every state, all sent by the
// agent "bot".

test("the window opens on the overview, and again the next time", async () => {
  const profile = fs.mkdtempSync(path.join(os.tmpdir(), "hmdesk-"));
  let { app, page } = await launch(undefined, { profile, start: true });
  const work = () => page.getByRole("heading", { name: "概览", level: 1 });
  // The start page is picked once the owner's world is in: its sessions are counted.
  await expect(page.getByRole("region", { name: "需要你处理" })).toContainText("选颜色", { timeout: 30_000 });
  await expect(work()).toBeVisible();
  await app.close();

  ({ app, page } = await launch(undefined, { profile, start: true }));
  await expect(page.getByRole("region", { name: "需要你处理" })).toContainText("选颜色", { timeout: 30_000 });
  await expect(work()).toBeVisible();
  await app.close();
});

test("the sidebar is bots alone; every session is a page of its own, its list beside the one open; collapsed, the rail shows the bots (工单 #63)", async () => {
  const { app, page } = await launch();
  await page.setViewportSize({ width: 1240, height: 800 });
  const sidebar = page.getByRole("complementary", { name: "边栏" });
  await expect(sidebar.getByRole("region", { name: "Bot" })).toBeVisible({ timeout: 30_000 });
  // No session list in the sidebar, no pages but the overview and the task feed at its top (工单 #102); something
  // waits on the owner: counted on the overview there.
  await expect(sidebar.locator(".side-nav").getByRole("button")).toHaveText([/^概览/, /^任务动态/]);
  await expect(sidebar.getByRole("list", { name: "任务" })).toHaveCount(0);
  await expect(sidebar.locator(".side-nav .nav-i").first().locator(".nav-count")).toBeVisible();

  // Every session: 全部任务, from the account menu; one chosen shows beside the list, marked in it.
  const list = await sessionList(page);
  await expect(list).toContainText("写 hello");
  const hello = list.getByRole("listitem").filter({ hasText: "写 hello" });
  await hello.click();
  await expect(page.getByRole("heading", { name: "写 hello" })).toBeVisible();
  await expect(hello).toHaveAttribute("aria-current", "true");
  await expect(list).toBeVisible();
  await snap(page, "sidebar-session");

  // Collapsed, the sidebar is its rail: each bot's face, saying on hover how it stands; a click opens its page.
  await page.getByRole("button", { name: "收成窄栏" }).click();
  const rail = page.getByRole("complementary", { name: "窄栏" });
  const gemini = rail.getByRole("button", { name: /^Gemini CLI · / });
  await gemini.hover();
  await expect(page.getByRole("tooltip")).toHaveText(/^Gemini CLI · /);
  await gemini.click();
  await expect(page.getByRole("region", { name: /^Gemini CLI/ })).toBeVisible();
  await expect(gemini).toHaveAttribute("aria-current", "true");
  await navTo(page, /^概览/);
  await expect(page.getByRole("heading", { name: "概览", level: 1 })).toBeVisible();
  await expect(rail).toBeVisible();
  await page.getByRole("button", { name: "展开边栏" }).click();
  await expect(sidebar).toBeVisible();
  await app.close();
});

test("The overview: what waits is listed and one click answers a simple question; recently finished; the link map stays", async () => {
  const { app, page } = await launch();
  await expect(page.getByRole("heading", { name: "概览", level: 1 })).toBeVisible();
  await expect(page.getByRole("region", { name: "链路" })).toBeVisible();
  const needs = page.getByRole("region", { name: "需要你处理" });
  const question = needs.getByRole("listitem").filter({ hasText: "选颜色" });
  await expect(question).toContainText("bot → Gemini · demo · 选哪个颜色");
  await expect(needs.getByRole("listitem").filter({ hasText: "限流" })).toBeVisible();
  await expect(page.getByRole("region", { name: "最近完成" })).toContainText("写 hello");
  await snap(page, "team-work");

  await question.getByRole("button", { name: "蓝", exact: true }).click();
  await expect(page.locator(".toast")).toHaveText("已回答：蓝");
  await expect(question).toHaveCount(0, { timeout: 30_000 });
  // The session goes on with the answer.
  await (await sessionList(page)).getByRole("listitem").filter({ hasText: "选颜色" }).click();
  await expect(page.getByRole("heading", { name: "选颜色" })).toBeVisible();
  await expect(page.getByRole("log", { name: "对话" })).toContainText("蓝", { timeout: 30_000 });
  await app.close();
});
