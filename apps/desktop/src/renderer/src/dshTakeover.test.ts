import assert from "node:assert/strict";
import { test } from "node:test";
import { Code, ConnectError } from "@connectrpc/connect";
import type { DSHBridge } from "@aide/proto/machine";
import { bridgeView, newRequestId, retryKeepsId } from "./dshTakeover.ts";

test("a message's id is aide- and 26 lowercase letters and digits, as the machine takes it", () => {
  const id = newRequestId();
  assert.match(id, /^aide-[0-9a-z]{26}$/);
  assert.notEqual(newRequestId(), id);
  assert.equal(newRequestId((n) => new Uint8Array(n).fill(35)), "aide-" + "z".repeat(26));
});

test("only a timeout keeps the id for the retry", () => {
  assert.equal(retryKeepsId(new ConnectError("slow", Code.DeadlineExceeded)), true);
  assert.equal(retryKeepsId(new ConnectError("off", Code.FailedPrecondition)), false);
  assert.equal(retryKeepsId(new Error("x")), false);
});

test("the bridge as the page tells it", () => {
  // DSHBridgeState: 1 not installed, 2 not running, 3 running, 4 incompatible.
  const b = (state: number, version = "", detail = "") => ({ state, version, detail }) as Pick<DSHBridge, "state" | "version" | "detail">;
  assert.deepEqual(bridgeView(b(3, "0.1.0")), { ok: true, key: "running", detail: "0.1.0" });
  assert.deepEqual(bridgeView(b(1, "", "install it")), { ok: false, key: "notInstalled", detail: "install it" });
  assert.equal(bridgeView(b(2)).key, "notRunning");
  assert.equal(bridgeView(b(4)).key, "incompatible");
  assert.deepEqual(bridgeView(undefined), { ok: false, key: "unknown", detail: "" });
});
