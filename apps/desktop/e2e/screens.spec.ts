import { expect, test } from "@playwright/test";
import { fixture, launch, onWindows, snap } from "./app";

// The website's download page shows the app in a demo world (e2e
// TestScreens, AIDE_SCREENS_DEMO=zh|en), on a Mac or a PC: the overview, a finished
// session, and a question the agent waits on,
// dark, at 1440×900 in twice as many pixels. It is no test of the app, and runs only then.
const lang = process.env.AIDE_SCREENS_DEMO;
test.skip(!lang, "the download page's screenshots (TestScreens)");

const zh = lang === "zh";
const system = onWindows ? "windows" : "mac";
const t = (z: string, e: string) => (zh ? z : e);

test("the download page's screenshots", async () => {
  test.setTimeout(5 * 60_000);
  const { app, page } = await launch(fixture.socket, { systemLanguages: [zh ? "zh-Hans" : "en"], dark: true, args: ["--force-device-scale-factor=2"] });
  await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].setContentSize(1440, 900));
  type Main = { aideTest: { mainWindow(): { webContents: { send(channel: string, ...args: unknown[]): void } } } };
  // As a window in front: the tests' windows never take focus (background.cjs).
  const front = () => app.evaluate(() => (globalThis as unknown as Main).aideTest.mainWindow().webContents.send("window-focus", true));
  const sessions = page.getByRole("list", { name: t("任务", "Tasks") });
  const open = async (title: string) => {
    await sessions.getByText(title, { exact: true }).click();
    await expect(page.getByRole("heading", { name: title })).toBeVisible();
  };

  // A picture has no vibrancy behind the sidebar (it comes out pale): its dark as macOS draws it.
  // And the account is the demo's, not this computer's user.
  await page.addStyleTag({
    content: [
      ".side{background:#1d1d21 !important}",
      ".side-foot .who b, .side-foot .av{color:transparent !important;position:relative}",
      '.side-foot .who b::after{content:"Alex";color:var(--l1);position:absolute;inset:0}',
      '.side-foot .av::after{content:"A";color:var(--l2);position:absolute;inset:0;display:grid;place-items:center}',
    ].join("\n"),
  });
  await expect(sessions.getByText(t("给设置页加深色模式", "Dark mode for Settings"), { exact: true })).toBeVisible();
  await front();
  await page.waitForTimeout(1500); // the overview's numbers and map settle
  await snap(page, `${system}-overview`);

  await open(t("修复登录按钮点击无反应 #142", "Fix Sign in doing nothing #142"));
  await front();
  await snap(page, `${system}-session`);

  await open(t("升级到 React 19", "Upgrade to React 19"));
  await front();
  await page.waitForTimeout(600);
  await snap(page, `${system}-question`);
  await app.close();
});
