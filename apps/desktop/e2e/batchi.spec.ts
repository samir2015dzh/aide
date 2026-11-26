import { expect, test, type Page } from "@playwright/test";
import { createHash } from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { launch, sessionList, snap } from "./app";

// 批 I, the session page (工单 #90–#92): its right side lists the sessions to switch to, by project, its settings in a
// menu; the box to write in is Claude's — + for images, files and audio, / for commands, what it runs with under it —
// and what goes with a message reaches the agent byte for byte (played by the scripted ACP agent, which reads each
// linked file and says its SHA-256).

const sha = (b: Buffer) => createHash("sha256").update(b).digest("hex").slice(0, 12);
const png = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==", "base64");
const notes = Buffer.from("the secret is 4217\n");
const memo = Buffer.from("RIFF$\0\0\0WAVEfmt fake");

/** Starts a session of the owner's with Gemini CLI from 新建任务, and opens it. */
async function start(page: Page, prompt: string) {
  await (await sessionList(page)).waitFor();
  await page.locator(".side-h").getByRole("button", { name: "新建任务" }).click();
  const sheet = page.getByRole("dialog", { name: "新建任务" });
  await sheet.getByRole("group", { name: "Agent" }).getByRole("button", { name: "Gemini CLI" }).click();
  await sheet.getByLabel("消息", { exact: true }).fill(prompt);
  await sheet.getByRole("button", { name: "启动" }).click();
  await expect(sheet).toBeHidden();
  await expect(page.getByRole("heading", { name: prompt })).toBeVisible();
}

