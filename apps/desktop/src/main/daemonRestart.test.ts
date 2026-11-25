import assert from "node:assert/strict";
import test from "node:test";
import { runDaemonRestart } from "./daemonRestartLogic.ts";

const okStatus = { initialized: true, relayConnected: true };
const tr = (k: string) => k;

test("runDaemonRestart: restarts and waits for probe confirmation before returning ok", async () => {
  let restarted = false;
  let probes = 0;
  let recovered: unknown = null;
  const res = await runDaemonRestart({
    installed: true,
    needsApproval: () => false,
    restart: async () => {
      restarted = true;
    },
    probe: async () => {
      probes++;
      return probes >= 3 ? okStatus : null;
    },
    onRecovered: (s) => {
      recovered = s;
    },
    timeoutMs: 1000,
    intervalMs: 10,
    t: tr,
  });
  assert.equal(restarted, true);
  assert.equal(probes, 3);
  assert.deepEqual(res, { ok: true });
  assert.equal(recovered, okStatus);
});

test("runDaemonRestart: never claims success when probe keeps failing after restart exits cleanly", async () => {
  let restarted = false;
  const res = await runDaemonRestart({
    installed: true,
    needsApproval: () => false,
    restart: async () => {
      restarted = true;
    },
    probe: async () => null,
    timeoutMs: 40,
    intervalMs: 10,
    t: tr,
  });
  assert.equal(restarted, true);
  assert.deepEqual(res, { ok: false, reason: "daemon.restartTimedOut", canOpenLogs: true });
});

test("runDaemonRestart: surfaces the real restart failure reason in place without probing", async () => {
  let probed = false;
  const res = await runDaemonRestart({
    installed: true,
    needsApproval: () => false,
    restart: async () => {
      throw new Error("Access is denied");
    },
    probe: async () => {
      probed = true;
      return okStatus;
    },
    t: tr,
  });
  assert.equal(probed, false);
  assert.deepEqual(res, { ok: false, reason: "Access is denied", canOpenLogs: true });
});

test("runDaemonRestart: reports approval requirement directly on macOS when Login Item is disabled", async () => {
  let restarted = false;
  const res = await runDaemonRestart({
    installed: true,
    needsApproval: () => true,
    restart: async () => {
      restarted = true;
    },
    probe: async () => null,
    t: tr,
  });
  assert.equal(restarted, false);
  assert.deepEqual(res, { ok: false, needsApproval: true, reason: "daemon.needsApproval", canOpenLogs: true });
});

test("runDaemonRestart: reports explicit error in dev/external mode when daemon is still unreachable", async () => {
  const res = await runDaemonRestart({
    installed: false,
    needsApproval: () => false,
    restart: async () => {},
    probe: async () => null,
    t: tr,
  });
  assert.deepEqual(res, { ok: false, reason: "daemon.restartDev", canOpenLogs: true });
});
