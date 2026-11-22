import { expect, test, type ElectronApplication, type Page } from "@playwright/test";
import { launch, sessionList } from "./app";

// New Task (docs/technical-design.md §3.4): the owner starts a coding
// agent from the app, in a project or a GitHub repository, then goes on
// writing to it under the conversation. What the machine refuses stays in
// the sheet as it said it. An agent's session has no message box.

/** What the app menu sends the page (文件 › 新建任务…). */
async function command(app: ElectronApplication, page: Page, c: string) {
  await expect(await sessionList(page)).toBeVisible();
  type Main = { aideTest: { mainWindow(): { webContents: { send(channel: string, c: string): void } } } };
  await app.evaluate((_, c) => (globalThis as unknown as Main).aideTest.mainWindow().webContents.send("command", c), c);
}

test("starting a session in a project, then writing to it under the conversation", async () => {
  const { app, page } = await launch();
  await page.setViewportSize({ width: 1440, height: 800 });
  // The + at the head of the task list.
  await (await sessionList(page)).waitFor();
  await page.locator(".side-h").getByRole("button", { name: "新建任务" }).click();
  const sheet = page.getByRole("dialog", { name: "新建任务" });
  await expect(sheet.getByRole("group", { name: "机器" }).getByRole("button", { name: "owner-mac" })).toHaveAttribute("aria-pressed", "true");
  await sheet.getByRole("group", { name: "Agent" }).getByRole("button", { name: "Gemini CLI" }).click();
  await expect(sheet.getByRole("group", { name: "工作位置" }).getByRole("button", { name: "项目" })).toHaveAttribute("aria-pressed", "true");
  await expect(sheet.locator("button.picker[aria-label=\"项目\"]")).toHaveText("demo");
  // Gemini told this machine its models in an earlier session: they can be picked, the default unset.
  await expect(sheet.getByRole("button", { name: "模型", exact: true })).toHaveText("默认");
  await expect(sheet.getByRole("button", { name: "启动" })).toBeDisabled();
  await sheet.getByLabel("消息", { exact: true }).fill("say 从桌面发起");
  await sheet.getByRole("button", { name: "启动" }).click();

  // The session opens, started by you, and runs.
  await expect(sheet).toBeHidden();
  await expect(page.getByRole("heading", { name: "say 从桌面发起" })).toBeVisible();
  const card = page.getByRole("complementary", { name: "任务信息" });
  await expect(card.getByRole("listitem").first()).toHaveText(/你 发起$/);
  const log = page.getByRole("log", { name: "对话" });
  await expect(log).toContainText("从桌面发起");
  await expect(page.getByText("这一轮已完成。在下面再发一条消息即可在这个任务里继续。")).toBeVisible();

  // Written to from the box under it: ↩ sends.
  const box = page.getByLabel("发给 Gemini CLI 的消息");
  await box.fill("say 接着做");
  await box.press("Enter");
  await expect(log).toContainText("接着做");
  await expect(box).toHaveValue("");
  await app.close();
});

test("starting in a GitHub repository; what the machine refuses stays in the sheet", async () => {
  const { app, page } = await launch();
  await page.setViewportSize({ width: 1440, height: 800 });
  await command(app, page, "newSession");
  const sheet = page.getByRole("dialog", { name: "新建任务" });
  await sheet.getByRole("group", { name: "Agent" }).getByRole("button", { name: "Gemini CLI" }).click();
  await sheet.getByRole("group", { name: "工作位置" }).getByRole("button", { name: "GitHub 仓库" }).click();
  await sheet.getByLabel("消息", { exact: true }).fill("say 在仓库里");
  const repo = sheet.getByLabel("仓库", { exact: true });
  const branch = sheet.getByLabel("分支", { exact: true });
  for (const [r, b, said] of [
    ["acme/private", "", "cannot reach GitHub repository acme/private"],
    ["acme/widget", "gone", "has no branch"],
  ]) {
    await repo.fill(r);
    await branch.fill(b);
    await sheet.getByRole("button", { name: "启动" }).click();
    await expect(sheet.getByRole("alert")).toContainText(said);
    await expect(sheet).toBeVisible();
  }
  await branch.fill("");
  await repo.fill("https://github.com/acme/widget");
  await sheet.getByRole("button", { name: "启动" }).click();
  await expect(sheet).toBeHidden();
  const card = page.getByRole("complementary", { name: "任务信息" });
  await expect(card.getByRole("listitem")).toContainText(["GitHub 仓库 acme/widget，从 main 开始"]);
  await expect(page.getByRole("log", { name: "对话" })).toContainText("在仓库里");

  // An agent's session takes no messages from here.
  await (await sessionList(page)).getByText("来自 GitHub", { exact: true }).click();
  await expect(page.getByRole("heading", { name: "来自 GitHub" })).toBeVisible();
  await expect(page.getByLabel(/^发给 .* 的消息$/)).toHaveCount(0);
  await app.close();
});
