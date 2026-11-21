import type { AgentStatus } from "@aide/proto/machine";

/** A protobuf timestamp as a Date, or undefined when unset. */
function dateOf(ts: { seconds: bigint; nanos: number } | undefined): Date | undefined {
  return ts && ts.seconds > 0n ? new Date(Number(ts.seconds) * 1000 + Math.floor(ts.nanos / 1e6)) : undefined;
}

/**
 * When the agent's plan usage resets, to pause it until then (SetAgentPaused resume_at), as far as the machine
 * read it (Claude Code only): the latest reset of the windows used up, since it runs again only once all of them
 * have reset; with none used up, the reset of the fullest. Undefined without a reading of a reset still to come:
 * no time is guessed, and only the owner resumes it then.
 */
export function usageResetAt(a: Pick<AgentStatus, "usage">, now: Date): Date | undefined {
  const windows = (a.usage?.windows ?? [])
    .map((w) => ({ full: w.utilization >= 100, utilization: w.utilization, at: dateOf(w.resetsAt) }))
    .filter((w): w is { full: boolean; utilization: number; at: Date } => !!w.at && w.at > now);
  const full = windows.filter((w) => w.full);
  if (full.length > 0) return new Date(Math.max(...full.map((w) => w.at.getTime())));
  if (windows.length === 0) return undefined;
  return windows.reduce((a, b) => (b.utilization > a.utilization ? b : a)).at;
}

/** The agent's pause, as its status says it: undefined when not paused; `until` unset when only the owner resumes it. */
export function pauseOf(a: Pick<AgentStatus, "pause">): { since?: Date; until?: Date } | undefined {
  return a.pause ? { since: dateOf(a.pause.since), until: dateOf(a.pause.resumeAt) } : undefined;
}

/** When the agent last resumed by itself, once its pause came to its end, to show. */
export function autoResumedAt(a: Pick<AgentStatus, "autoResumedAt">): Date | undefined {
  return dateOf(a.autoResumedAt);
}
