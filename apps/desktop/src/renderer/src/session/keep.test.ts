import assert from "node:assert/strict";
import { test } from "node:test";
import { isMainWindowHash, keptRecord, readKeptRecord, resumeAfter } from "./keep.ts";

test("a conversation kept only in part is read again from its start", () => {
  const events = ["e1", "e2", "e3"];
  const part = readKeptRecord(keptRecord(true, events, 2));
  assert.deepEqual(part, { whole: false, events: ["e2", "e3"] });
  assert.equal(resumeAfter([2n, 3n], part.whole), 0n);

  const all = readKeptRecord(keptRecord(true, events, 5));
  assert.deepEqual(all, { whole: true, events });
  assert.equal(resumeAfter([1n, 2n, 3n], all.whole), 3n);

  // A stream that never got to the start keeps it not whole.
  assert.equal(readKeptRecord(keptRecord(false, events, 5)).whole, false);
});

test("the first format (a bare list) is not taken for the whole conversation", () => {
  assert.deepEqual(readKeptRecord(JSON.stringify(["e1", "e2"])), { whole: false, events: ["e1", "e2"] });
  assert.deepEqual(readKeptRecord("not json"), { whole: false, events: [] });
  assert.deepEqual(readKeptRecord(null), { whole: false, events: [] });
});

test("only the main window runs the outbox", () => {
  assert.equal(isMainWindowHash(""), true);
  for (const h of ["#tray", "#settings", "#settings/account", "#updates", "#about", "#whats-new"]) {
    assert.equal(isMainWindowHash(h), false, h);
  }
});
