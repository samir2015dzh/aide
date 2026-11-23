import { expect, test } from "@playwright/test";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { launch, snap } from "./app";

// The app on Windows (docs/windows.md §5), against the real daemon there:
// a PC's words and Ctrl shortcuts, the menu button where macOS has the
// menu bar, the notification area's icon, and staying there once the
// window closes. The rest of the suite runs on macOS.
test.skip(process.platform !== "win32", "Windows only");

type Hook = { tray(): { view: { state: string } }; mainWindow(): { close(): void } | undefined };

test("a PC's words, type and corners, and Ctrl shortcuts, the menu button, the notification area, staying there without a window, and a link bringing it back", async () => {
  const { app, page } = await launch();
  await expect(page.locator("html")).toHaveAttribute("data-platform", "win");
  const map = page.getByRole("region", { name: "链路" });
  await expect(map.getByRole("button", { name: "这台电脑", exact: true })).toBeVisible();
  // Search, in the sidebar's top band, says its shortcut with Ctrl.
  await expect(page.locator(".side-top").getByRole("button", { name: "搜索" })).toHaveAttribute("title", "搜索 Ctrl+K");
  await expect(page.getByRole("button", { name: "菜单" })).toBeVisible();
  // A PC's type a size up (14 px), with the same Apple corners and selection as a Mac (工单 #95): no Windows 11 accent bar.
  // The selected row: a bot's (工单 #63: the sidebar is bots alone).
  const overview = page.locator(".side-bots .bot-row").first();
  await overview.click();
  expect(await page.evaluate(() => getComputedStyle(document.body).fontSize)).toBe("14px");
  expect(await overview.evaluate((e) => getComputedStyle(e).borderRadius)).toBe("6px");
  expect(await overview.evaluate((e) => getComputedStyle(e, "::before").content)).toBe("none");
  await expect.poll(() => app.evaluate(() => (globalThis as unknown as { aideTest: Hook }).aideTest.tray().view.state)).toBe("ok");
  await snap(page, "windows");
  await app.evaluate(() => (globalThis as unknown as { aideTest: Hook }).aideTest.mainWindow()?.close());
  await expect.poll(() => app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows().length)).toBe(0);
  // Still there: its notification area's icon still follows the daemon.
  expect(await app.evaluate(() => (globalThis as unknown as { aideTest: Hook }).aideTest.tray().view.state)).toBe("ok");
  // An aide: link, opened again as Windows does (a second Aide.exe), brings the window back.
  await app.evaluate(({ app }) => app.emit("second-instance", {}, ["Aide.exe", "aide://open"]));
  await expect.poll(() => app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows().filter((w) => w.isVisible()).length)).toBe(1);
  await app.close();
});

test("the window opens at the size it had last time, loading in the middle until aided answers", async () => {
  const width = (app: Awaited<ReturnType<typeof launch>>["app"]) => app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0]?.getBounds().width ?? 0);
  const profile = fs.mkdtempSync(path.join(os.tmpdir(), "hmdesk-"));
  const first = await launch(undefined, { profile });
  await expect(first.page.getByRole("button", { name: "菜单" })).toBeVisible();
  await expect.poll(() => width(first.app)).toBeGreaterThan(900);
  await first.app.close();

  const { app, page } = await launch(path.join(fs.mkdtempSync(path.join(os.tmpdir(), "hmsock-")), "none"), { profile });
  await expect(page.getByText("正在连接后台服务…")).toBeVisible();
  expect(await width(app)).toBeGreaterThan(900);
  await app.close();
});
