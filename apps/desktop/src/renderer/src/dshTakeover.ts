// Taking over a DSH session from Aide (docs/dsh-takeover.md): the parts
// of the page that decide, without React.

import { Code, ConnectError } from "@connectrpc/connect";
import type { DSHBridge } from "@aide/proto/machine";

// DSHBridgeState's values (proto/aide/machine/v1/machine.proto), by number: the enum isn't
// imported, so that node --test loads this file as the other page logic.
const NOT_INSTALLED = 1;
const NOT_RUNNING = 2;
const RUNNING = 3;
const INCOMPATIBLE = 4;

/** A message's id for DSH ("aide-" + 26 lowercase letters and digits), which drops a second one with it. */
export function newRequestId(random: (n: number) => Uint8Array = (n) => crypto.getRandomValues(new Uint8Array(n))): string {
  const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz";
  return "aide-" + Array.from(random(26), (b) => alphabet[b % alphabet.length]).join("");
}

/**
 * After a send failed: keep the same id when DSH may yet have taken it
 * (it ran out of time), so sending again can't make two; a refusal sent
 * nothing, so the next try is a new message.
 */
export function retryKeepsId(e: unknown): boolean {
  return e instanceof ConnectError && e.code === Code.DeadlineExceeded;
}

export type BridgeView = { ok: boolean; key: "running" | "notInstalled" | "notRunning" | "incompatible" | "unknown"; detail: string };

/** What the page says of the bridge plugin; ok: messages can go. */
export function bridgeView(b: Pick<DSHBridge, "state" | "version" | "detail"> | undefined): BridgeView {
  switch (b?.state as number | undefined) {
    case RUNNING:
      return { ok: true, key: "running", detail: b!.version };
    case NOT_INSTALLED:
      return { ok: false, key: "notInstalled", detail: b!.detail };
    case NOT_RUNNING:
      return { ok: false, key: "notRunning", detail: b!.detail };
    case INCOMPATIBLE:
      return { ok: false, key: "incompatible", detail: b!.detail };
    default:
      return { ok: false, key: "unknown", detail: "" };
  }
}
