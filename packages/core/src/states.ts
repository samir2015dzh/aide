import { timestampDate, type Timestamp } from "@bufbuild/protobuf/wkt";
import type { GetStatusResponse, Machine } from "@hermesh/proto/local";
import { FailureKind, SessionLink, SessionState, SessionStatus, type Session } from "@hermesh/proto/session";
import { t } from "./i18n";

// What every screen says of a session (docs/session-states.md): what it is
// doing (SessionState) and how fresh that is (SessionLink), never folded
// into one. The same table as internal/sessionstate (Go) and the Android
// app's States.kt; states.test.ts checks it against the cases they share
// (internal/sessionstate/testdata/cases.json).

/** The limits states are worked out with (GetStatusResponse.state_limits). */
export type StateLimits = { lostAfterMs: number; stallAfterMs: number };

export const defaultStateLimits: StateLimits = { lostAfterMs: 30_000, stallAfterMs: 5 * 60_000 };

/** The limits the core says, else the defaults. */
export function stateLimitsOf(status: GetStatusResponse | null | undefined): StateLimits {
  const l = status?.stateLimits;
  if (!l || l.lostAfterSeconds === 0 || l.stallAfterSeconds === 0) return defaultStateLimits;
  return { lostAfterMs: l.lostAfterSeconds * 1000, stallAfterMs: l.stallAfterSeconds * 1000 };
}

/** What is known of a session's machine. */
export type MachineLiveness = {
  /** Listed: a session of a machine that is not is lost. */
  known: boolean;
  local: boolean;
  unreachable: string;
  syncing: boolean;
  /** Set once it is not heard from. */
  heardAt?: Date;
  unreachableSince?: Date;
};

export function livenessOf(m: Machine | undefined): MachineLiveness {
  if (!m) return { known: false, local: false, unreachable: "", syncing: false };
  return { known: true, local: m.local, unreachable: m.unreachable, syncing: m.syncing, heardAt: dateOrUndefined(m.heardAt), unreachableSince: dateOrUndefined(m.unreachableSince) };
}

function dateOrUndefined(ts: Timestamp | undefined): Date | undefined {
  return ts && (ts.seconds > 0n || ts.nanos > 0) ? timestampDate(ts) : undefined;
}

export type StateView = {
  state: SessionState;
  link: SessionLink;
  lastEventAt?: Date;
  /** How long since its last event, while it runs (ms). */
  quietMs: number;
  heardAt?: Date;
  /** How long its machine has not been heard from, while it is not (ms). */
  silentMs: number;
};

/** What a session is doing, from its status and progress alone. */
export function stateOf(s: Session): SessionState {
  switch (s.status) {
    case SessionStatus.QUEUED:
    case SessionStatus.PREPARING:
      return SessionState.QUEUED;
    case SessionStatus.RUNNING:
      return SessionState.RUNNING;
    case SessionStatus.WAITING_INPUT:
      return SessionState.WAITING_INPUT;
    case SessionStatus.ARCHIVED:
      return SessionState.ARCHIVED;
    case SessionStatus.CRASHED:
      return SessionState.FAILED;
    case SessionStatus.IDLE: {
      const f = s.progress?.lastFailure;
      if (!f) return SessionState.DONE;
      return f.kind === FailureKind.INTERRUPTED ? SessionState.INTERRUPTED : SessionState.FAILED;
    }
    default:
      return SessionState.UNSPECIFIED;
  }
}

/** When a session last recorded anything: its last event, else (an older machine) when it last changed. */
export function lastEventAt(s: Session): Date | undefined {
  return dateOrUndefined(s.progress?.lastEventAt) ?? dateOrUndefined(s.updatedAt);
}

