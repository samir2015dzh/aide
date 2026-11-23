import { expect, test } from "@playwright/test";
import { launch, snap } from "./app";

// A coding agent its machine found has a settings page like a connected agent's grant page (工单 #93): what it can use
// (its machine, its projects), its kind told from what it is until the owner files it otherwise (#52: the owner's say
// first, kept on its machine), its name and icon, its scope, stopping it. Filed personal, it is listed with the
// personal agents; named, it is called by the name everywhere. All on its settings page (工单 #97).

test("Claude Code's settings: what it can use, its kind changed and back, renamed, stopped and resumed", async () => {
  const { page } = await launch();
  const roster = page.getByRole("region", { name: "Bot" });
  await roster.getByRole("button", { name: /^Claude Code/ }).first().click({ timeout: 30_000 });
  const bot = page.getByRole("region", { name: /^Claude Code/ });
  // Its settings are a page of their own, from the button in its head (工单 #97).
  await bot.getByRole("button", { name: "设置", exact: true }).click();
  const settings = page.getByRole("region", { name: /^Claude Code.* 的设置$/ });
  await expect(settings).toContainText("能用什么");
  await expect(settings).toContainText("owner-mac（它在这台上运行）");
  await expect(settings.getByRole("button", { name: "修改能用的范围" })).toBeVisible();
  // What a grant has and it hasn't is not there: no expiry, no exec switch.
  await expect(settings.getByRole("switch")).toHaveCount(0);
  await snap(page, "agentsettings-claude");

  // Its kind: filed personal, it moves to the personal agents; back to told from what it is, it returns.
  const kind = settings.getByRole("radiogroup", { name: "类别" });
  await expect(kind.getByRole("radio", { checked: true })).toHaveText(/自动判断 · 本机编码 Agent/);
  await kind.getByRole("radio", { name: "个人 Agent" }).click();
  const personal = roster.getByRole("group", { name: /^个人/ });
  await expect(personal.getByRole("button", { name: /^Claude Code/ })).toBeVisible({ timeout: 30_000 });
  await expect(kind.getByRole("radio", { checked: true })).toHaveText("个人 Agent");
  await kind.getByRole("radio", { name: /^自动判断/ }).click();
  await expect(personal.getByRole("button", { name: /^Claude Code/ })).toHaveCount(0, { timeout: 30_000 });

  // Stopped, it takes no work; used again, it does.
  await settings.getByRole("button", { name: "暂停", exact: true }).click();
  await expect(settings.locator(".bot-pill")).toHaveText("已暂停", { timeout: 30_000 });
  await settings.getByRole("button", { name: "恢复", exact: true }).click();
  await expect(settings.locator(".bot-pill")).not.toHaveText("已暂停", { timeout: 30_000 });

  // Renamed from its settings: called by the name in the roster and on its page.
  await settings.getByRole("region", { name: "名字和头像" }).getByRole("button", { name: "给它起名字、换头像" }).click();
  const form = page.getByRole("dialog", { name: "添加联系人" });
  await form.getByLabel("名字", { exact: true }).fill("写代码的");
  await form.getByRole("button", { name: "保存" }).click();
  await expect(roster.getByRole("button", { name: /^写代码的/ })).toBeVisible({ timeout: 30_000 });
  await page.getByRole("button", { name: "返回 写代码的" }).click();
  await expect(page.getByRole("region", { name: "写代码的", exact: true }).getByRole("heading", { level: 1 })).toHaveText("写代码的");
});
