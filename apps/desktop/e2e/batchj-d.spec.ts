import { _electron as electron, expect, test, type ElectronApplication, type Page } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";
import { background, cli, fixture, launch, snap } from "./app";

// 批 J · D: a sender by its connection's name now (工单 #101), the version and Check for Updates in Settings (#103).
// The account's and the devices' faces (#100) and the bubble's colours (#101) wait for the design drafts (#106/#107).

async function openSettings(app: ElectronApplication, page: Page, tab?: string): Promise<Page> {
  const [win] = await Promise.all([app.waitForEvent("window"), page.evaluate((t) => window.aide?.openSettings(t), tab)]);
  await win.waitForLoadState();
  return win;
}

async function api<T>(page: Page, method: string, body: object = {}): Promise<T> {
  return page.evaluate(
    async ([m, b]) => (await fetch(`app://aide/api/aide.local.v1.LocalService/${m}`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(b) })).json(),
    [method, body] as const,
  );
}

// A conversation begun while the agent's connection was named "bot (127.0.0.1)", as MCP connections were before
// 工单 #85: once the name is "bot" again, the conversation says "bot", its earlier answer too (the room on the machine
// follows the grant's name).
test("工单 #101: a sender kept as \"bot (127.0.0.1)\" reads as its connection's name now", async () => {
  const { page } = await launch();
  const grant = async () => (await api<{ clients: { grantId: string; label: string }[] }>(page, "ListClients")).clients.find((c) => c.label.startsWith("bot"))!;
  const rename = async (label: string) => {
    await api(page, "RenameClient", { grantId: (await grant()).grantId, label });
    await expect.poll(async () => (await grant()).label, { timeout: 30_000 }).toBe(label);
  };
  await rename("bot (127.0.0.1)");
  const roster = page.getByRole("region", { name: "Bot" });
  await roster.getByRole("group", { name: "个人 agent" }).getByRole("button", { name: /^bot/ }).click();
  const botPage = page.getByRole("region", { name: /^bot/ }).first();
  const log = botPage.getByRole("log", { name: "对话" });
  await botPage.getByRole("textbox", { name: /^给 bot/ }).fill("在吗");
  await botPage.getByRole("button", { name: "发送" }).click();
  let letter: { id: string; from: { inbox: string; session: string } } | undefined;
  await expect.poll(async () => {
    const { messages } = JSON.parse(await cli("message", "list", "-o", "json")) as { messages: (typeof letter & { text: string })[] };
    letter = messages.find((m) => m!.from.session?.startsWith("room_") && m!.text.includes("在吗"));
    return !!letter;
  }, { timeout: 30_000 }).toBe(true);
  await cli("message", "send", "--to", `${letter!.from.inbox}/${letter!.from.session}`, "--reply-to", letter!.id, "--message", "在的");
  await expect(log.locator(".chat-msg:not(.mine) .chat-who")).toHaveText("bot (127.0.0.1)", { timeout: 30_000 });

  await rename("bot");
  await expect.poll(async () => (await log.locator(".chat-msg:not(.mine) .chat-who").allTextContents()).join("|"), { timeout: 30_000 }).toBe("bot");
  await expect(log).not.toContainText("127.0.0.1");
  await snap(page, "batchj-d-sender-renamed");
});

test("工单 #103: General opens with the version the app runs; Check for Updates says what it found", async () => {
  const { app, page } = await launch();
  const version = await app.evaluate(({ app }) => app.getVersion());
  const settings = await openSettings(app, page);
  const general = settings.getByRole("main", { name: "通用" });
  const about = general.getByRole("group", { name: "关于 Aide" });
  await expect(about.getByText(`版本 ${version}`, { exact: true })).toBeVisible();
  // A development build: no updates, said so, the button off.
  await expect(about.getByRole("status", { name: "检查更新的结果" })).toHaveText("开发版本不检查更新");
  await expect(about.getByRole("button", { name: "检查更新" })).toBeDisabled();
  await snap(settings, "batchj-d-general-dev");
  await app.close();
});

/**
 * The app as installed (app.isPackaged), on this system: its data where the installed app keeps it (AIDE_DATA_DIR),
 * talking to the world's daemon, its version 1.0.0, and the update feed a stand-in the test answers (latest, fails).
 */