test("the session page: its sessions by project to switch to, its settings in a menu, nothing lost (工单 #90)", async () => {
  const { page } = await launch();
  await page.setViewportSize({ width: 1440, height: 860 });
  await start(page, "say 右栏");
  const list = page.getByRole("complementary", { name: "会话列表" });
  await expect(list).toBeVisible();
  // At the top a new session, and which project folder's sessions to show; nothing else stays there.
  await expect(list.getByRole("button", { name: "新会话" })).toBeVisible();
  await expect(list.getByRole("button", { name: "项目文件夹" })).toBeVisible();
  // By project, the open one marked; this agent's sessions on this machine only.
  const demo = list.getByRole("group", { name: "demo" });
  await expect(demo.getByRole("button", { name: /say 右栏/ })).toHaveAttribute("aria-current", "true");
  await expect(demo.getByRole("button", { name: /写 hello/ })).toBeVisible();
  await expect(demo.getByRole("button", { name: /写 hello/ }).locator(".sdot")).toHaveCount(1);
  await snap(page, "batchi-session-list");
  // One click to another session, one click back.
  await demo.getByRole("button", { name: /写 hello/ }).click();
  await expect(page.getByRole("heading", { name: "写 hello" })).toBeVisible();
  await list.getByRole("button", { name: /say 右栏/ }).click();
  await expect(page.getByRole("heading", { name: "say 右栏" })).toBeVisible();
  // The folder narrows the list to one project's sessions.
  await list.getByRole("button", { name: "项目文件夹" }).click();
  await page.getByRole("menuitemradio", { name: "demo" }).click();
  await expect(list.getByRole("group")).toHaveCount(1);

  // Every setting the info panel had is in the menu from the gear, shown only when asked for.
  const card = page.getByRole("complementary", { name: "任务信息" });
  await expect(card).toHaveCount(0);
  await page.getByRole("button", { name: "会话设置" }).click();
  const menu = page.getByRole("dialog", { name: "会话设置" });
  await expect(card.getByRole("listitem")).toHaveText([/你 发起$/, "Gemini CLI 执行", /这台/, "demo", /^aide\//]);
  await expect(card.getByRole("button", { name: "机器设置" })).toBeVisible();
  await expect(menu.getByRole("complementary", { name: "复盘" }).getByRole("button", { name: "复盘这个任务" })).toBeVisible();
  await snap(page, "batchi-session-settings");
  await page.keyboard.press("Escape");
  await expect(menu).toHaveCount(0);

  // A new session from the list: the sheet, for this machine and agent.
  await list.getByRole("button", { name: "新会话" }).click();
  const sheet = page.getByRole("dialog", { name: "新建任务" });
  await expect(sheet.getByRole("group", { name: "Agent" }).getByRole("button", { name: "Gemini CLI" })).toHaveAttribute("aria-pressed", "true");
  await page.keyboard.press("Escape");
});

test("the box to write in: + attaches images, files and audio (pasted, dropped or picked), / lists commands, and the files reach the agent (工单 #91, #92)", async () => {
  const { page } = await launch();
  await page.setViewportSize({ width: 1440, height: 860 });
  await start(page, "say 附件");
  const log = page.getByRole("log", { name: "对话" });
  await expect(log).toContainText("附件");
  const composer = page.locator(".mbox form.composer");
  const box = composer.getByRole("textbox", { name: "发给 Gemini CLI 的消息" });
  // Claude's: the line, + on the left of its foot, send on the right; what it runs with under it on the right.
  await expect(box).toHaveAttribute("placeholder", "输入 / 以使用命令");
  await expect(composer.getByRole("button", { name: "更多" })).toBeVisible();
  await expect(composer.getByRole("button", { name: "发送" })).toBeDisabled();
  await expect(composer.locator(".composer-params")).toContainText("Gemini CLI");
  // This computer has no dictation the app can start (Linux): no microphone button that would do nothing.
  if (process.platform === "linux") await expect(composer.getByRole("button", { name: "语音输入" })).toHaveCount(0);
  // What Chromium's own speech recognition does here, for the record (工单 #92: Electron builds lack its service).
  const speech = await page.evaluate(
    () =>
      new Promise<string>((done) => {
        const C = (window as unknown as { webkitSpeechRecognition?: new () => { start(): void; onerror: (e: { error: string }) => void; onstart: () => void } }).webkitSpeechRecognition;
        if (!C) return done("absent");
        const r = new C();
        r.onerror = (e) => done(`error: ${e.error}`);
        r.onstart = () => done("started");
        try {
          r.start();
        } catch (e) {
          done(`threw: ${e}`);
        }
        setTimeout(() => done("no answer in 8s"), 8000);
      }),
  );
  if (process.env.AIDE_SCREENS) fs.writeFileSync(path.join(process.env.AIDE_SCREENS, "batchi-speech.txt"), `webkitSpeechRecognition in Electron: ${speech}\n`);

  // / lists the commands; one chosen does what it says (!plan, the machine's macro).
  await box.fill("/");
  const cmds = composer.getByRole("listbox", { name: "命令" });
  await expect(cmds.getByRole("option")).toContainText(["/plan", "/implement", "/new", "/archive", "/id"]);
  await box.fill("/pl");
  await expect(cmds.getByRole("option")).toHaveCount(1);
  await box.press("Enter");
  await expect(box).toHaveValue("!plan ");
  await expect(cmds).toHaveCount(0);
  await box.fill("");

  // + offers images, files and audio (and the files go with the message).
  await composer.getByRole("button", { name: "更多" }).click();
  await expect(composer.getByRole("menu", { name: "更多" }).getByRole("menuitem")).toHaveText(["图片", "文件", "音频"]);
  await composer.getByRole("button", { name: "更多" }).click();
  // Picked: a text file and a recording (no format kept out); pasted: a screenshot; one more, then taken off.
  await composer.locator('input[data-attach="file"]').setInputFiles([{ name: "notes.txt", mimeType: "text/plain", buffer: notes }, { name: "junk.bin", mimeType: "application/octet-stream", buffer: Buffer.from("x") }]);
  await composer.locator('input[data-attach="audio"]').setInputFiles({ name: "memo.wav", mimeType: "audio/wav", buffer: memo });
  await box.evaluate((el, b64) => {
    const data = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
    const dt = new DataTransfer();
    dt.items.add(new File([data], "shot.png", { type: "image/png" }));
    el.dispatchEvent(new ClipboardEvent("paste", { clipboardData: dt, bubbles: true, cancelable: true }));
  }, png.toString("base64"));
  const files = composer.getByRole("list", { name: "已附上" });
  await expect(files.getByRole("listitem")).toHaveCount(4);
  await expect(files.getByRole("img", { name: "shot.png" })).toBeVisible();
  // 工单 #143: words copied with a picture of them (a spreadsheet's cells) paste as the words, the picture left out;
  // the same file pasted again goes once; files dropped anywhere over the window come to the box.
  await box.evaluate((el, b64) => {
    const data = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
    const dt = new DataTransfer();
    dt.items.add(new File([data], "cells.png", { type: "image/png" }));
    dt.setData("text/plain", "收入\t123");
    el.dispatchEvent(new ClipboardEvent("paste", { clipboardData: dt, bubbles: true, cancelable: true }));
  }, png.toString("base64"));
  await expect(files.getByRole("listitem")).toHaveCount(4);
  await expect(files.getByRole("listitem").filter({ hasText: "cells.png" })).toHaveCount(0);
  await composer.locator('input[data-attach="file"]').setInputFiles({ name: "notes.txt", mimeType: "text/plain", buffer: notes });
  await expect(composer.locator(".composer-state")).not.toContainText("没加上");
  await expect(files.getByRole("listitem").filter({ hasText: "notes.txt" })).toHaveCount(1);
  const dropped = await page.evaluateHandle(() => {
    const dt = new DataTransfer();
    dt.items.add(new File(["dropped"], "dropped.txt", { type: "text/plain" }));
    return dt;
  });
  const far = page.locator(".dmain, .bot-body, main").first();
  await far.dispatchEvent("dragover", { dataTransfer: dropped });
  await expect(composer.locator(".composer-drop")).toContainText("松手添加附件");
  await far.dispatchEvent("drop", { dataTransfer: dropped });
  await expect(composer.locator(".composer-drop")).toHaveCount(0);
  await expect(files.getByRole("listitem")).toHaveCount(5);
  await composer.getByRole("button", { name: "移除 dropped.txt" }).click();
  await expect(files.getByRole("listitem")).toHaveCount(4);
  await expect(files.getByRole("listitem").filter({ hasText: "notes.txt" })).toContainText("文件 · 19 B");
  await expect(files.getByRole("listitem").filter({ hasText: "memo.wav" })).toContainText("音频");
  await composer.getByRole("button", { name: "移除 junk.bin" }).click();
  await expect(files.getByRole("listitem")).toHaveCount(3);
  await expect(composer.getByRole("button", { name: "发送" })).toBeEnabled();
  await box.fill("files");
  await snap(page, "batchi-composer");
  const shots = process.env.AIDE_SCREENS;
  if (shots) await composer.screenshot({ path: path.join(shots, "batchi-composer-box.png") });

  // Sent with the words at once; the agent read each file: its name, type and the SHA-256 of what it read.
  await box.press("Enter");
  await expect(log).toContainText(`link notes.txt text/plain ${sha(notes)}`, { timeout: 30_000 });
  await expect(log).toContainText(`${sha(memo)}`);
  await expect(log).toContainText(`link shot.png image/png ${sha(png)}`);
  await expect(files).toHaveCount(0);
  await expect(box).toHaveValue("");
  // In its bubble (工单 #143): the words as written, not the list the agent read; the files over them, the picture as
  // its thumbnail.
  const sent = page.locator(".um").filter({ has: page.getByRole("list", { name: "随消息发送的附件" }) }).last();
  await expect(sent.getByRole("img", { name: "shot.png" })).toBeVisible({ timeout: 30_000 });
  await expect(sent.locator(".um-file").filter({ hasText: "notes.txt" })).toContainText("文件 · 19 B");
  await expect(sent.locator(".bub")).toHaveText("files");
  await snap(page, "batchi-sent-files");

  // More than a message takes: the rest is refused by kind and name, said in the box, not dropped unsaid.
  await composer.locator('input[data-attach="file"]').setInputFiles(Array.from({ length: 21 }, (_, i) => ({ name: `f${i}.txt`, mimeType: "text/plain", buffer: Buffer.from("x") })));
  await expect(files.getByRole("listitem")).toHaveCount(20);
  await expect(composer.locator(".composer-state")).toContainText("文件「f20.txt」没加上：一条消息最多带 20 个附件");
});

test("a coding agent's bot page takes attachments and commands too (工单 #91, #92)", async () => {
  const { page } = await launch();
  const roster = page.getByRole("region", { name: "Bot" });
  await roster.getByRole("button", { name: /^Gemini CLI/ }).first().click({ timeout: 30_000 });
  const bot = page.getByRole("region", { name: /^Gemini CLI/ });
  const composer = bot.locator("form.composer");
  await expect(composer.getByRole("textbox")).toHaveAttribute("placeholder", "输入 / 以使用命令");
  await composer.getByRole("button", { name: "更多" }).click();
  await expect(composer.getByRole("menu", { name: "更多" }).getByRole("menuitem")).toContainText(["图片", "文件", "音频", "开启新对话"]);
});
