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