/** What is shown of a session at now (internal/sessionstate Of). */
export function sessionStateOf(s: Session, m: MachineLiveness, limits: StateLimits, now: Date): StateView {
  const v: StateView = { state: stateOf(s), link: SessionLink.LIVE, lastEventAt: lastEventAt(s), quietMs: 0, silentMs: 0 };
  if (!m.local) v.heardAt = m.heardAt;
  if (!m.known) {
    v.link = SessionLink.LOST;
    if (m.heardAt) v.silentMs = Math.max(0, now.getTime() - m.heardAt.getTime());
  } else if (!m.local) {
    let since = m.heardAt;
    if (m.unreachable !== "") {
      v.link = SessionLink.CHECKING;
      since ??= m.unreachableSince;
      if (!since) v.link = SessionLink.LOST;
    } else if (m.syncing) {
      v.link = SessionLink.SYNCING;
    } else if (since) {
      v.link = SessionLink.CHECKING; // readable a moment ago, but no heartbeat
    }
    if (since) {
      v.silentMs = Math.max(0, now.getTime() - since.getTime());
      if (v.silentMs >= limits.lostAfterMs) v.link = SessionLink.LOST;
    }
  }
  if (v.state === SessionState.RUNNING && v.lastEventAt) {
    v.quietMs = Math.max(0, now.getTime() - v.lastEventAt.getTime());
    if (v.link === SessionLink.LIVE && v.quietMs >= limits.stallAfterMs) v.link = SessionLink.STALLED;
  }
  return v;
}

/** What is shown is current: the session may be acted on. */
export function isLiveLink(l: SessionLink): boolean {
  return l === SessionLink.LIVE || l === SessionLink.STALLED;
}

export function stateTitle(st: SessionState): string {
  switch (st) {
    case SessionState.QUEUED:
      return t("state.queued");
    case SessionState.RUNNING:
      return t("state.running");
    case SessionState.WAITING_INPUT:
      return t("state.waitingInput");
    case SessionState.WAITING_APPROVAL:
      return t("state.waitingApproval");
    case SessionState.DONE_UNSEEN:
      return t("state.doneUnseen");
    case SessionState.DONE:
      return t("state.done");
    case SessionState.FAILED:
      return t("state.failed");
    case SessionState.INTERRUPTED:
      return t("state.interrupted");
    case SessionState.ARCHIVED:
      return t("state.archived");
    default:
      return t("state.unknown");
  }
}

/** A duration as lists say it: "40 s", "12 min", "3 h", "2 d". */
export function durationText(ms: number): string {
  const s = Math.floor(ms / 1000);
  if (s < 60) return t("ago.seconds", { count: s });
  if (s < 3600) return t("ago.minutes", { count: Math.floor(s / 60) });
  if (s < 48 * 3600) return t("ago.hours", { count: Math.floor(s / 3600) });
  return t("ago.days", { count: Math.floor(s / 86400) });
}

/**
 * The one line every screen shows: "Running", "Running · quiet 12 min",
 * "Lost · last known Running · 7 min ago" (internal/sessionstate Text).
 */
export function stateLine(v: StateView): string {
  const st = stateTitle(v.state);
  switch (v.link) {
    case SessionLink.STALLED:
      return t("link.stalled", { state: st, ago: durationText(v.quietMs) });
    case SessionLink.CHECKING:
      return t("link.checking", { state: st });
    case SessionLink.LOST:
      return v.silentMs > 0 ? t("link.lostFor", { state: st, ago: durationText(v.silentMs) }) : t("link.lost", { state: st });
    case SessionLink.SYNCING:
      return t("link.syncing", { state: st });
    default:
      return st;
  }
}

/** The times a session's machine was lost while the session was there, the one under way last (to undefined). */
export function lostDuring(m: Machine | undefined, s: Session): { from: Date; to?: Date }[] {
  const created = dateOrUndefined(s.createdAt)?.getTime() ?? 0;
  const out: { from: Date; to?: Date }[] = [];
  for (const x of m?.lostSpans ?? []) {
    const from = dateOrUndefined(x.from);
    const to = dateOrUndefined(x.to);
    if (from && (!to || to.getTime() >= created)) out.push(to ? { from, to } : { from });
  }
  return out;
}
