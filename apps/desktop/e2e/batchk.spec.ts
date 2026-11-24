import { expect, test, type ElectronApplication, type Locator, type Page } from "@playwright/test";
import { launch, navTo, snap } from "./app";

// 批 K（工单 #109–#119）的桌面端用例。世界里有第二台机器 work-mac 和它的 billing 项目（desktop_test.go 的 batchk 分支），
// 所以链路图右侧的项目节点带「类型 · 机器名」副标题。

/** Each text line of a node: on one line (its unscaled height under two lines'), and whether it shows whole. */
async function lines(node: Locator) {
  return node.evaluate((el) =>
    [...el.querySelectorAll<HTMLElement>(".lm-t b, .lm-t small")].map((t) => ({
      text: t.textContent ?? "",
      oneLine: t.offsetHeight < parseFloat(getComputedStyle(t).fontSize) * 1.8,
      whole: t.scrollWidth <= t.clientWidth,
    })),
  );
}

// 工单 #119: the subtitle "类型 · 机器" of a node on the map's right never breaks in the middle; it shows whole at the
// usual width, cut with an ellipsis only when there is no room; the right column's nodes are as tall as the left's.
test("the map's project nodes keep their subtitle on one line, whole at the usual width, cut not broken when narrow", async () => {
  const { app, page } = await launch();
  try {
    await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].setSize(1280, 860));
    await navTo(page, /^概览/);
    const map = page.getByRole("region", { name: "链路" });
    const billing = map.locator(".lm-node.p", { hasText: "billing" });
    await expect(billing).toBeVisible();
    const sub = billing.locator(".lm-t small");
    await expect(sub).toContainText("work-mac");
    expect(await sub.evaluate((el) => getComputedStyle(el).whiteSpace)).toBe("nowrap");
    for (const l of await lines(billing)) {
      expect(l.oneLine, `${l.text} on one line`).toBe(true);
      expect(l.whole, `${l.text} shown whole`).toBe(true);
    }
    // Every node on the right as tall as the others there and as the grants on the left (before scaling).
    const heights = await map.locator(".lm-node.p, .lm-node.g").evaluateAll((els) => els.map((e) => (e as HTMLElement).offsetHeight));
    expect(new Set(heights).size).toBe(1);
    await expect(sub).toHaveAttribute("title", /work-mac$/);
    await snap(page, "batchk-119-map-wide");

    // Narrow: the stage scales down, the text never breaks (an ellipsis is fine).
    await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].setSize(900, 860));
    await expect.poll(() => map.locator(".lmap-stage").evaluate((el) => el.style.transform)).not.toBe("scale(1)");
    for (const l of await lines(billing)) expect(l.oneLine, `${l.text} on one line when narrow`).toBe(true);
    // The legend and the headline count keep each of their items on one line too.
    for (const el of await map.locator(".lmap-foot span, .lmap-pill").all())
      expect(await el.evaluate((e) => getComputedStyle(e).whiteSpace), "legend items do not wrap").toBe("nowrap");
    await snap(page, "batchk-119-map-narrow");
  } finally {
    await app.close();
  }
});

/** The local machine's own page in Settings, its coding agents listed with how each is connected. */
async function machinePage(app: ElectronApplication, page: Page): Promise<Page> {
  const { machines } = await page.evaluate(async () => (await fetch("app://aide/api/aide.local.v1.LocalService/ListMachines", { method: "POST", headers: { "content-type": "application/json" }, body: "{}" })).json());
  const id = machines.find((m: { local?: boolean }) => m.local).machineId;
  const [win] = await Promise.all([app.waitForEvent("window"), page.evaluate((t) => window.aide?.openSettings(t), `machines/${id}`)]);
  await win.waitForLoadState();
  return win;
}

// 工单 #118: signing Claude Code in from Aide never claims a browser it cannot see. The row shows the address Claude
// Code printed, to open (on this computer, or copied to any device); the code the page shows goes back from here; a
// wrong one fails in place, said why; the right one signs it in, as Claude Code itself then says. The stand-in Claude
// Code (fakeclaude, via addressLoginClaude in e2e/batchk_test.go) prints its address and reads the code, as the real
// one does when no browser opens.
test("Claude Code signs in by its address and the code the page gives, not a browser it claims to have opened", async () => {
  const address = "https://sign-in.example/oauth/authorize?code=true&client_id=aide-e2e&state=batchk";
  const { app, page } = await launch();
  try {
    await app.evaluate(({ shell }) => {
      (globalThis as unknown as { opened: string[] }).opened = [];
      shell.openExternal = async (u: string) => void (globalThis as unknown as { opened: string[] }).opened.push(u);
    });
    const settings = await machinePage(app, page);
    const row = settings.getByRole("list", { name: "编码 Agent" }).getByRole("group", { name: "Claude Code 命令行" });
    await expect(row).toContainText("未登录", { timeout: 30_000 });
    await row.getByRole("button", { name: /^在.+上登录$/ }).click();

    // Its address, not a browser: nothing says one opened.
    const box = row.getByRole("group", { name: "打开登录地址" });
    await expect(box).toContainText(address, { timeout: 30_000 });
    await expect(row).toContainText("在任意设备上打开下面的登录地址");
    await expect(row).not.toContainText("已打开浏览器");
    await box.getByRole("button", { name: "打开登录地址" }).click();
    await expect.poll(() => app.evaluate(() => (globalThis as unknown as { opened: string[] }).opened)).toEqual([address]);
    await snap(settings, "batchk-118-address");

    // A wrong code: failed, in Claude Code's words, and it can be tried again.
    await box.getByRole("textbox", { name: "授权码" }).fill("bad-code");
    await box.getByRole("button", { name: "提交授权码" }).click();
    await expect(row).toContainText("上次登录没完成：OAuth error: Invalid code", { timeout: 30_000 });
    await snap(settings, "batchk-118-wrong-code");

    // The right one: signed in, as Claude Code's own `auth status` then says.
    await row.getByRole("button", { name: /^在.+上登录$/ }).click();
    await expect(row.getByRole("group", { name: "打开登录地址" })).toBeVisible({ timeout: 30_000 });
    await row.getByRole("textbox", { name: "授权码" }).fill("good-code");
    await row.getByRole("textbox", { name: "授权码" }).press("Enter");
    await expect(row).toContainText("已登录", { timeout: 30_000 });
    await expect(row.getByRole("group", { name: "打开登录地址" })).toHaveCount(0);
    await snap(settings, "batchk-118-signed-in");
  } finally {
    await app.close();
  }
});
