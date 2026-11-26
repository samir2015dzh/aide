import { expect, test } from "@playwright/test";
import { cli, fixture, launch, sessionList } from "./app";

// Rolling a session back from its page: each turn's prompt rolls back to before it, after a warning that every
// message and change from that turn on is discarded; the conversation keeps the discarded turns with a note.

test("a turn's prompt rolls the session back to before it, once the warning is confirmed", async () => {
  const title = "回滚三轮";
  const started = JSON.parse(await cli("session", "start", "--project", "demo", "--agent", "claude", "--title", title, "--prompt", "write a.txt one\nsay 第一轮", "--wait", "-o", "json"));
  const id = started.session.session_id;
  await cli("session", "send", id, "--wait", "--message", "write b.txt two\nsay 第二轮");
  await cli("session", "send", id, "--wait", "--message", "write c.txt three\nsay 第三轮");
  const get = async () => JSON.parse(await cli("session", "get", id, "-o", "json")).session;

  const { app, page } = await launch();
  try {
    await (await sessionList(page)).getByText(title, { exact: true }).click();
    const log = page.getByRole("log", { name: "对话" });
    const prompt = log.locator(".um").filter({ hasText: "write b.txt two" });
    await prompt.hover();
    await prompt.getByRole("button", { name: "回滚到这条消息之前" }).click();

    // The warning says what goes; cancelling leaves the session as it was.
    const sheet = page.getByRole("dialog");
    await expect(sheet).toContainText("回滚到第 2 轮之前？");
    await expect(sheet).toContainText("会丢弃第 2 轮起其后轮次的对话与改动（第 2–3 轮）");
    await sheet.getByRole("button", { name: "取消" }).click();
    expect((await get()).turns).toBe(3);

    await prompt.hover();
    await prompt.getByRole("button", { name: "回滚到这条消息之前" }).click();
    await page.getByRole("dialog").getByRole("button", { name: "回滚", exact: true }).click();
    await expect(log).toContainText("已回滚到第 1 轮：上面第 2–3 轮已丢弃。");
    const s = await get();
    expect(s.turns).toBe(1);
    expect(s.status).toBe("idle");
    // The discarded turns stay in the conversation, but are no checkpoints any more; the first still is.
    await expect(log).toContainText("第三轮");
    await prompt.hover();
    await expect(prompt.getByRole("button", { name: "回滚到这条消息之前" })).toHaveCount(0);
    const first = log.locator(".um").filter({ hasText: "write a.txt one" });
    await first.hover();
    await expect(first.getByRole("button", { name: "回滚到这条消息之前" })).toBeVisible();

    const worktree = JSON.parse(await cli("session", "events", id, "-o", "json", "--limit", "500")).events
      .filter((e: { type: string }) => e.type === "rolled_back");
    expect(worktree).toHaveLength(1);
    expect(worktree[0]).toMatchObject({ to_turn: 1, last_turn: 3 });
    // The session goes on from there.
    await cli("session", "send", id, "--wait", "--message", "say 接着做");
    await expect(log).toContainText("接着做");
    expect((await get()).turns).toBe(2);
  } finally {
    await app.close();
  }
});

test("an in-place session's prompts offer no rollback: it has no snapshots", async () => {
  const id = fixture.sessions["就地"];
  await cli("session", "send", id, "--wait", "--message", "say 再来");
  const { app, page } = await launch();
  try {
    await (await sessionList(page)).getByText("就地", { exact: true }).click();
    const log = page.getByRole("log", { name: "对话" });
    await expect(log).toContainText("再来");
    for (const bubble of await log.locator(".um").all()) {
      await bubble.hover();
      await expect(bubble.getByRole("button", { name: "回滚到这条消息之前" })).toHaveCount(0);
    }
  } finally {
    await app.close();
  }
});

// 工单 #131: a Claude Code session Aide runs on this computer goes on in the Claude desktop app, through the app's own
// link (claude://resume?session=<Claude Code's session id>), which lists it there: from the session's settings, once
// Aide is done with its turn — while a turn runs the button says to wait, as the app would run a second process on it.
test("a Claude Code session opens in the Claude desktop app from its settings, once Aide's turn is done", async () => {
  const title = "去 Claude 里接着聊";
  const started = JSON.parse(await cli("session", "start", "--project", "demo", "--agent", "claude", "--title", title, "--prompt", "say 开始", "--wait", "-o", "json"));
  const id = started.session.session_id;
  const get = async () => JSON.parse(await cli("session", "get", id, "-o", "json")).session;
  const ref = (await get()).agent_session_id as string;
  expect(ref).toMatch(/^[0-9a-f-]{36}$/);
  const { app, page } = await launch();
  try {
    await app.evaluate(({ shell }) => {
      (globalThis as unknown as { opened: string[] }).opened = [];
      shell.openExternal = async (u: string) => void (globalThis as unknown as { opened: string[] }).opened.push(u);
    });
    await (await sessionList(page)).getByText(title, { exact: true }).click();
    await expect(page.getByRole("heading", { name: title })).toBeVisible();
    await page.getByRole("button", { name: "会话设置" }).click();
    const card = page.getByRole("complementary", { name: "任务信息" });
    const button = card.getByRole("button", { name: "在 Claude 中打开" });

    // A turn running: not yet, and it says why.
    await cli("session", "send", id, "--message", "sleep 20s\nsay 慢慢来");
    await expect.poll(async () => (await get()).status).toBe("running");
    await expect(button).toBeDisabled();
    await expect(button).toHaveAttribute("title", /这一轮结束后才能在 Claude 里接着聊/);

    // Done: it opens the session in the app by its link.
    await expect(button).toBeEnabled({ timeout: 60_000 });
    await button.click();
    await expect.poll(() => app.evaluate(() => (globalThis as unknown as { opened: string[] }).opened)).toEqual([`claude://resume?session=${ref}`]);
  } finally {
    await app.close();
  }
});
