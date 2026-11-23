import { expect, test, type Page } from "@playwright/test";
import { cli, fixture, launch, snap } from "./app";

// 工单 #83, as the owner does it: every bot on the roster is a live character (BotArk's Grok Bot engine draws it), and
// only its real state moves it — this computer's Gemini CLI (the scripted agent) runs a shell command, and its face
// runs, then is done, then rests. Its look is changed from its page: a click on its face opens the one avatar editor
// (the live stage, 26 shapes, 11 colours; no emoji left), a shape and a colour picked are kept with its contact and
// drawn on the roster, and changed again from the same place.

async function api<T>(page: Page, method: string, body: object = {}): Promise<T> {
  return page.evaluate(
    async ([m, b]) => (await fetch(`app://aide/api/aide.local.v1.LocalService/${m}`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(b) })).json(),
    [method, body] as const,
  );
}

test("the roster's faces are live characters moved by real state; the look picked in the editor is kept and drawn", async () => {
  const { app, page } = await launch();
  await page.setViewportSize({ width: 1440, height: 860 });
  const here = page.getByRole("region", { name: "Bot" }).getByRole("group", { name: "编码 agent · 跟机器走" });
  const row = here.getByRole("button", { name: /^Gemini CLI/ });
  const mark = row.locator(".sand-grok-bot-mark");
  // The engine draws it: an SVG it renders frame by frame, not a picture.
  await expect(mark.locator("svg path").first()).toBeAttached({ timeout: 30_000 });
  await expect(page.locator(".side-bots img")).toHaveCount(0);
  // Its world: a question waits on the owner (选颜色), so it is blocked; that turn stopped, its long shell command
  // shows (跑很久), so it runs; that stopped too, it rests.
  await expect(mark).toHaveAttribute("data-grok-state", "blocked", { timeout: 30_000 });
  await snap(page, "avatar-roster-blocked");
  await cli("session", "interrupt", fixture.sessions["选颜色"]);
  await expect(mark).toHaveAttribute("data-grok-state", "running", { timeout: 30_000 });
  await cli("session", "interrupt", fixture.sessions["跑很久"]);
  await expect(mark).toHaveAttribute("data-grok-state", "idle", { timeout: 30_000 });

  // At work: a shell command runs (running), then it is done (finished), then it rests (idle).
  const started = cli("session", "start", "--project", "demo", "--agent", "gemini", "--title", "动一动", "--prompt", "shell sleep 4\nsay 好了", "--wait");
  await expect(mark).toHaveAttribute("data-grok-state", "running", { timeout: 30_000 });
  await snap(page, "avatar-roster-running");
  await started;
  await expect(mark).toHaveAttribute("data-grok-state", "finished", { timeout: 30_000 });
  await expect(mark).toHaveAttribute("data-grok-state", "idle", { timeout: 30_000 });

  // Its look, from its page: a click on its face opens the editor (a new contact: it has none yet).
  await row.click();
  const bot = page.getByRole("region", { name: /^Gemini CLI/ });
  await bot.getByRole("button", { name: "换头像", exact: true }).click();
  const form = page.getByRole("dialog", { name: "添加联系人" });
  const shapes = form.getByRole("group", { name: "形状" });
  const colors = form.getByRole("group", { name: "颜色" });
  await expect(shapes.getByRole("button")).toHaveCount(26);
  await expect(colors.getByRole("button")).toHaveCount(11);
  await expect(form.getByRole("button", { name: /fox|owl|robot/ })).toHaveCount(0);
  // The stage is live (it follows the pointer and is never paused); the grid's cells rest.
  const stage = form.getByRole("button", { name: "戳它一下" }).locator(".sand-grok-bot-mark");
  await expect(stage).not.toHaveAttribute("data-paused", "true");
  await expect(shapes.locator(".sand-grok-bot-mark").first()).toHaveAttribute("data-paused", "true");
  await shapes.getByRole("button", { name: "爱心" }).click();
  await colors.getByRole("button", { name: "品红" }).click();
  await expect(stage).toHaveAttribute("data-grok-shape", "heart");
  await expect(stage).toHaveAttribute("data-grok-color", "magenta");
  await form.getByLabel("名字", { exact: true }).fill("形象测试");
  await snap(page, "avatar-editor");
  await form.getByRole("button", { name: "保存" }).click();

  // Drawn on the roster in that look, and kept with its contact.
  const named = here.getByRole("button", { name: /^形象测试/ }).locator(".sand-grok-bot-mark");
  await expect(named).toHaveAttribute("data-grok-shape", "heart");
  await expect(named).toHaveAttribute("data-grok-color", "magenta");
  const kept = async () => (await api<{ personas?: { name: string; icon: string; color: string }[] }>(page, "ListPersonas")).personas?.find((p) => p.name === "形象测试");
  expect(await kept()).toMatchObject({ icon: "heart", color: "#FF309B" });

  // Changed again from the same face: the contact's own editor, the same one.
  await page.getByRole("region", { name: "形象测试" }).getByRole("button", { name: "换头像", exact: true }).click();
  const edit = page.getByRole("dialog", { name: "编辑联系人" });
  await expect(edit.getByRole("group", { name: "形状" }).getByRole("button", { name: "爱心" })).toHaveAttribute("aria-pressed", "true");
  await edit.getByRole("group", { name: "形状" }).getByRole("button", { name: "星星" }).click();
  await edit.getByRole("group", { name: "颜色" }).getByRole("button", { name: "青" }).click();
  await edit.getByRole("button", { name: "保存" }).click();
  await expect(named).toHaveAttribute("data-grok-shape", "star");
  await expect(named).toHaveAttribute("data-grok-color", "cyan");
  expect(await kept()).toMatchObject({ icon: "star", color: "#00BCA6" });

  // Still so after the window is opened again.
  await page.reload();
  await expect(here.getByRole("button", { name: /^形象测试/ }).locator(".sand-grok-bot-mark")).toHaveAttribute("data-grok-shape", "star", { timeout: 30_000 });
  await snap(page, "avatar-roster");

  // Back as it was.
  await here.getByRole("button", { name: /^形象测试/ }).click();
  await page.getByRole("region", { name: "形象测试" }).getByRole("button", { name: "换头像", exact: true }).click();
  await page.getByRole("dialog", { name: "编辑联系人" }).getByRole("button", { name: "删除联系人" }).click();
  await expect(here.getByRole("button", { name: /^Gemini CLI/ })).toBeVisible();
  await app.close();
});
