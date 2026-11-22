import type { StateView } from "@aide/core";
import type { Client } from "@aide/proto/local";
import type { Session } from "@aide/proto/session";

// The tasks page (work order #27): the work the owner's agents (Muse first)
// handed out through Aide, as it goes. What each is doing comes from the
// session itself (its tool now, its last words, its title), never from what
// an agent says of it; its state is the one every screen shows
// (docs/session-states.md, worked out by the caller's viewOf). The session
// list has every session; this has only the work given out, the finished
// ones for a day.
//
// Plain numbers for the enums: this file is tested under node --test, which
// takes no TypeScript enums (session.proto's values).
const STATUS_ARCHIVED = 5;
const STATE_QUEUED = 1,
  STATE_RUNNING = 2,
  STATE_WAITING_INPUT = 3,
  STATE_WAITING_APPROVAL = 4,
  STATE_DONE_UNSEEN = 5,
  STATE_FAILED = 7;
const LINK_STALLED = 2,
  LINK_CHECKING = 3,
  LINK_LOST = 4;

/** Finished work stays this long. */
export const keepDoneMs = 24 * 3600_000;

export type TaskRow = {
  session: Session;
  /** Who gave it out: the agent's name, as the app lists it. */
  dispatcher: string;
  dispatcherId: string;
  view: StateView;
  /** 0 needs a look (asks, failed, stalled, lost or not heard from), 1 at work, 2 done not opened, 3 done. */
  rank: 0 | 1 | 2 | 3;
  /** What it is doing, from itself. */
  doing: { kind: "tool" | "said" | "title"; text: string };
  startedAt?: Date;
  lastActivity?: Date;
};

function dateOf(ts: { seconds: bigint; nanos: number } | undefined): Date | undefined {
  return ts && (ts.seconds > 0n || ts.nanos > 0) ? new Date(Number(ts.seconds) * 1000 + Math.floor(ts.nanos / 1e6)) : undefined;
}

export function rankOf(v: Pick<StateView, "state" | "link">): TaskRow["rank"] {
  const link = v.link as number;
  if (link === LINK_LOST || link === LINK_CHECKING || link === LINK_STALLED) return 0;
  switch (v.state as number) {
    case STATE_WAITING_INPUT:
    case STATE_WAITING_APPROVAL:
    case STATE_FAILED:
      return 0;
    case STATE_RUNNING:
    case STATE_QUEUED:
      return 1;
    case STATE_DONE_UNSEEN:
      return 2;
  }
  return 3;
}

export function doingOf(s: Session, v: Pick<StateView, "state">): TaskRow["doing"] {
  const p = s.progress;
  if ((v.state as number) === STATE_RUNNING && p?.currentTool) return { kind: "tool", text: p.currentTool };
  const said = p?.lastMessageExcerpt
    ?.split("\n")
    .find((l) => l.trim() !== "")
    ?.trim();
  if (said) return { kind: "said", text: said };
  return { kind: "title", text: s.title || s.projectId };
}

/** The work handed out, needing a look first, then at work, then done; the latest first within each. */
export function taskRows(sessions: Session[], clients: Client[], viewOf: (s: Session) => StateView, now: Date): TaskRow[] {
  const rows: TaskRow[] = [];
  for (const s of sessions) {
    if (s.createdBy === "owner" || s.createdBy === "" || (s.status as number) === STATUS_ARCHIVED) continue;
    const view = viewOf(s);
    const rank = rankOf(view);
    const lastActivity = view.lastEventAt ?? dateOf(s.updatedAt);
    if (rank === 3 && lastActivity && now.getTime() - lastActivity.getTime() > keepDoneMs) continue;
    const c = clients.find((x) => x.clientId === s.createdBy);
    rows.push({ session: s, dispatcher: c?.label || s.createdBy, dispatcherId: s.createdBy, view, rank, doing: doingOf(s, view), startedAt: dateOf(s.createdAt), lastActivity });
  }
  return rows.sort((a, b) => a.rank - b.rank || (b.lastActivity?.getTime() ?? 0) - (a.lastActivity?.getTime() ?? 0));
}

/** The agents that gave work out, with how much, the most first. */
export function dispatchers(rows: TaskRow[]): { id: string; name: string; count: number }[] {
  const m = new Map<string, { id: string; name: string; count: number }>();
  for (const r of rows) {
    const d = m.get(r.dispatcherId) ?? { id: r.dispatcherId, name: r.dispatcher, count: 0 };
    d.count++;
    m.set(r.dispatcherId, d);
  }
  return [...m.values()].sort((a, b) => b.count - a.count || a.name.localeCompare(b.name));
}
