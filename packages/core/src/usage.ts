import type { MessageInitShape } from "@bufbuild/protobuf";
import { WorkBucket, WorkDimension, type QueryWorkRequestSchema, type QueryWorkResponse } from "@aide/proto/local";

// The usage heatmap: a year of days by week (columns) and weekday (rows,
// Sunday first), each day's tokens and a level of 0 (none) to 4, and the
// figures above and below it, and the year's tokens in parts: by coding
// agent or by machine (工单 #142).

export type UsageCell = { date: Date; key: string; tokens: number; sessions: number; level: number };

export type UsageYear = {
  /** weeks × 7 cells, column by column; null past today. */
  cells: (UsageCell | null)[];
  weeks: number;
  /** The first day shown (a Sunday), YYYY-MM-DD: what QueryWork is asked from. */
  since: string;
  total: number;
  today: number;
  /** Days with any tokens. */
  active: number;
  /** Days in a row with tokens, ending today (or yesterday, before today's first turn). */
  streak: number;
  /** The last 30 days against the 30 before, in percent; null without the 30 before. */
  delta: number | null;
};

export const dayKey = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;

/** The first day a year of weeks ending in today's week starts on. */
export function usageSince(today: Date, weeks = 53): string {
  return dayKey(new Date(today.getFullYear(), today.getMonth(), today.getDate() - today.getDay() - (weeks - 1) * 7));
}

/** Days from the first day of a year of weeks to today, today included. */
const daysTo = (today: Date, since: string) => {
  const [y, m, d] = since.split("-").map(Number);
  return Math.round((new Date(today.getFullYear(), today.getMonth(), today.getDate()).getTime() - new Date(y, m - 1, d).getTime()) / 86_400_000) + 1;
};

/**
 * The QueryWork question for a year of weeks: from usageSince to today, by day, coding agent and machine. Both
 * groupings are in the one answer (工单 #142): switching between them asks nothing again, and a session has one
 * agent and one machine, so either adds up to the same days.
 */
export function usageRequest(today: Date, weeks = 53, timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone): MessageInitShape<typeof QueryWorkRequestSchema> {
  const since = usageSince(today, weeks);
  return { timeZone, firstDay: since, days: daysTo(today, since), bucket: WorkBucket.DAY, groupBy: [WorkDimension.AGENT, WorkDimension.MACHINE] };
}

/** A year of usage from QueryWork's answer to usageRequest: each day's tokens and sessions at work. */
export function usageYear(res: Pick<QueryWorkResponse, "turns" | "sessions"> | null | undefined, today: Date, weeks = 53): UsageYear {
  const since = usageSince(today, weeks);
  const [y, m, dd] = since.split("-").map(Number);
  const keyOf = (bucket: number) => dayKey(new Date(y, m - 1, dd + bucket));
  const tokens = new Map<string, number>();
  const sessions = new Map<string, number>();
  for (const t of res?.turns ?? []) {
    const key = keyOf(t.bucket);
    tokens.set(key, (tokens.get(key) ?? 0) + Number(t.inputTokens) + Number(t.outputTokens));
  }
  // Sessions partition by agent and machine, so a day's add up exactly.
  for (const x of res?.sessions ?? []) sessions.set(keyOf(x.bucket), (sessions.get(keyOf(x.bucket)) ?? 0) + x.sessions);
  const todayKey = dayKey(today);
  const cells: (UsageCell | null)[] = [];
  let past = false;
  for (let i = 0; i < weeks * 7; i++) {
    const date = new Date(y, m - 1, dd + i);
    const key = dayKey(date);
    cells.push(past ? null : { date, key, tokens: tokens.get(key) ?? 0, sessions: sessions.get(key) ?? 0, level: 0 });
    if (key === todayKey) past = true;
  }
  const shown = cells.filter((c): c is UsageCell => c !== null);

  // Levels by the quartiles of the days with tokens, as GitHub's.
  const used = shown.map((c) => c.tokens).filter((n) => n > 0).sort((a, b) => a - b);
  const q = (p: number) => used[Math.min(used.length - 1, Math.floor(p * used.length))];
  const cuts = used.length ? [q(0.25), q(0.5), q(0.75)] : [];
  for (const c of shown) c.level = c.tokens === 0 ? 0 : Math.min(4, 1 + cuts.filter((x) => c.tokens >= x).length);

  const sum = (cs: UsageCell[]) => cs.reduce((n, c) => n + c.tokens, 0);
  const last30 = sum(shown.slice(-30));
  const prev30 = sum(shown.slice(-60, -30));
  let streak = 0;
  for (let i = shown.length - (shown.at(-1)?.tokens ? 1 : 2); i >= 0 && shown[i].tokens > 0; i--) streak++;
  const total = sum(shown);
  return {
    cells,
    weeks,
    since,
    total,
    today: shown.at(-1)?.tokens ?? 0,
    active: used.length,
    streak,
    delta: prev30 > 0 ? Math.round(((last30 - prev30) / prev30) * 100) : null,
  };
}

