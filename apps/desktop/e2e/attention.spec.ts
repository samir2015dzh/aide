import { expect, test, type ElectronApplication } from "@playwright/test";
import { cli, fixture, launch } from "./app";
import { sessionList } from "./sessions";

// Attention triage (docs/session-states.md §2, §7): an agent's finished turn is a new result, in a section of its
// own after what needs the owner, until the owner opens it; then it is done, on every device, for its machine keeps
// what the owner saw. A question this desktop notified of is told to its machine, with whether the owner is at the
// desktop (AIDE_TEST_PRESENCE stands in for the system's idle time), so a phone holds it back, or stays silent.

type Hook = { world(): { sessions: { sessionId: string; seen?: { turns: number; questionId: string; questionPresent: boolean } }[] } };
const seenOf = (app: ElectronApplication, id: string) =>
  app.evaluate((_, id) => {
    const s = (globalThis as unknown as { aideTest: Hook }).aideTest.world().sessions.find((x) => x.sessionId === id);
    return s?.seen ? { turns: s.seen.turns, questionId: s.seen.questionId, questionPresent: s.seen.questionPresent } : null;
  }, id);

test("a new result waits under 有新结果未看 until opened; opened, it is done, here and for every device", async () => {
  const { app, page } = await launch(fixture.socket, { env: { AIDE_TEST_PRESENCE: "away" } });
  const list = await sessionList(page);
  const group = (name: string) => list.getByRole("group", { name: new RegExp(`^${name}`) });
  // The fixture's finished sessions were opened before (ownerOpened); an agent's turn ends now.
  const id = JSON.parse(await cli("session", "start", "--project", "demo", "--agent", "gemini", "--title", "新结果", "--prompt", "say 好了", "--wait", "-o", "json")).session.session_id;
  await expect(group("有新结果未看")).toContainText("新结果");
  await expect(list.locator(".grp-h")).toHaveText(["需要处理", "有新结果未看", "进行中", "已结束"]);
  await expect(group("需要处理")).toContainText("选颜色");
  await expect(group("已结束")).toContainText("写 hello");
  const state = async () => JSON.parse(await cli("session", "get", id, "-o", "json")).session.state;
  expect(await state()).toBe("done_unseen");
  // Not by itself: it stays while the owner looks elsewhere.
  await page.waitForTimeout(2000);
  await expect(group("有新结果未看")).toContainText("新结果");

  await group("有新结果未看").getByText("新结果", { exact: true }).click();
  await expect(page.getByRole("heading", { name: "新结果" })).toBeVisible();
  await expect(group("已结束")).toContainText("新结果");
  await expect(group("有新结果未看")).toHaveCount(0);
  await expect.poll(state).toBe("done");
  await app.close();

  // Another window, nothing remembered here: still done (the machine's word).
  const again = await launch(fixture.socket, { env: { AIDE_TEST_PRESENCE: "away" } });
  await expect((await sessionList(again.page)).getByRole("group", { name: /^已结束/ })).toContainText("新结果");
  await again.app.close();
});

test("a question notified here is told to its machine; the owner at the desktop, too", async () => {
  const waiting = fixture.sessions["选颜色"];
  const q = JSON.parse(await cli("session", "get", waiting, "-o", "json")).session.question;
  const away = await launch(fixture.socket, { env: { AIDE_TEST_PRESENCE: "away" } });
  await expect.poll(() => seenOf(away.app, waiting), { timeout: 30_000 }).toMatchObject({ questionId: q.question_id, questionPresent: false });
  await away.app.close();
  const here = await launch(fixture.socket, { env: { AIDE_TEST_PRESENCE: "here" } });
  await expect.poll(() => seenOf(here.app, waiting), { timeout: 30_000 }).toMatchObject({ questionId: q.question_id, questionPresent: true });
  await here.app.close();
});
