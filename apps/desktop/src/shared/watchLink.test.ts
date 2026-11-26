import { test } from "node:test";
import assert from "node:assert/strict";
import { aidedGone, dedupeLinkEntries, healDelay, keepWatching, type LinkEntry } from "./watchLink.ts";

/** A watch that yields msgs, then fails with fail, or (hang) waits until aborted, as a stuck stream does. */
function watch(msgs: string[], end: { fail?: string; hang?: boolean }) {
  return async function* (signal: AbortSignal) {
    for (const m of msgs) yield m;
    if (end.hang) {
      await new Promise<void>((_, reject) => {
        if (signal.aborted) reject(new Error("aborted"));
        signal.addEventListener("abort", () => reject(new Error("aborted")), { once: true });
      });
    }
    if (end.fail) throw new Error(end.fail);
  };
}

test("keepWatching: a lost watch goes down once, is watched again, and comes back with its first message", async () => {
  const ctl = new AbortController();
  const said: string[] = [];
  const rounds = [watch(["a"], { fail: "aided: connect ENOENT /tmp/x.sock" }), watch([], { fail: "aided: connect ENOENT /tmp/x.sock" }), watch(["b"], { hang: true })];
  let i = 0;
  await keepWatching({
    open: (s) => rounds[Math.min(i++, rounds.length - 1)](s),
    message: (m) => (said.push(`msg ${m}`), m === "b" && ctl.abort()),
    down: (why, detail) => said.push(`down ${why} ${aidedGone(detail)}`),
    up: () => said.push("up"),
    signal: ctl.signal,
    retry: 1,
  });
  assert.deepEqual(said, ["msg a", "down lost true", "up", "msg b"]);
});

test("keepWatching: a watch that hears nothing for the silence hangs: dropped, down as hung, watched again", async () => {
  const ctl = new AbortController();
  const said: string[] = [];
  let i = 0;
  await keepWatching({
    open: (s) => (i++ === 0 ? watch(["a"], { hang: true }) : watch(["b"], { hang: true }))(s),
    message: (m) => (said.push(`msg ${m}`), m === "b" && ctl.abort()),
    down: (why, detail) => said.push(`down ${why} ${detail}`),
    up: () => said.push("up"),
    signal: ctl.signal,
    silence: 60,
    retry: 1,
  });
  assert.deepEqual(said, ["msg a", "down hung ", "up", "msg b"]);
});

test("aidedGone: aided not there at all, told from a link that broke", () => {
  assert.equal(aidedGone("aided: connect ECONNREFUSED"), true);
  assert.equal(aidedGone("[unavailable] aided: connect ENOENT /run/aide.sock"), true);
  assert.equal(aidedGone("[internal] protocol error"), false);
  assert.equal(aidedGone(undefined), false);
});

test("healDelay: aided started again soon after it went down, then every five minutes while it stays down (工单 #128)", () => {
  assert.deepEqual([0, 1, 2, 3, 10].map(healDelay), [30_000, 60_000, 120_000, 300_000, 300_000]);
});

test("dedupeLinkEntries: merges paired app/window observations within the window and keeps distinct incidents (#138 P15)", () => {
  const base = 1_700_000_000_000;
  const entries: LinkEntry[] = [
    { at: base + 12_000, link: "aided", kind: "back", downFor: 3_000, from: "window" },
    { at: base + 11_700, link: "aided", kind: "back", downFor: 3_200, from: "app" },
    { at: base + 9_000, link: "aided", kind: "down", why: "lost", from: "window" },
    { at: base + 8_800, link: "aided", kind: "down", why: "lost", detail: "connect ECONNREFUSED", from: "app" },
    // Distinct earlier break (more than the window earlier) must remain separate.
    { at: base, link: "aided", kind: "down", why: "hung", from: "app" },
    // Different link at the same timestamp must remain separate.
    { at: base + 9_100, link: "relay", kind: "down", from: "app" },
  ];
  const merged = dedupeLinkEntries(entries);
  assert.equal(merged.length, 4);
  assert.equal(merged[0].kind, "back");
  assert.equal(merged[0].from, "app");
  assert.equal(merged[1].kind, "down");
  assert.equal(merged[1].detail, "connect ECONNREFUSED");
  assert.equal(merged[2].why, "hung");
  assert.equal(merged[3].link, "relay");
});

// 审计 B（#138）：只合并不同来源（应用与窗口）对同一事件的两次记录。同一来源每 4 秒断开又恢复一次是崩溃循环，
// 每一条都要留下（原先 12 条被连锁合成 6 条，掩盖了 #128 的崩溃循环）。
test("dedupeLinkEntries: one source flapping every 4s keeps every entry", () => {
  const base = 1_700_000_000_000;
  for (const from of ["app", "window"] as const) {
    const entries: LinkEntry[] = [];
    for (let i = 0; i < 6; i++) {
      entries.push({ at: base + i * 4_000, link: "aided", kind: "down", why: "lost", detail: "connect ECONNREFUSED", from });
      entries.push({ at: base + i * 4_000 + 2_000, link: "aided", kind: "back", downFor: 2_000, from });
    }
    assert.equal(dedupeLinkEntries(entries).length, 12);
  }
});

// 审计 B（#138）：合并时留下更具体的证据——应用看到的 lost + ECONNREFUSED（aided 根本不在）胜过窗口的光秃秃的 hung；
// 恢复时长只有一方记了也带上。
test("dedupeLinkEntries: merging keeps the more specific evidence", () => {
  const base = 1_700_000_000_000;
  const merged = dedupeLinkEntries([
    { at: base, link: "aided", kind: "down", why: "hung", from: "window" },
    { at: base + 1_500, link: "aided", kind: "down", why: "lost", detail: "aided: connect ECONNREFUSED", from: "app" },
    { at: base + 9_000, link: "aided", kind: "back", from: "app" },
    { at: base + 9_200, link: "aided", kind: "back", downFor: 7_500, from: "window" },
  ]);
  assert.equal(merged.length, 2);
  assert.deepEqual([merged[0].why, merged[0].detail, aidedGone(merged[0].detail)], ["lost", "aided: connect ECONNREFUSED", true]);
  assert.equal(merged[1].downFor, 7_500);
});

// 审计 B（#138）：窗口按每组最早那条原始记录算，不随合并后的代表连锁延伸；两个来源一组最多各一条。
test("dedupeLinkEntries: the window is measured from the group's first entry, never chained", () => {
  const base = 1_700_000_000_000;
  const merged = dedupeLinkEntries([
    { at: base, link: "aided", kind: "down", why: "hung", from: "app" },
    // Within the window of the first: merged, and wins (more specific).
    { at: base + 6_000, link: "aided", kind: "down", why: "lost", detail: "connect ECONNREFUSED", from: "window" },
    // Within the window of the representative (6 s) but not of the group's first (12 s), and its source is taken.
    { at: base + 12_000, link: "aided", kind: "down", why: "lost", detail: "connect ECONNREFUSED", from: "app" },
    { at: base + 13_000, link: "aided", kind: "down", why: "lost", from: "window" },
  ]);
  assert.deepEqual(merged.map((e) => [e.at - base, e.from]), [[6_000, "window"], [12_000, "app"]]);
});