/**
 * What the year's tokens are split by (工单 #142): the coding agent that did the work, or the machine it ran on.
 * A personal agent follows the account, not a machine, and is neither: what it asked for is counted under the
 * coding agent that did it and the machine that ran it, as a local coding agent's own work is.
 */
export type UsageGroup = "agent" | "machine";
/** In the order the control shows them; the first is the one the view opens on, as upstream's. */
export const usageGroups: readonly UsageGroup[] = ["agent", "machine"];

/** A part of the year's tokens: an agent or machine (other: the least busy added up), its tokens, its share and its tokens by day (YYYY-MM-DD). */
export type UsagePart = { id: string; other: boolean; tokens: number; share: number; days: Map<string, number> };

/** At most this many parts are named; past it the least busy are one, Other. */
export const usagePartsShown = 5;

/**
 * The year's tokens in parts from the same answer as usageYear, the busiest first: its parts add up to its total,
 * and a day's to the day's, whichever the grouping (past `shown` parts, Other holds the rest).
 */
export function usageParts(res: Pick<QueryWorkResponse, "turns"> | null | undefined, today: Date, group: UsageGroup, weeks = 53, shown = usagePartsShown): UsagePart[] {
  const since = usageSince(today, weeks);
  const [y, m, dd] = since.split("-").map(Number);
  const last = daysTo(today, since) - 1;
  const all = new Map<string, UsagePart>();
  for (const t of res?.turns ?? []) {
    if (t.bucket > last) continue; // not a day the heatmap shows, so not in its total
    const n = Number(t.inputTokens) + Number(t.outputTokens);
    if (!n) continue;
    const id = (group === "agent" ? t.group?.agent : t.group?.machineId) ?? "";
    let p = all.get(id);
    if (!p) all.set(id, (p = { id, other: false, tokens: 0, share: 0, days: new Map() }));
    const key = dayKey(new Date(y, m - 1, dd + t.bucket));
    p.tokens += n;
    p.days.set(key, (p.days.get(key) ?? 0) + n);
  }
  const parts = [...all.values()].sort((a, b) => b.tokens - a.tokens || a.id.localeCompare(b.id));
  const total = parts.reduce((n, p) => n + p.tokens, 0);
  const named = parts.length > shown ? parts.slice(0, shown - 1) : parts;
  if (named.length < parts.length) {
    const other: UsagePart = { id: "", other: true, tokens: 0, share: 0, days: new Map() };
    for (const p of parts.slice(named.length)) {
      other.tokens += p.tokens;
      for (const [k, n] of p.days) other.days.set(k, (other.days.get(k) ?? 0) + n);
    }
    named.push(other);
  }
  for (const p of named) p.share = total ? p.tokens / total : 0;
  return named;
}
