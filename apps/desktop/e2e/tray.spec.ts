import { expect, test } from "@playwright/test";
import { execFile } from "node:child_process";
import fs from "node:fs";
import { promisify } from "node:util";
import type { Menu } from "electron";
import { fixture, launch, snap } from "./app";

// The app in the notification area only, its window closed (docs/resident.md
// §5): what its processes hold then is the tray state of the resident
// budget (the owner's target is 105 MB with aided and her, measured as Task
// Manager's Memory column, the private working set). A regression check: the
// app's processes together under AIDE_TRAY_BUDGET_MB (default trayBudgetMB),
// and none of them a page.
test.skip(process.platform !== "win32", "Windows only");

// The app's processes in the tray state on a GitHub Windows runner (no GPU:
// Chromium's GPU process runs on WARP there): 55.0 MB at first (2026-11-20:
// main 36.7, GPU 11.6, network service 6.7), with about a third more for the
// runner's noise. Lower it when the app gets smaller; raising it needs a reason.
const trayBudgetMB = 75;
// How long after the window closes it is measured: the tray panel's page goes 30 s after it hides, and Chromium
// settles what the window left.
const settle = 45_000;
const samples = 10;

type Hook = { tray(): { view: { state: string } }; mainWindow(): { close(): void } | undefined };

/** Each process's private working set (WorkingSetPrivate, Task Manager's Memory column), in MB. */
async function privateMB(pids: number[]): Promise<Map<number, number>> {
  const { stdout } = await promisify(execFile)(
    "powershell.exe",
    [
      "-NoProfile",
      "-NonInteractive",
      "-Command",
      `Get-CimInstance Win32_PerfRawData_PerfProc_Process | Where-Object { @(${pids.join(",")}) -contains $_.IDProcess } | ForEach-Object { "$($_.IDProcess) $($_.WorkingSetPrivate)" }`,
    ],
    { windowsHide: true, timeout: 60_000 },
  );
  const out = new Map<number, number>();
  for (const line of stdout.split(/\r?\n/)) {
    const [pid, bytes] = line.trim().split(" ").map(Number);
    if (pid && Number.isFinite(bytes)) out.set(pid, bytes / (1 << 20));
  }
  return out;
}

test("in the notification area only, the app's processes stay within the tray budget, with no page", async () => {
  test.setTimeout(5 * 60_000);
  const { app } = await launch();
  await expect.poll(() => app.evaluate(() => (globalThis as unknown as { aideTest: Hook }).aideTest.tray().view.state)).toBe("ok");
  await app.evaluate(() => (globalThis as unknown as { aideTest: Hook }).aideTest.mainWindow()?.close());
  await expect.poll(() => app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows().length)).toBe(0);
  await new Promise((r) => setTimeout(r, settle));

  const procs = await app.evaluate(({ app }) => app.getAppMetrics().map((m) => ({ pid: m.pid, type: m.type, name: m.name ?? m.serviceName ?? "" })));
  // No page in the tray state: the window and the tray panel's are gone.
  expect(procs.filter((p) => p.type === "Tab").map((p) => p.pid)).toEqual([]);
  const by = new Map<string, number[]>();
  for (let i = 0; i < samples; i++) {
    const mb = await privateMB(procs.map((p) => p.pid));
    for (const p of procs) {
      const k = p.type === "Utility" ? `utility ${p.name}` : p.type.toLowerCase();
      const list = by.get(k) ?? [];
      list[i] = (list[i] ?? 0) + (mb.get(p.pid) ?? 0);
      by.set(k, list);
    }
    await new Promise((r) => setTimeout(r, 2000));
  }
  const mean = (xs: number[]) => xs.reduce((a, b) => a + b, 0) / xs.length;
  const rows = [...by].map(([k, xs]) => [k, mean(xs)] as const).sort((a, b) => b[1] - a[1]);
  const total = rows.reduce((n, [, v]) => n + v, 0);
  const budget = Number(process.env.AIDE_TRAY_BUDGET_MB || trayBudgetMB);
  const table = ["| process | private working set (MB) |", "|---|---|", ...rows.map(([k, v]) => `| ${k} | ${v.toFixed(1)} |`), `| **total** | **${total.toFixed(1)}** (budget ${budget}) |`];
  console.log(`tray state, the app's processes, mean of ${samples} samples:\n${table.join("\n")}`);
  if (process.env.GITHUB_STEP_SUMMARY) fs.appendFileSync(process.env.GITHUB_STEP_SUMMARY, `\n### Tray state (desktop app)\n\n${table.join("\n")}\n`);
  expect(total, "the app's processes in the tray state, MB").toBeLessThanOrEqual(budget);
  await app.close();
});

