import { test } from "node:test";
import assert from "node:assert/strict";
import { presenceDot } from "./presence.ts";

// 审计 B / 工单 #135 P25: one colour per meaning — no two presences share a dot, and the four states the ticket names
// (在线 / 已登录 / 未确认 / 未登录) are green / blue / amber / red.
test("each presence has its own dot", () => {
  const all = ["online", "signedIn", "unconfirmed", "signedOut", "checking", "unknown"] as const;
  assert.equal(new Set(all.map(presenceDot)).size, all.length);
  assert.deepEqual(
    ["online", "signedIn", "unconfirmed", "signedOut"].map((p) => presenceDot(p as (typeof all)[number])),
    ["on", "ready", "warn", "bad"],
  );
  assert.equal(presenceDot("unknown"), "");
});
