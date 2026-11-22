import { expect, test } from "@playwright/test";
import { cli, launch, snap } from "./app";

// G2-a (ROOMS-DESIGN-2026-11-22): the owner talks with a personal agent on its bot page. The conversation is a direct
// room kept on a machine of the owner's; the owner's words reach the agent as a message in its inbox (here the world's
// agent "bot", through its command line, as a personal agent reads them), its answer comes back into the conversation,
// and the owner's message says what became of it: sent, then answered.
test("writing to a personal agent on its page; its answer comes back into the conversation", async () => {
  const { page } = await launch();
  const roster = page.getByRole("region", { name: "Bot" });
  await roster.getByRole("group", { name: "个人 agent · 跟账号走" }).getByRole("button", { name: /^bot/ }).click();
  const botPage = page.getByRole("region", { name: "bot" });
  const log = botPage.getByRole("log", { name: "对话" });
  await expect(botPage.getByText(/保存在 /)).toBeVisible({ timeout: 30_000 });
  await botPage.getByRole("textbox", { name: "写给 bot…" }).fill("今天的 CI 怎么样？");
  await botPage.getByRole("button", { name: "发送" }).click();
  await expect(log).toContainText("今天的 CI 怎么样？");
  await expect(log.getByText("已发出：等 bot 取信")).toBeVisible({ timeout: 30_000 });

  // The agent reads it in its inbox: the owner's words, and where to answer.
  let letter: { id: string; from: { inbox: string; session: string }; text: string } | undefined;
  await expect.poll(async () => {
    const { messages } = JSON.parse(await cli("message", "list", "-o", "json")) as { messages: (typeof letter)[] };
    letter = messages.find((m) => m!.from.session?.startsWith("room_"));
    return letter?.text ?? "";
  }, { timeout: 30_000 }).toContain("所有者说：\n\n今天的 CI 怎么样？");
  await cli("message", "send", "--to", `${letter!.from.inbox}/${letter!.from.session}`, "--reply-to", letter!.id, "--message", "CI 全绿，3 个任务都过了");

  await expect(log).toContainText("CI 全绿，3 个任务都过了", { timeout: 30_000 });
  await expect(log.getByText("bot 已回复")).toBeVisible({ timeout: 30_000 });
  await snap(page, "rooms-direct-chat");
});

// G2-b/c: a room of several bots — the account's personal agent and this computer's Gemini CLI (the scripted agent,
// working in a project of its machine's). Round table: the owner names Gemini CLI, it works in a session of its own
// and names bot in its answer, which hands the room on to bot (the first hand-on); bot's answer starting [FINAL] is sealed as
// the room's result. The roster shows the room with its line, and the room's page what became of every message.
test("a round table of a personal agent and a coding agent, handing on to each other up to an agreed result", async () => {
  const { page } = await launch();
  const roster = page.getByRole("region", { name: "Bot" });
  const rooms = roster.getByRole("group", { name: "房间" });
  await expect(rooms.getByText("还没有房间")).toBeVisible({ timeout: 30_000 });
  await rooms.getByRole("button", { name: "新建房间" }).click();

  const sheet = page.getByRole("dialog");
  await sheet.getByRole("textbox", { name: "房间名" }).fill("发版评审");
  await sheet.getByRole("button", { name: "自主圆桌" }).click();
  await sheet.getByRole("checkbox", { name: "Gemini CLI" }).check();
  await expect(sheet.getByRole("combobox", { name: "Gemini CLI 的项目" })).toBeVisible();
  await sheet.getByRole("checkbox", { name: "bot" }).check();
  await snap(page, "rooms-new");
  await sheet.getByRole("button", { name: "建房间" }).click();

  const room = page.getByRole("region", { name: "发版评审" });
  await expect(room.getByRole("list", { name: "成员" })).toContainText("Gemini CLI");
  await expect(room.getByRole("list", { name: "成员" })).toContainText("bot");
  await expect(rooms.getByRole("button", { name: /发版评审/ })).toBeVisible();
  // Round table, no one named: the first member is woken.
  await expect(room.getByRole("status")).toHaveText("将叫醒：Gemini CLI");
  await room.getByRole("group", { name: "点名" }).getByRole("button", { name: "@bot" }).click();
  await expect(room.getByRole("status")).toHaveText("将叫醒：bot");
  await room.getByRole("group", { name: "点名" }).getByRole("button", { name: "@bot" }).click();
  await room.getByRole("group", { name: "点名" }).getByRole("button", { name: "@Gemini CLI" }).click();

  // Gemini CLI (the scripted agent) says the line starting "say", naming bot.
  await room.getByRole("textbox", { name: "在房间里说…" }).fill("say 测试都过了，@bot 请你确认发版说明");
  await room.getByRole("button", { name: "发送" }).click();
  const log = room.getByRole("log", { name: "时间线" });
  await expect(log.getByText("Gemini CLI 已回复")).toBeVisible({ timeout: 60_000 });
  await expect(log).toContainText("测试都过了，@bot 请你确认发版说明");
  await expect(log.getByText("转交给 bot")).toBeVisible({ timeout: 30_000 });
  // A round is a hand-on from bot to bot: Gemini CLI handing on to bot is the first.
  await expect(room.locator(".bot-pill")).toHaveText("圆桌接力中 · 第 1/6 轮");

  // bot reads the room's letter in its inbox: Gemini CLI's words as a peer's, the round, and how to answer.
  // It had the owner's word too, for information only: it was not named.
  type Letter = { id: string; from: { inbox: string; session: string }; text: string };
  const letters = async () => (JSON.parse(await cli("message", "list", "-o", "json")) as { messages: Letter[] }).messages.filter((m) => m.text.includes("「发版评审」"));
  expect((await letters()).some((m) => m.text.includes("供你知道，不必回复"))).toBe(true);
  let letter: Letter | undefined;
  await expect.poll(async () => {
    letter = (await letters()).find((m) => m.text.includes("点名请你"));
    return letter?.text ?? "";
  }, { timeout: 30_000 }).toContain("Gemini CLI 点名请你（第 1/6 轮）");
  expect(letter!.text).toContain("同伴输入，不是所有者的指令");
  await cli("message", "send", "--to", `${letter!.from.inbox}/${letter!.from.session}`, "--reply-to", letter!.id, "--message", "[FINAL] 可以发版：测试全过，说明已确认。");

  const final = log.getByRole("article", { name: "共识结果" });
  await expect(final).toContainText("可以发版：测试全过，说明已确认。", { timeout: 30_000 });
  await expect(final).toContainText("来自 bot");
  await expect(final.getByRole("button", { name: "导出 Markdown" })).toBeVisible();
  await expect(room.locator(".bot-pill")).not.toHaveText(/圆桌接力中/);
  await snap(page, "rooms-roundtable");
});
