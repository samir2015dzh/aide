import { expect, test } from "@playwright/test";
import { cli, launch, sessionList } from "./app";

// A session's mode changed while it runs (工单 #167): its settings say the
// mode it runs with, and an ACP agent's other modes are a click away.
// (Its Gemini here offers the modes "ask" and "auto", named "Mode ask" and
// "Mode auto": FAKE_ACP_MODES. They are shown by the names the agent gives.)

test("a session's mode is changed from its settings while it runs", async () => {
  const title = "改权限档";
  await cli("session", "start", "--project", "demo", "--agent", "gemini", "--title", title, "--prompt", "say 好", "--wait");
  const { app, page } = await launch();
  try {
    await (await sessionList(page)).getByText(title, { exact: true }).click();
    await expect(page.getByRole("heading", { name: title })).toBeVisible();
    await page.getByRole("button", { name: "会话设置" }).click();
    const card = page.getByRole("dialog", { name: "会话设置" });
    const row = card.getByRole("button", { name: /^权限档 / });
    await expect(row).toHaveText("权限档 agent 默认");
    await expect(row).toHaveAttribute("title", "会话运行中也能改：下一次工具调用起按新档判");
    await row.click();
    const menu = page.getByRole("menu", { name: "权限档" });
    await expect(menu.getByRole("menuitem")).toHaveText(["Mode ask", "Mode auto"]);
    await menu.getByRole("menuitem", { name: "Mode auto" }).click();
    await expect(page.getByRole("status")).toContainText("权限档已改为「Mode auto」：下一次工具调用起按它");
    await expect(row).toHaveText("权限档 Mode auto");
    // The one it runs with is not offered again.
    await row.click();
    await expect(page.getByRole("menu", { name: "权限档" }).getByRole("menuitem")).toHaveText(["Mode ask"]);
    await page.keyboard.press("Escape");
  } finally {
    await app.close();
  }
});