// 工单 #116: the icon's right-click menu offers Check for Updates… between Settings… and the separator, and it
// takes the main menu's path: the update window opens with what a check found (here a development build's
// answer), in place, not silently.
test("the icon's right-click menu checks for updates in place", async () => {
  const { app } = await launch();
  type MenuHook = { aideTest: { tray(): { menu(): Menu } } };
  const items = await app.evaluate(() =>
    (globalThis as unknown as MenuHook).aideTest.tray().menu().items.map((i) => ({ id: i.id, label: i.label, type: i.type })),
  );
  expect(items.map((i) => i.id || i.type)).toEqual(["open", "settings", "checkForUpdates", "separator", "quit"]);
  expect(items.find((i) => i.id === "checkForUpdates")?.label).toBe("检查更新…");
  const [updates] = await Promise.all([
    app.waitForEvent("window"),
    app.evaluate(() => (globalThis as unknown as MenuHook).aideTest.tray().menu().getMenuItemById("checkForUpdates")?.click()),
  ]);
  await expect.poll(() => updates.url()).toMatch(/#updates$/);
  await expect(updates.getByRole("status", { name: "更新状态" })).toContainText("开发版本不检查更新");
  await snap(updates, "tray-check-updates");
  await app.close();
});

// 工单 #120: when the background service is unreachable, the tray panel offers an in-place
// "重启后台服务" button, reports failure reasons + "查看日志" on failure, and only returns to
// running after a real post-restart probe succeeds; when healthy, the button is hidden.
test("tray panel restarts unreachable daemon in place, surfaces failures, and probes recovery", async () => {
  const deadSocket = process.platform === "win32" ? "\\\\.\\pipe\\aide-e2e-dead-120" : "/tmp/aide-e2e-dead-120.sock";
  const { app } = await launch(deadSocket);
  try {
    type TrayHook = {
      aideTest: {
        tray(): { view: { state: string }; toggle(): void; panel(): unknown };
        setDaemonRestarter(fn: (() => Promise<void>) | null): void;
      };
    };
    await expect.poll(() => app.evaluate(() => (globalThis as unknown as TrayHook).aideTest.tray().view.state)).toBe("daemon");

    const [panel] = await Promise.all([
      app.waitForEvent("window"),
      app.evaluate(() => (globalThis as unknown as TrayHook).aideTest.tray().toggle()),
    ]);
    await expect.poll(() => panel.url()).toMatch(/#tray$/);
    await expect(panel.getByText("后台服务没有响应")).toBeVisible();
    const restartBtn = panel.getByRole("button", { name: "重启后台服务" });
    await expect(restartBtn).toBeVisible();
    await snap(panel, "tray-daemon-down");

    // 1. Failing restart shows real reason + View Logs in place and stays in daemon state.
    await app.evaluate(() => {
      (globalThis as unknown as TrayHook).aideTest.setDaemonRestarter(async () => {
        throw new Error("服务重启权限被拒绝");
      });
    });
    await restartBtn.click();
    await expect(panel.getByRole("alert")).toContainText("服务重启权限被拒绝");
    await expect(panel.getByRole("button", { name: "查看日志" })).toBeVisible();
    await expect(panel.getByText("未运行")).toBeVisible();
    await snap(panel, "tray-daemon-restart-failed");

    // 2. Restart pointing at a live aided probes GetStatus and updates in place to healthy.
    await app.evaluate((_electron, liveSock) => {
      (globalThis as unknown as TrayHook).aideTest.setDaemonRestarter(async () => {
        process.env.AIDE_SOCKET = liveSock;
      });
    }, fixture.socket);
    await restartBtn.click();
    await expect(panel.getByText("后台服务已恢复运行")).toBeVisible({ timeout: 15_000 });
    await expect(panel.getByRole("button", { name: "重启后台服务" })).toHaveCount(0);
    await expect.poll(() => app.evaluate(() => (globalThis as unknown as TrayHook).aideTest.tray().view.state)).not.toBe("daemon");
    await snap(panel, "tray-daemon-recovered");
  } finally {
    await app.close();
  }
});
