import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-node";
import { LocalService, type GetStatusResponse } from "@aide/proto/local";
import fs from "node:fs";
import path from "node:path";
import type { DaemonRestartResult } from "../shared/bridge";
import { i18n } from "../shared/i18n";
import { runDaemonRestart } from "./daemonRestartLogic";
import { noteDaemonRecovered } from "./notify";
import { dataDir, socketPath } from "./paths";
import { platform } from "./platform";

export { errorText, runDaemonRestart, type DaemonRestartDeps } from "./daemonRestartLogic";

/**
 * 工单 #120: In-place background service restart with mandatory post-restart probe.
 * Never reports success merely because a restart command was launched: it must
 * observe aided answering LocalService.GetStatus before returning ok: true.
 */

let inflight: Promise<DaemonRestartResult> | null = null;
let testRestarter: (() => Promise<void>) | null = null;

/** For end-to-end tests: replace the restart trigger (and optionally force a failure or restart the test aided). */
export function setDaemonRestarterForTest(fn: (() => Promise<void>) | null) {
  testRestarter = fn;
}

export async function probeDaemonStatus(timeoutMs = 1500): Promise<GetStatusResponse | null> {
  const ac = new AbortController();
  const timer = setTimeout(() => ac.abort(), timeoutMs);
  try {
    const local = createClient(
      LocalService,
      createConnectTransport({
        baseUrl: "http://aided",
        httpVersion: "1.1",
        useBinaryFormat: true,
        nodeOptions: { socketPath: socketPath() },
      }),
    );
    return await local.getStatus({}, { signal: ac.signal });
  } catch {
    return null;
  } finally {
    clearTimeout(timer);
  }
}

export function restartDaemonWithProbe(): Promise<DaemonRestartResult> {
  if (inflight) return inflight;
  const run = runDaemonRestart<GetStatusResponse>({
    installed: platform.installed || testRestarter !== null,
    needsApproval: () => platform.daemonNeedsApproval(),
    restart: () => (testRestarter ? testRestarter() : platform.restartDaemonNow("owner requested")),
    probe: () => probeDaemonStatus(),
    onRecovered: (status) => noteDaemonRecovered(status),
    t: (key) => i18n.t(key, { ns: "common" }),
  }).finally(() => {
    inflight = null;
  });
  inflight = run;
  return run;
}

/** Opens aided's logs directory (falling back to the Aide data directory if logs/ does not exist yet). */
export function openDaemonLogs() {
  const logs = path.join(dataDir(), "logs");
  const target = fs.statSync(logs, { throwIfNoEntry: false })?.isDirectory() ? logs : dataDir();
  if (fs.statSync(target, { throwIfNoEntry: false })?.isDirectory()) {
    platform.openFolder(target, "files");
  }
}
