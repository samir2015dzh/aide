import { test } from "node:test";
import assert from "node:assert/strict";
import { dispatchers, doingOf, rankOf, taskRows } from "./tasks.ts";

// session.proto's values (no TypeScript enums under node --test).
const IDLE = 4,
  RUNNING_STATUS = 3,
  WAITING = 3,
  ARCHIVED = 5;
const QUEUED = 1,
  RUNNING = 2,
  DONE_UNSEEN = 5,
  DONE = 6,
  FAILED = 7;
const LIVE = 1,
  STALLED = 2,
  LOST = 4;

const now = new Date("2026-11-22T10:00:00Z");
const ago = (min: number) => new Date(now.getTime() - min * 60_000);
const ts = (d: Date) => ({ seconds: BigInt(d.getTime() / 1000), nanos: 0 });
const session = (id: string, over: Record<string, unknown> = {}) =>
  ({ sessionId: id, machineId: "m1", projectId: "web", agent: "claude", createdBy: "c_muse", title: id, status: IDLE, createdAt: ts(ago(60)), updatedAt: ts(ago(1)), ...over }) as never;
const clients = [
  { clientId: "c_muse", label: "Muse" },
  { clientId: "c_grok", label: "Grok Bot" },
] as never;
// What every screen would work out, per session id.
const views: Record<string, { state: number; link: number; at: number }> = {
  done: { state: DONE, link: LIVE, at: 5 },
  mine: { state: RUNNING, link: LIVE, at: 1 },
  running: { state: RUNNING, link: LIVE, at: 1 },
  asks: { state: WAITING, link: LIVE, at: 3 },
  old: { state: DONE, link: LIVE, at: 60 * 30 },
  quiet: { state: RUNNING, link: STALLED, at: 20 },
  away: { state: RUNNING, link: LOST, at: 2 },
  queued: { state: QUEUED, link: LIVE, at: 0.5 },
};
const viewOf = (s: { sessionId: string }) => {
  const v = views[s.sessionId];
  return { state: v.state, link: v.link, lastEventAt: ago(v.at), quietMs: 0, silentMs: 0 } as never;
};

test("taskRows: the work agents gave out, needing a look first, then at work, then done; not the owner's, archived or long done", () => {
  const rows = taskRows(
    [
      session("done"),
      session("mine", { createdBy: "owner", status: RUNNING_STATUS }),
      session("running", { status: RUNNING_STATUS, progress: { currentTool: "Bash" } }),
      session("asks", { createdBy: "c_grok" }),
      session("archived", { status: ARCHIVED }),
      session("old"),
      session("quiet", { status: RUNNING_STATUS }),
      session("away", { status: RUNNING_STATUS }),
      session("queued"),
    ],
    clients,
    viewOf,
    now,
  );
  assert.deepEqual(
    rows.map((r) => r.session.sessionId),
    ["away", "asks", "quiet", "queued", "running", "done"],
  );
  assert.equal(rows[1].dispatcher, "Grok Bot");
  assert.deepEqual(rows[4].doing, { kind: "tool", text: "Bash" });
  assert.deepEqual(dispatchers(rows), [
    { id: "c_muse", name: "Muse", count: 5 },
    { id: "c_grok", name: "Grok Bot", count: 1 },
  ]);
});

test("taskRows: an agent no longer listed is named by its id", () => {
  const rows = taskRows([session("done", { createdBy: "c_gone" })], clients, viewOf, now);
  assert.equal(rows[0].dispatcher, "c_gone");
});

test("rankOf and doingOf", () => {
  assert.equal(rankOf({ state: DONE_UNSEEN, link: LIVE } as never), 2);
  assert.equal(rankOf({ state: FAILED, link: LIVE } as never), 0);
  assert.equal(rankOf({ state: DONE, link: LIVE } as never), 3);
  assert.deepEqual(doingOf({ title: "t", progress: { lastMessageExcerpt: "\n  Done: 3 files changed\nmore" } } as never, { state: DONE } as never), { kind: "said", text: "Done: 3 files changed" });
  assert.deepEqual(doingOf({ title: "", projectId: "web", progress: {} } as never, { state: DONE } as never), { kind: "title", text: "web" });
});
