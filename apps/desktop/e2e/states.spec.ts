import { expect, test, type Page } from "@playwright/test";
import { cli, fixture, launch, onWindows, sessionList, snap } from "./app";

// A session's two-part state (docs/session-states.md) as the app shows it: a
// turn on work-mac quiet past the stall limit (5 s here, HERMESH_STALL_AFTER)
// reads stalled; work-mac frozen, it reads lost, last known running, never
// running; work-mac back, it reads as it is, and its page keeps when work-mac
// was out of touch. The pictures go to the delivery record (HERMESH_SCREENS).

async function open(page: Page, title: string) {
  await (await sessionList(page)).getByText(title, { exact: true }).click();
  await expect(page.getByRole("heading", { name: title })).toBeVisible();
}

test("a quiet turn reads stalled; its machine gone, lost, never running; back, as it is, with when it was gone", async () => {
  test.skip(onWindows, "it freezes the other machine by its process group");
  test.setTimeout(5 * 60_000);
  await cli("session", "start", "--project", "work-mac/billing", "--agent", "gemini", "--title", "长任务", "--prompt", "sleep 10m");
  // In Chinese, as the owner reads it (Linux takes the test override, as Windows does).
  const { app, page } = await launch(fixture.socket, { env: { HERMESH_TEST_LANGUAGES: "zh-Hans" } });
  await open(page, "长任务");
  const row = (await sessionList(page)).locator(".srow", { hasText: "长任务" });
  const bar = page.locator(".cbar");
  await expect(bar).toContainText(/\d+ 秒无新事件/, { timeout: 30_000 });
  // The row's ring says it too (behind the ⋯ button on the open row): amber, labelled.
  await expect(row.locator(".busy.quiet")).toHaveAttribute("aria-label", /^运行中 · \d+ 秒无新事件$/);
  await snap(page, "states-stalled");

  process.kill(-fixture.otherMac, "SIGSTOP");
  try {
    // Checked while the relay finds it silent, then lost at 30 s from its last heartbeat: never running.
    await expect(row).toContainText("失联 · 最后已知 运行中", { timeout: 90_000 });
    // Still with the work in progress, not with what is done: it may well be running yet.
    await expect(page.getByRole("group", { name: /^进行中/ }).getByText("长任务", { exact: true })).toBeVisible();
    // The page says it under the conversation, in place of the control area (no "working", nothing to stop).
    await expect(page.locator(".livebar.stale")).toContainText("失联 · 最后已知 运行中");
    await expect(bar).toHaveCount(0);
    await snap(page, "states-lost");
  } finally {
    process.kill(-fixture.otherMac, "SIGCONT");
  }
  await expect(row).not.toContainText("失联", { timeout: 90_000 });
  await expect(bar).toContainText(/曾失联 \d\d:\d\d–\d\d:\d\d/, { timeout: 30_000 });
  await snap(page, "states-back");
  await app.close();
});
