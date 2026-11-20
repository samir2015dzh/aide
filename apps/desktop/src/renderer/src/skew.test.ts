import assert from "node:assert/strict";
import { test } from "node:test";
import { Code, ConnectError } from "@connectrpc/connect";
import type { Machine } from "@aide/proto/local";
import { herAnswers, herDown, herNeedsUpdate, needsUpdate } from "./skew.ts";

// A machine older than this app (docs/version-skew.md §6.1, §6.3).

test("unimplemented and an unknown method of her's say the machine needs an update; nothing else does", () => {
  assert.equal(needsUpdate(new ConnectError("her's \"bots/memory\" is not open to the owner's devices", Code.Unimplemented)), true);
  assert.equal(needsUpdate({ code: -32601, kind: "method_not_found", message: "unknown method 'bots/memory'" }), true);
  assert.equal(needsUpdate(new ConnectError("no", Code.PermissionDenied)), false);
  assert.equal(needsUpdate({ kind: "invalid_params" }), false);
  assert.equal(needsUpdate(new Error("x")), false);
  assert.equal(needsUpdate(null), false);
});

test("what a machine lists is what it answers; nothing listed is unknown", () => {
  const m = (her?: { methods: string[] }) => ({ machineId: "m", her }) as unknown as Machine;
  assert.equal(herAnswers(m(), "bots/memory"), undefined);
  assert.equal(herAnswers(m({ methods: [] }), "bots/memory"), undefined);
  const listed = m({ methods: ["bots/subscribe", "bots/memory"] });
  assert.equal(herAnswers(listed, "bots/memory"), true);
  assert.equal(herAnswers(listed, "bots/routine"), false);
});

test("a failed call of her's says her doesn't run, not that Aide is older, while she doesn't run there", () => {
  const unimplemented = new ConnectError("her's x is not open", Code.Unimplemented);
  const machine = (her: object | undefined) => ({ machineId: "m", machineName: "alexwork", status: her === undefined ? {} : { her } }) as unknown as Machine;
  assert.equal(herDown(machine({ enabled: false })), "off");
  assert.equal(herDown(machine({ enabled: true, running: false })), "starting");
  assert.equal(herDown(machine({ enabled: true, running: false, error: "her could not start: no such file" })), "her could not start: no such file");
  assert.equal(herDown(machine({ enabled: true, running: true, methods: ["bots/memory"] })), null);
  // From before Her (no Her in its status): older, as before.
  assert.equal(herDown(machine(undefined)), null);
  assert.equal(herNeedsUpdate(machine({ enabled: true, running: false }), unimplemented), false);
  assert.equal(herNeedsUpdate(machine({ enabled: true, running: true }), unimplemented), true);
  assert.equal(herNeedsUpdate(machine(undefined), unimplemented), true);
  assert.equal(herNeedsUpdate(machine({ enabled: true, running: true }), new Error("other")), false);
});
