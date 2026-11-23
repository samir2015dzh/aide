import { expect, test } from "@playwright/test";
import { launch, pick, snap } from "./app";

// 批 I, as the owner meets it. The roster (工单 #75): personal agents under 「个人 agent」, the coding agents under their
// machine's name, rows naming the agent alone. A project's ⋯ menu (工单 #77 追加二) in words someone who never used git
// reads: a copy per task or right in the folder, which agents may use it, the same for every agent, and removing it
// from Aide with its folder untouched. DSH's permission (工单 #89, DSH's own presets): set on the project there, it is
// the box's default for DSH (said so), the task runs with it, and a choice above it is said before it is sent.
test("the roster by machine; a project's menu in plain words; DSH's permission set on the project goes with the work", async () => {
  const { app, page } = await launch();
  await page.setViewportSize({ width: 1440, height: 900 });
  const roster = page.getByRole("region", { name: "Bot" });
  const mac = roster.getByRole("group", { name: "owner-mac", exact: true });
  await expect(mac.getByRole("button", { name: "owner-mac", exact: true })).toBeVisible({ timeout: 30_000 });
  await expect(roster.getByRole("group", { name: "个人 agent", exact: true })).toBeVisible();
  for (const word of ["跟账号走", "跟机器走", "编码 agent"]) await expect(roster).not.toContainText(word);
  await mac.getByRole("button", { name: /^DSH/ }).click({ timeout: 30_000 });
  await snap(page, "batchi-roster");

  const bot = page.getByRole("region", { name: /^DSH/ });
  const project = bot.getByRole("group", { name: "demo" });
  await expect(project).toBeVisible({ timeout: 30_000 });
  await app.evaluate(({ ipcMain }) => {
    ipcMain.removeHandler("popup-menu");
    ipcMain.handle("popup-menu", (_e, items: { label?: string; checked?: boolean; enabled?: boolean; separator?: boolean }[]) => {
      (globalThis as unknown as { shown: string[] }).shown = items.map((i) => (i.separator ? "—" : `${i.checked ? "✓ " : ""}${i.label}${i.enabled === false ? " (off)" : ""}`));
      return "preset:read-only";
    });
  });
  await project.getByRole("button", { name: "更多" }).click();
  const shown = await app.evaluate(() => (globalThis as unknown as { shown: string[] }).shown);
  expect(shown).toEqual(expect.arrayContaining([
    "改名…",
    expect.stringMatching(/^(✓ )?每个任务用一份副本：互不干扰，干完再合回来$/),
    expect.stringMatching(/^(✓ )?直接在这个文件夹里干活：改动立即生效，没有副本$/),
    "DSH 的默认权限（派活时可以逐次改）： (off)",
    "仅可查看",
    "工作区内修改",
    "完全权限",
    "✓ 不指定：按 DSH 自己的设置",
    "给哪些 agent 用： (off)",
    "✓ DSH",
    "✓ Gemini CLI",
    "从 Aide 里移除这个项目（文件夹和里面的文件不动）",
  ]));
  expect(shown.filter((l) => /worktree|原地|不再给/.test(l))).toEqual([]);
  await expect(project).toContainText("DSH：仅可查看");

  // The box: DSH's permission chip has the project's as its default, and the task runs with it.
  const chip = bot.getByRole("button", { name: "权限", exact: true });
  await expect(chip).toHaveText("项目默认：仅可查看");
  await snap(page, "batchi-permission");
  const box = bot.getByRole("textbox", { name: /^给 DSH/ });
  await box.fill("config");
  await box.press("Enter");
  await expect(bot.locator(".composer-state")).toContainText("已开新对话", { timeout: 30_000 });
  await bot.locator(".conv-head-t").click();
  await expect(page.getByRole("log", { name: "对话" })).toContainText("read-only", { timeout: 30_000 });

  // Back on its page, chosen above the project's: it is said over the box before anything is sent.
  await roster.getByRole("group", { name: "owner-mac", exact: true }).getByRole("button", { name: /^DSH/ }).click();
  await pick(bot.getByRole("button", { name: "权限", exact: true }), "完全权限");
  await expect(bot.getByRole("note")).toContainText("比项目「demo」定的 DSH 权限「仅可查看」高：这次会按你选的「完全权限」来");
  await snap(page, "batchi-above");
  await app.close();
});
