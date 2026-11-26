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

test("dedupeLinkEntries: merges paired app/window observations within 5s and keeps distinct incidents (#138 P15)", () => {
  const base = 1_700_000_000_000;
  const entries: LinkEntry[] = [
    { at: base + 12_000, link: "aided", kind: "back", downFor: 3_000, from: "window" },
    { at: base + 11_700, link: "aided", kind: "back", downFor: 3_200, from: "app" },
    { at: base + 9_000, link: "aided", kind: "down", why: "lost", from: "window" },
    { at: base + 8_800, link: "aided", kind: "down", why: "lost", detail: "connect ECONNREFUSED", from: "app" },
    // Distinct earlier break (> 5s earlier) must remain separate.
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
