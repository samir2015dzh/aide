import { Code, ConnectError } from "@connectrpc/connect";
import type { Machine } from "@aide/proto/local";

// A machine whose Aide is older than this app (docs/version-skew.md).

/**
 * The error says the machine's Aide is older than this app (§6.3): her lacks the method, or
 * aided or its services don't know the call.
 */
export function needsUpdate(e: unknown): boolean {
  if (e instanceof ConnectError) return e.code === Code.Unimplemented;
  return (e as { kind?: unknown } | null)?.kind === "method_not_found";
}

/**
 * Whether the owner's devices can call `method` of her's protocol on this machine (§6.1): undefined
 * while unknown (her not running there, or a machine too old to list its methods), which callers
 * take as yes.
 */
export function herAnswers(m: Machine, method: string): boolean | undefined {
  const methods = m.her?.methods ?? [];
  return methods.length ? methods.includes(method) : undefined;
}

/**
 * Whether her on this machine has `method`, told apart from her not running there (2026-11-20: a
 * machine whose her never ran lists no methods, which is not a machine too old):
 * - "off": Her is not turned on there;
 * - "notRunning": turned on, but her is not running (starting, or it failed: her.error says why);
 * - "older": her runs and lacks it, as its list says, or as its answer to a call showed (`lacked`:
 *   the call came back needing an update, needsUpdate);
 * - "yes": her runs and lists it; "unknown": her runs without a list (one from before the list) and
 *   no call has said, which callers take as yes.
 * Only "older" says the machine's Aide is older than this app.
 */
export type HerHas = "yes" | "unknown" | "off" | "notRunning" | "older";

export function herHas(m: Pick<Machine, "her">, method: string, lacked = false): HerHas {
  const her = m.her;
  if (!her?.enabled) return "off";
  if (!her.running) return "notRunning";
  const methods = her.methods ?? [];
  if (methods.length) return methods.includes(method) && !lacked ? "yes" : "older";
  return lacked ? "older" : "unknown";
}
