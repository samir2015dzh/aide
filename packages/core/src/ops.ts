import { ExecOutcome, type ExecRecord } from "@hermesh/proto/machine";
import { dateOf } from "./sessions";

/** How a command of the ops log (docs/machine-ops.md) ended, as its row says it. */
export type OpsResult =
  | { kind: "ok" }
  | { kind: "exit"; code: number }
  | { kind: "timeout" }
  | { kind: "refused" }
  | { kind: "failed" };

export function opsResult(r: ExecRecord): OpsResult {
  switch (r.outcome) {
    case ExecOutcome.EXITED:
      return r.exitCode === 0 ? { kind: "ok" } : { kind: "exit", code: r.exitCode };
    case ExecOutcome.TIMED_OUT:
      return { kind: "timeout" };
    case ExecOutcome.REFUSED:
      return { kind: "refused" };
    default:
      return { kind: "failed" };
  }
}

/** How long it ran, in milliseconds (0 when the machine didn't say). */
export function opsMillis(r: ExecRecord): number {
  const from = dateOf(r.startedAt);
  const to = dateOf(r.endedAt);
  return from && to ? Math.max(0, to.getTime() - from.getTime()) : 0;
}