async function installed(): Promise<{ app: ElectronApplication; home: string }> {
  const home = fs.mkdtempSync("/tmp/hmj-");
  const data = path.join(home, "d");
  fs.mkdirSync(path.join(data, "run"), { recursive: true });
  fs.symlinkSync(fixture.socket, path.join(data, "run", "aided.sock"));
  fs.mkdirSync(path.join(data, "desktop"));
  fs.writeFileSync(path.join(data, "desktop", "preferences.json"), JSON.stringify({ language: "zh-CN" }));
  fs.writeFileSync(path.join(data, "desktop", "updates.json"), JSON.stringify({ autoDownload: false }));
  const entry = path.join(process.env.AIDE_DESKTOP_OUT || path.join(import.meta.dirname, "..", "out"), "main", "index.js");
  const bootstrap = path.join(home, "start.cjs");
  fs.writeFileSync(bootstrap, `
    const { app, autoUpdater, net } = require("electron");
    Object.defineProperty(app, "isPackaged", { get: () => true });
    app.getVersion = () => "1.0.0";
    app.setAsDefaultProtocolClient = () => true;
    const state = globalThis.feed = { latest: "1.0.0", fails: false, requests: 0 };
    autoUpdater.setFeedURL = () => {};
    autoUpdater.checkForUpdates = () => {};
    const fetch = net.fetch.bind(net);
    net.fetch = async (url, ...args) => {
      if (!String(url).includes("/RELEASES-")) return fetch(url, ...args);
      state.requests++;
      const zh = { improvements: ["在 Aide 中检查更新。"], fixes: [] };
      return new Response(JSON.stringify({ currentRelease: state.latest, aideNotes: [{ version: state.latest, summary: { en: zh, "zh-CN": zh } }] }), { status: state.fails ? 503 : 200 });
    };
    require("node:module").syncBuiltinESMExports();
    import(require("node:url").pathToFileURL(${JSON.stringify(entry)}).href);
  `);
  const env = { ...process.env, AIDE_DATA_DIR: data } as Record<string, string>;
  delete env.AIDE_SOCKET;
  const app = await electron.launch({ args: ["-r", background, bootstrap], env });
  return { app, home };
}

test("工单 #103: Check for Updates on an installed app: up to date, a newer version (on to the Updates window), a failure", async () => {
  const { app, home } = await installed();
  try {
    const main = await app.firstWindow();
    await main.locator("aside.side").first().waitFor({ timeout: 30_000 });
    const settings = await openSettings(app, main);
    const about = settings.getByRole("main", { name: "通用" }).getByRole("group", { name: "关于 Aide" });
    await expect(about.getByText("版本 1.0.0", { exact: true })).toBeVisible();
    const said = about.getByRole("status", { name: "检查更新的结果" });
    const check = about.getByRole("button", { name: "检查更新" });

    await check.click();
    await expect(said).toHaveText("已是最新版本 1.0.0");
    await snap(settings, "batchj-d-check-current");

    await app.evaluate(() => { (globalThis as unknown as { feed: { latest: string } }).feed.latest = "1.2.0"; });
    await check.click();
    await expect(said).toContainText("有新版本 1.2.0");
    await snap(settings, "batchj-d-check-available");
    // The newer version is taken on where the menu takes it: the Updates window.
    const [updates] = await Promise.all([app.waitForEvent("window"), said.getByRole("button", { name: "查看并更新…" }).click()]);
    await expect(updates.getByRole("status")).toHaveText("有可用更新");
    await expect(updates.getByRole("button", { name: "下载更新" })).toBeVisible();
    await updates.close();

    await app.evaluate(() => { (globalThis as unknown as { feed: { fails: boolean } }).feed.fails = true; });
    await check.click();
    await expect(said).toContainText("没能检查更新，请检查网络连接。");
    await snap(settings, "batchj-d-check-failed");
    expect(await app.evaluate(() => (globalThis as unknown as { feed: { requests: number } }).feed.requests)).toBeGreaterThanOrEqual(3);
  } finally {
    await app.close();
    fs.rmSync(home, { recursive: true, force: true });
  }
});
