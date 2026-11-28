import { expect, test } from "@playwright/test";
import { launch, snap } from "./app";

// Claude Code's own sessions (docs/claude-sessions.md), on its bot page (工单 #76): the Claude Code sessions the
// Claude desktop app has on the owner's machines, listed by their title in the app, with the GitHub repository their
// folder's own origin names, and opened read only there: the conversation as Claude Code wrote it.

test("the Claude app's sessions: listed by title with their repository, opened read only", async () => {
  const { app, page } = await launch();
  await page.getByRole("region", { name: "Bot" }).getByRole("button", { name: /^Claude Code/ }).first().click({ timeout: 30_000 });
  const bot = page.getByRole("region", { name: /^Claude Code/ });
  await bot.getByRole("button", { name: /全部对话/ }).click();
  const list = bot.getByRole("region", { name: "它自己的会话" });
  const row = list.getByRole("button", { name: /改首页/ });
  await expect(row).toBeVisible({ timeout: 30_000 });
  await expect(row).toContainText("octo/site");
  await expect(row).toContainText("已完成");
  await row.click();
  await expect(page.locator(".dsh-msg.user").filter({ hasText: "首页标题改一下" })).toBeVisible();
  await expect(page.getByText("改好了。")).toBeVisible();
  // No aide hooks reported its conversation on this machine: other agents can't write to it, and it says so.
  await expect(page.getByText(/还不能给它寄信/)).toBeVisible();
  // No takeover; but the owner can go on with it in Aide (工单 #181).
  const opened = page.locator(".own-session");
  await expect(opened.getByRole("switch")).toHaveCount(0);
  const goOn = opened.getByRole("region", { name: "在 Aide 里接着聊" });
  await expect(goOn).toContainText("在它原来的文件夹");
  await snap(page, "claude-sessions");
  // Back on its page: the conversations again, its own sessions under them.
  await opened.getByRole("button", { name: "返回对话" }).click();
  await expect(bot.getByRole("list", { name: "对话" })).toBeVisible();
  await app.close();
});

// 工单 #181: one of the app's Claude Code sessions goes on in Aide. Its folder is in no project: said so, with the way
// on — add it as a project — and then it goes on: a session of Aide's that resumes Claude Code's own conversation in its
// own folder, opened, its reply there; still among the app's sessions, saying where it goes on.
test("an app's Claude session goes on in Aide: its folder added as a project, then resumed there", async () => {
  const { app, page } = await launch();
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.getByRole("region", { name: "Bot" }).getByRole("button", { name: /^Claude Code/ }).first().click({ timeout: 30_000 });
  const bot = page.getByRole("region", { name: /^Claude Code/ });
  await bot.getByRole("button", { name: /全部对话/ }).click();
  await bot.getByRole("region", { name: "它自己的会话" }).getByRole("button", { name: /改首页/ }).click({ timeout: 30_000 });
  const goOn = page.locator(".own-session").getByRole("region", { name: "在 Aide 里接着聊" });
  await goOn.getByRole("textbox").fill("再把副标题也改了");
  await goOn.getByRole("button", { name: "接着聊" }).click();
  await expect(goOn.getByRole("alert")).toContainText("没有哪个项目包含它工作的文件夹");
  await snap(page, "181-no-project");
  await goOn.getByRole("button", { name: "把这个文件夹加为项目，再接着聊" }).click();
  // The session of Aide's it is now, opened, resumed: Claude Code answered there.
  await expect(page.getByRole("log", { name: "对话" })).toContainText("running on", { timeout: 30_000 });
  await expect(page.getByRole("log", { name: "对话" })).toContainText("再把副标题也改了");
  await snap(page, "181-continued");
  const s = await page.evaluate(async () => {
    const r = await fetch("app://aide/api/aide.local.v1.LocalService/ListSessions", { method: "POST", headers: { "content-type": "application/json" }, body: "{}" });
    return (((await r.json()).sessions ?? []) as { agent: string; title: string; providerSessionRef?: string }[]).find((x) => x.title === "改首页");
  });
  expect(s?.agent).toBe("claude");
  // Still among its own sessions — its earlier conversation is there — opening the session it goes on in.
  await page.getByRole("region", { name: "Bot" }).getByRole("button", { name: /^Claude Code/ }).first().click();
  await bot.getByRole("button", { name: /全部对话/ }).click();
  await bot.getByRole("region", { name: "它自己的会话" }).getByRole("button", { name: /改首页/ }).click();
  const gone = page.locator(".own-session").getByRole("region", { name: "在 Aide 里接着聊" });
  await expect(gone).toContainText("已经在 Aide 里接着聊了");
  await expect(gone.getByRole("textbox")).toHaveCount(0);
  await snap(page, "181-continued-listed");
  await gone.getByRole("button", { name: "打开 Aide 里的会话" }).click();
  await expect(page.getByRole("log", { name: "对话" })).toContainText("再把副标题也改了");
  await app.close();
});
