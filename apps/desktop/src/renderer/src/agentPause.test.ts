import { test } from "node:test";
import assert from "node:assert/strict";
import { autoResumedAt, pauseOf, usageResetAt } from "./agentPause.ts";

const now = new Date("2026-11-22T10:00:00Z");
const ts = (iso: string) => ({ seconds: BigInt(Date.parse(iso) / 1000), nanos: 0 });
const win = (utilization: number, resets?: string) => ({ key: "", utilization, resetsAt: resets ? ts(resets) : undefined });
const usage = (...windows: ReturnType<typeof win>[]) => ({ usage: { windows } }) as never;

test("usageResetAt: the latest reset of the windows used up", () => {
  const a = usage(win(100, "2026-11-22T12:00:00Z"), win(100, "2026-11-26T07:00:00Z"), win(40, "2026-11-22T11:00:00Z"));
  assert.equal(usageResetAt(a, now)?.toISOString(), "2026-11-26T07:00:00.000Z");
});

test("usageResetAt: none used up, the fullest window's reset", () => {
  const a = usage(win(30, "2026-11-22T11:00:00Z"), win(95, "2026-11-25T00:00:00Z"));
  assert.equal(usageResetAt(a, now)?.toISOString(), "2026-11-25T00:00:00.000Z");
});

test("usageResetAt: no reading, or only resets already past: none, no time guessed", () => {
  assert.equal(usageResetAt({ usage: undefined } as never, now), undefined);
  assert.equal(usageResetAt(usage(win(100)), now), undefined);
  assert.equal(usageResetAt(usage(win(100, "2026-11-22T09:00:00Z")), now), undefined);
});

test("pauseOf and autoResumedAt read the status", () => {
  assert.equal(pauseOf({ pause: undefined } as never), undefined);
  assert.deepEqual(pauseOf({ pause: { since: ts("2026-11-22T09:00:00Z"), resumeAt: undefined } } as never), {
    since: new Date("2026-11-22T09:00:00Z"),
    until: undefined,
  });
  assert.equal(pauseOf({ pause: { since: ts("2026-11-22T09:00:00Z"), resumeAt: ts("2026-11-26T07:00:00Z") } } as never)?.until?.toISOString(), "2026-11-26T07:00:00.000Z");
  assert.equal(autoResumedAt({ autoResumedAt: ts("2026-11-22T09:30:00Z") } as never)?.toISOString(), "2026-11-22T09:30:00.000Z");
  assert.equal(autoResumedAt({ autoResumedAt: undefined } as never), undefined);
});
