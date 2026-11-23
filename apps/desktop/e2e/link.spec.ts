import { expect, test, type ElectronApplication, type Page } from "@playwright/test";
import fs from "node:fs";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import { fixture, launch, openWork, snap } from "./app";

// The app's link to aided on this computer (工单 #87): it breaks (here: aided's socket, through a stand-in in front of
// it, goes away as when aided stops), and while it is down the window keeps what it knew, says the link is being
// restored, and calls no agent offline; once aided answers again it comes back by itself, no restart of the app. The
// break and its end are in the settings' records.

/** A stand-in socket in front of aided's: up() forwards to it, down() drops every connection and stops answering. */
function standIn() {
  const sock = path.join(fs.mkdtempSync(path.join(os.tmpdir(), "hmlink-")), "aided.sock");
  const open = new Set<net.Socket>();
  let server: net.Server | undefined;
  return {
    sock,
    up: () =>
      new Promise<void>((resolve) => {
        fs.rmSync(sock, { force: true });
        server = net.createServer((c) => {
          const u = net.connect(fixture.socket);
          open.add(c).add(u);
          const end = () => (c.destroy(), u.destroy(), open.delete(c), open.delete(u));
          c.pipe(u).pipe(c);
          c.on("error", end).on("close", end);
          u.on("error", end).on("close", end);
        });
        server.listen(sock, resolve);
      }),
    down: () =>
      new Promise<void>((resolve) => {
        for (const s of open) s.destroy();
        open.clear();
        if (!server) return resolve();
        server.close(() => resolve());
      }),
  };
}

async function openSettings(app: ElectronApplication, page: Page, tab: string): Promise<Page> {
  const [win] = await Promise.all([app.waitForEvent("window"), page.evaluate((t) => window.aide?.openSettings(t), tab)]);
  await win.waitForLoadState();
  return win;
}

test("aided gone and back: reconnecting meanwhile, no agent called offline, back by itself, kept in the records", async () => {
  const link = standIn();
  await link.up();
  const { app, page } = await launch(link.sock);
  try {
    await openWork(app, page);
    const roster = page.getByRole("region", { name: "Bot" });
    const claude = roster.getByRole("button", { name: /^Claude Code/ });
    await expect(claude).toContainText("已登录", { timeout: 30_000 });
    await claude.click();
    const bot = page.getByRole("region", { name: /^Claude Code/ });
    await expect(bot.locator(".bot-pill")).toHaveText("已登录");

    await link.down();
    const banner = page.getByRole("status").filter({ hasText: "与这台电脑上 Aide 后台的连接中断，正在重连" });
    await expect(banner).toBeVisible({ timeout: 15_000 });
    // The page stays, every bot says it is reconnecting: not offline, not unknown.
    await expect(bot.locator(".bot-pill")).toHaveText("正在重连");
    await expect(claude).toContainText("正在重连");
    await expect(roster).not.toContainText("离线");
    await expect(roster).not.toContainText("状态未知");
    await snap(page, "link-reconnecting");

    await link.up();
    await expect(banner).toBeHidden({ timeout: 15_000 });
    await expect(bot.locator(".bot-pill")).toHaveText("已登录", { timeout: 15_000 });

    const settings = await openSettings(app, page, "ops");
    const records = settings.getByRole("list", { name: "这台电脑的连接" });
    await expect(records.getByRole("listitem").filter({ hasText: /Aide 后台(停了或正在重启|的连接断开)/ }).first()).toBeVisible({ timeout: 15_000 });
    await expect(records.getByRole("listitem").filter({ hasText: "已自动重新连上这台电脑上的 Aide 后台" }).first()).toBeVisible();
    await snap(settings, "link-records");
  } finally {
    await app.close();
    await link.down();
  }
});
