import assert from "node:assert/strict";
import { test } from "node:test";
import { Code, ConnectError } from "@connectrpc/connect";
import type { Machine } from "@aide/proto/local";
import { herAnswers, herHas, needsUpdate } from "./skew.ts";

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

test("only a running her that lacks a method says the machine is older; one not running, or off, says so instead", () => {
  const m = (her: Partial<NonNullable<Machine["her"]>> | undefined) => ({ her: her as Machine["her"] });
  // Her never ran with this build: no methods, and that is not an older machine.
  assert.equal(herHas(m(undefined), "bots/memory"), "off");
  assert.equal(herHas(m({ enabled: false, methods: [] }), "bots/memory"), "off");
  assert.equal(herHas(m({ enabled: true, running: false, methods: [] }), "bots/memory"), "notRunning");
  assert.equal(herHas(m({ enabled: true, running: false, methods: [], error: "exit status 1" }), "bots/memory", true), "notRunning");
  // Running: its list, or its answer, says.
  assert.equal(herHas(m({ enabled: true, running: true, methods: ["bots/memory"] }), "bots/memory"), "yes");
  assert.equal(herHas(m({ enabled: true, running: true, methods: ["bots/list"] }), "bots/memory"), "older");
  assert.equal(herHas(m({ enabled: true, running: true, methods: [] }), "bots/memory"), "unknown");
  assert.equal(herHas(m({ enabled: true, running: true, methods: [] }), "bots/memory", true), "older");
});
