import { expect, test, type ElectronApplication, type Page } from "@playwright/test";
import { launch, snap } from "./app";

// 工单 #109 / #107: Desktop Fluent visual acceptance — 40px centered-search titlebar, borderless main sidebar nav,
// folded "All conversations" flyout, bot context menu (Settings / Rename & change avatar), auto-download update moved
// next to version, Fluent tokens in light & dark modes, and side-by-side comparison screenshots.

async function openSettings(app: ElectronApplication, page: Page, tab?: string): Promise<Page> {
  const [win] = await Promise.all([app.waitForEvent("window"), page.evaluate((t) => window.aide?.openSettings(t), tab)]);
  await win.waitForLoadState();
  return win;
}

test("工单 #109 / #107: Desktop Fluent titlebar, sidebar, bot flyout, settings layout, light & dark", async () => {
  const { app, page } = await launch();
  await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].setContentSize(1280, 820));

  // 1. 40px top titlebar with centered search box, borderless main sidebar nav (7402986 ruling preserved).
  const titlebar = page.locator(".win-titlebar");
  await expect(titlebar).toBeVisible();
  const tbBox = await titlebar.boundingBox();
  expect(tbBox?.height).toBeCloseTo(40, 0);
  await expect(titlebar.locator(".side-searchbox")).toBeVisible();
  const navBorder = await page.locator("aside.side .side-nav").evaluate((el) => getComputedStyle(el).borderBottomStyle);
  expect(navBorder).toBe("none");

  // 2. Fluent light page token (#f3f2f1 = rgb(243, 242, 241)) & person DeviceFace.
  await expect(page.locator(".side-foot [data-face=person]")).toBeVisible();
  const lightBg = await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue("--surface-page").trim());
  expect(lightBg.toLowerCase()).toBe("#f3f2f1");
  await snap(page, "desktop-overview-light");

  await page.emulateMedia({ colorScheme: "dark" });
  const darkBg = await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue("--surface-page").trim());
  expect(darkBg.toLowerCase()).toBe("#1b1a19");
  await snap(page, "desktop-overview-dark");
  await page.emulateMedia({ colorScheme: "light" });

  // 3. Coding bot page: conversations folded into "全部对话" flyout popover (#107 §0.5c #4A).
  const roster = page.getByRole("region", { name: "Bot" });
  const codingBot = roster.locator(".bot-row").filter({ hasText: /Claude|Codex|DeepSeek/i }).first();
  if (await codingBot.count()) {
    await codingBot.click();
    const allBtn = page.locator(".conv-all-btn");
    await expect(allBtn).toBeVisible();
    await allBtn.click();
    await expect(page.locator(".conv-flyout")).toBeVisible();
    await snap(page, "desktop-bot-conv-flyout-light");
    await page.emulateMedia({ colorScheme: "dark" });
    await snap(page, "desktop-bot-conv-flyout-dark");
    await page.emulateMedia({ colorScheme: "light" });
    await page.locator(".conv-flyout .tb").click();
    await expect(page.locator(".conv-flyout")).toHaveCount(0);
  }

  // 4. Settings: General has auto-download update switch inside "关于 Aide" group next to version (#107 §0.5c #8 & §9).
  const settings = await openSettings(app, page, "general");
  const about = settings.getByRole("main", { name: "通用" }).getByRole("group", { name: "关于 Aide" });
  await expect(about.getByRole("switch", { name: "自动下载更新" })).toBeVisible();
  await snap(settings, "desktop-settings-general-light");
  await settings.emulateMedia({ colorScheme: "dark" });
  await snap(settings, "desktop-settings-general-dark");
  await settings.emulateMedia({ colorScheme: "light" });

  // 5. Account & Machines Fluent DeviceFace light & dark snapshots.
  await settings.getByRole("navigation", { name: "设置" }).getByRole("button", { name: /^账号与设备/ }).click();
  await expect(settings.locator(".dhero [data-face=person]")).toBeVisible();
  await snap(settings, "desktop-settings-account-light");
  await settings.emulateMedia({ colorScheme: "dark" });
  await snap(settings, "desktop-settings-account-dark");
  await settings.emulateMedia({ colorScheme: "light" });

  await settings.getByRole("navigation", { name: "设置" }).getByRole("button", { name: /^机器/ }).click();
  await expect(settings.locator("[data-face]").first()).toBeVisible();
  await snap(settings, "desktop-settings-machines-light");
  await settings.emulateMedia({ colorScheme: "dark" });
  await snap(settings, "desktop-settings-machines-dark");

  await app.close();
});

// 审计 K / 工单 #109: the keyboard focus ring is an outline, so the box-shadows buttons and segmented controls draw for
// themselves (.btn, .btn.primary, .seg button.on) can no longer cover it — they did when the ring was a box-shadow.
test("审计 K / 工单 #109: the keyboard focus ring stays on buttons that draw their own shadow, light and dark", async () => {
  const { app, page } = await launch();
  await page.evaluate(() => {
    const box = document.createElement("div");
    box.id = "focus-probe";
    box.innerHTML =
      '<button class="btn" id="fp-0">0</button><button class="btn" id="fp-1">1</button>' +
      '<button class="btn primary" id="fp-2">2</button><div class="seg"><button class="on" id="fp-3">3</button></div>';
    document.body.append(box);
  });
  const ring = (id: string) =>
    page.locator(`#${id}`).evaluate((el) => {
      const s = getComputedStyle(el);
      return { focused: el.matches(":focus-visible"), style: s.outlineStyle, width: s.outlineWidth, color: s.outlineColor };
    });
  for (const [scheme, color] of [
    ["light", "rgb(0, 120, 212)"],
    ["dark", "rgb(71, 158, 245)"],
  ] as const) {
    await page.emulateMedia({ colorScheme: scheme });
    await page.locator("#fp-0").focus();
    for (const id of ["fp-1", "fp-2", "fp-3"]) {
      await page.keyboard.press("Tab");
      expect(await ring(id)).toEqual({ focused: true, style: "solid", width: "2px", color });
    }
  }
  await page.emulateMedia({ colorScheme: "light" });
  await app.close();
});
