import { expect, test } from "@playwright/test";
import { launch, sessionList } from "./app";

// Hand Work To (工单 #41, docs/personas.md): every agent the machines detected
// is there with no contact made first, and work goes to it at once. A contact
// is a custom face for one of them, on the same line: the first message starts
// a task with the contact's machine, project and agent, which opens as any
// other; the next goes on in the same task. Deleting the contact keeps its task.
test("handing work to a detected agent with no contact; adding a contact, writing to it twice, deleting it", async () => {
  const { app, page } = await launch();
  await page.setViewportSize({ width: 1440, height: 800 });
  const list = await sessionList(page);
  await list.waitFor();
  const contacts = page.locator(".side-h").getByRole("button", { name: "派活" });
  await contacts.click();
  const sheet = page.getByRole("dialog", { name: "派活给谁" });
  // No contact yet, and the agents are all there, by machine: work goes to one at once.
  const gemini = sheet.getByRole("option", { name: /^Gemini CLI/ });
  await expect(gemini).toBeVisible();
  await expect(sheet.getByRole("group", { name: "owner-mac" })).toBeVisible();
  await gemini.click();
  await sheet.getByLabel("发给 Gemini CLI 的消息").fill("say 直接派活");
  await sheet.getByRole("button", { name: "发送" }).click();
  await expect(sheet).toBeHidden();
  await expect(page.getByRole("heading", { name: "say 直接派活" })).toBeVisible();
  await contacts.click();

  // Added: a name, an avatar, its machine, agent and project.
  await sheet.getByRole("button", { name: "添加联系人…" }).click();
  const form = page.getByRole("dialog", { name: "添加联系人" });
  await form.getByLabel("名字", { exact: true }).fill("前端助手");
  await form.getByRole("group", { name: "头像" }).getByRole("button", { name: "fox" }).click();
  await expect(form.getByRole("group", { name: "机器" }).getByRole("button", { name: "owner-mac" })).toHaveAttribute("aria-pressed", "true");
  await form.getByRole("group", { name: "Agent" }).getByRole("button", { name: "Gemini CLI" }).click();
  await expect(form.getByLabel("项目", { exact: true })).toHaveValue("demo");
  await form.getByRole("button", { name: "保存" }).click();

  // Written to: a new task, opened.
  await expect(sheet.getByRole("option", { name: /前端助手/ })).toHaveAttribute("aria-selected", "true");
  await expect(sheet.getByText("会在项目「demo」里新开一个任务。")).toBeVisible();
  await sheet.getByLabel("发给 前端助手 的消息").fill("say 联系人第一句");
  await sheet.getByRole("button", { name: "发送" }).click();
  await expect(sheet).toBeHidden();
  await expect(page.getByRole("heading", { name: "say 联系人第一句" })).toBeVisible();
  const log = page.getByRole("log", { name: "对话" });
  await expect(log).toContainText("联系人第一句");
  await expect(page.getByText("这一轮已完成。在下面再发一条消息即可在这个任务里继续。")).toBeVisible();
  const tasks = await list.getByRole("listitem").count();

  // Written to again: the same task goes on.
  await contacts.click();
  await expect(sheet.getByText("正在用的任务：")).toBeVisible();
  await expect(sheet.getByRole("button", { name: "say 联系人第一句" })).toBeVisible();
  await sheet.getByLabel("发给 前端助手 的消息").fill("say 联系人第二句");
  await sheet.getByRole("button", { name: "发送" }).click();
  await expect(sheet).toBeHidden();
  await expect(page.getByRole("heading", { name: "say 联系人第一句" })).toBeVisible();
  await expect(log).toContainText("联系人第二句");
  await expect(list.getByRole("listitem")).toHaveCount(tasks);

  // Deleted: the contact goes, its task stays.
  await contacts.click();
  await sheet.getByRole("option", { name: /前端助手/ }).getByRole("button", { name: "编辑" }).click();
  await page.getByRole("dialog", { name: "编辑联系人" }).getByRole("button", { name: "删除联系人" }).click();
  await expect(sheet.getByRole("option", { name: /前端助手/ })).toHaveCount(0);
  await expect(gemini).toBeVisible(); // the agent itself stays
  await sheet.getByRole("button", { name: "取消" }).click();
  await expect(list.getByText("say 联系人第一句")).toBeVisible();
  await app.close();
});
