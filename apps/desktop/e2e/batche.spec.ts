import { expect, test, type ElectronApplication, type Page } from "@playwright/test";
import { launch, onWindows, snap } from "./app";

// 批 E (工单 #51/#52), as the owner does it: Claude Code's command line, not
// signed in, is signed in from this Mac's page with one button (a browser
// would open on that computer; here its `auth login` stands in), the row
// following it to signed in, no terminal; and an agent's kind, told from its
// name, set by the owner on its page, wins on the map, and goes back to its
// name on "told from its name".

test.skip(onWindows, "its stand-in Claude Code is a shell script");

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

test("Claude Code's command line signed in from this Mac's page, no terminal", async () => {
  const { app, page } = await launch();
  const { machines } = await api<{ machines: { machineId: string; local?: boolean }[] }>(page, "ListMachines");
  const settings = await openSettings(app, page, `machines/${machines.find((m) => m.local)!.machineId}`);
  const row = settings.getByRole("list", { name: "编码 Agent" }).getByRole("group", { name: "Claude Code 命令行" });
  await expect(row).toContainText("未登录", { timeout: 30_000 });
  await expect(row).toContainText("会在这台电脑上打开浏览器");
  await snap(settings, "batche-login-before");
  await row.getByRole("button", { name: "在这台电脑上登录" }).click();
  await expect(row).toContainText("正在登录…");
  await expect(row.getByRole("button", { name: "取消登录" })).toBeVisible();
  await snap(settings, "batche-login-waiting");
  await expect(row).toContainText("已登录", { timeout: 30_000 });
  await snap(settings, "batche-login-done");
  await app.close();
});

test("an agent's kind: told from its name, set by the owner, shown so on the map, and back to its name", async () => {
  const { app, page } = await launch();
  const map = page.getByRole("region", { name: "链路" });
  const bot = map.getByRole("button", { name: "bot", exact: true });
  await expect(bot).toContainText("个人 agent");
  const settings = await openSettings(app, page, "grants");
  await settings.getByRole("list", { name: "授权" }).getByRole("button", { name: /bot/ }).click();
  const kind = settings.getByRole("combobox", { name: "类别" });
  await expect(kind).toHaveValue("");
  await expect(kind.locator("option").first()).toHaveText("自动判断 · 个人 Agent");
  const { machines } = await api<{ machines: { machineId: string; local?: boolean }[] }>(page, "ListMachines");
  await kind.selectOption(`local:${machines.find((m) => m.local)!.machineId}`);
  await expect(settings.getByText("本机编码 Agent 归属它所在的机器，派活要先选那台机器。")).toBeVisible();
  await snap(settings, "batche-kind-set");
  // On the map it is drawn in its machine, no longer on the account's side.
  await expect(bot).toHaveCount(0, { timeout: 30_000 });
  await expect(map.locator(".lm-home")).toHaveCount(1);
  await kind.selectOption("");
  await expect(bot).toContainText("个人 agent", { timeout: 30_000 });
  await app.close();
});
