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
 * Why her can't answer on this machine now, when that, and not an older Aide, is why a call of hers
 * failed: Her is off there ("off"), her is not running yet ("starting"), or failed to start (her's
 * error). null: she runs, or the machine says nothing of Her (it is from before Her).
 */
export function herDown(m: Machine): string | null {
  const h = m.status?.her ?? m.her;
  if (!h) return null;
  if (!h.enabled) return "off";
  if (!h.running) return h.error || "starting";
  return null;
}

/** The call failed because the machine's Aide (or her) is older: not because her doesn't run there. */
export function herNeedsUpdate(m: Machine, e: unknown): boolean {
  return needsUpdate(e) && herDown(m) === null;
}
