import { expect, test } from "@playwright/test";
import { launch, sessionList, snap } from "./app";

// A managed workspace's requests in the conversation (docs/workspace-requests.md):
// each change as it stands, with who made it. The app only shows them: the
// coordinator answers through Aide's messages, never from here.

test("a managed workspace's requests show each change and who made it, with nothing to act on", async () => {
  const { app, page } = await launch();
  try {
    await (await sessionList(page)).getByText("工作区申请", { exact: true }).click();
    const log = page.getByRole("log", { name: "对话" });
    const rows = log.locator(".msg").filter({ hasText: "工作区申请：" });
    // The tool: pending, approved, provisioned, confirmed; the information: pending, rejected.
    await expect(rows).toHaveCount(6);
    await expect(rows.nth(0)).toContainText("工作区申请：工具 ripgrep");
    await expect(rows.nth(0)).toContainText("待批 · 由这个任务的 agent 提出");
    await expect(rows.filter({ hasText: "信息 本机时区" }).last()).toContainText("已拒绝 · muse");
    await expect(rows.filter({ hasText: "已拒绝" })).toContainText("探测类默认拒绝");
    await expect(rows.filter({ hasText: "已就位 · muse" })).toContainText("已放进工具库，版本 14.1.0。");
    await expect(rows.last()).toContainText("已确认 · 由这个任务的 agent 提出");
    await expect(rows.getByRole("button")).toHaveCount(0);
    await snap(page, "workspace-requests");
  } finally {
    await app.close();
  }
});
