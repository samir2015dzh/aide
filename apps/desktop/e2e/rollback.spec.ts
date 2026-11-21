import { expect, test } from "@playwright/test";
import { cli, fixture, launch, sessionList } from "./app";

// Rolling a session back from its page: each turn's prompt rolls back to before it, after a warning that every
// message and change from that turn on is discarded; the conversation keeps the discarded turns with a note.

test("a turn's prompt rolls the session back to before it, once the warning is confirmed", async () => {
  const title = "回滚三轮";
  const started = JSON.parse(await cli("session", "start", "--project", "demo", "--agent", "gemini", "--title", title, "--prompt", "write a.txt one\nsay 第一轮", "--wait", "-o", "json"));
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
