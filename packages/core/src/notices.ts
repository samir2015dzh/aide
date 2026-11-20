import { create } from "@bufbuild/protobuf";
import type { Machine } from "@aide/proto/local";
import { HerLatestKind, type HerLatest } from "@aide/proto/machine";
import { FailureSchema, SessionStatus, type Session } from "@aide/proto/session";
import { codingAgentTitle } from "./agents";
import { t } from "./i18n";
import { dateOf, displayTitle, isFailed, pendingQuestion } from "./sessions";
import { failureAdvice, failureTitle } from "./text";
import { isInterruption } from "./transcript";

// What a session's change reaches the owner as: a new session, a question,
// a failure, a finished turn. Kept apart from the notification center so it can be checked.

/** A session as it was at the last look. */
export interface NoticeSnapshot {
  status: SessionStatus;
  turns: number;
  question: string;
  failed: boolean;
}

export function noticeSnapshot(s: Session): NoticeSnapshot {
  return {
    status: s.status,
    turns: s.progress?.turnCount ?? 0,
    question: pendingQuestion(s)?.questionId ?? "",
    failed: isFailed(s),
  };
}

/** Each session's snapshot by its id; the first of a repeated id wins. */
export function noticeSnapshots(sessions: Session[]): Map<string, NoticeSnapshot> {
  const out = new Map<string, NoticeSnapshot>();
  for (const s of sessions) if (!out.has(s.sessionId)) out.set(s.sessionId, noticeSnapshot(s));
  return out;
}

export type NoticeKind = "started" | "question" | "failed" | "turnDone";

export interface NoticeItem {
  kind: NoticeKind;
  sessionId: string;
  title: string;
  body: string;
}

/**
 * The notices since the last look (previous: each session as it was).
 * A session seen for the first time tells about a question, and is new
 * when it was created at or after since (the first look: the sessions from
 * before, or of a machine coming back, are no news); one the owner started
 * is no news either (the owner just did).
 */
export function noticeDiff(
  previous: ReadonlyMap<string, NoticeSnapshot>,
  sessions: Session[],
  grant: (s: Session) => string,
  since?: Date,
): NoticeItem[] {
  const out: NoticeItem[] = [];
  for (const s of sessions) {
    const before = previous.get(s.sessionId);
    const created = dateOf(s.createdAt);
    if (!before && since && created && created >= since && s.createdBy !== "owner") {
      out.push({
        kind: "started",
        sessionId: s.sessionId,
        title: displayTitle(s),
        body: t("notice.startedBody", { grant: grant(s), project: s.projectId, agent: codingAgentTitle(s.agent) }),
      });
    }
    const q = pendingQuestion(s);
    if (q && q.questionId !== before?.question) {
      const text = q.items[0]?.text ?? "";
      out.push({
        kind: "question",
        sessionId: s.sessionId,
        // The owner's own session: its coding agent asks.
        title: t("notice.questionTitle", { grant: s.createdBy === "owner" ? codingAgentTitle(s.agent) : grant(s) }),
        body: t("notice.questionBody", { title: displayTitle(s), text }),
      });
    }
    if (!before) continue;
    const turns = s.progress?.turnCount ?? 0;
    if (isFailed(s) && !before.failed) {
      const f = s.progress?.lastFailure ?? create(FailureSchema);
      out.push({
        kind: "failed",
        sessionId: s.sessionId,
        title: t("notice.failedTitle", { title: displayTitle(s), failure: failureTitle(f.kind) }),
        body: t("notice.failedBody", { grant: grant(s), agent: codingAgentTitle(s.agent), project: s.projectId, advice: failureAdvice(f) }).trimEnd(),
      });
    } else if (
      s.status === SessionStatus.IDLE &&
      !isFailed(s) &&
      !isInterruption(s.progress?.lastFailure) &&
      turns > before.turns &&
      before.status !== SessionStatus.IDLE
    ) {
      const d = s.progress?.diffStat;
      const files = d?.filesChanged ?? 0;
      out.push({
        kind: "turnDone",
        sessionId: s.sessionId,
        title: t("notice.turnDoneTitle", { title: displayTitle(s), turn: turns }),
        body:
          files === 0
            ? t("notice.noChanges")
            : t("notice.changes", { count: files, insertions: d?.insertions ?? 0, deletions: d?.deletions ?? 0 }),
      });
    }
  }
  return out;
}

/** What a desktop tells a session's machine of a question it told the owner of (MarkSessionSeen). */
export interface QuestionAck {
  sessionId: string;
  questionId: string;
  present: boolean;
}

/**
 * What a desktop has to tell the machines of the questions it told the owner of (told: their question ids), the owner
 * at it now or not (present): each one its machine does not have yet (docs/session-states.md §7). Phones hold back a
 * question a desktop told of, for a while, and for good once the owner was there. A machine that keeps nothing seen
 * (an older one) is told nothing.
 */
export function questionAcks(sessions: Session[], told: ReadonlySet<string>, present: boolean): QuestionAck[] {
  const out: QuestionAck[] = [];
  for (const s of sessions) {
    const q = pendingQuestion(s);
    if (!q || !s.seen || !told.has(q.questionId)) continue;
    const has = s.seen.questionId === q.questionId;
    if (!has || (present && !s.seen.questionPresent)) out.push({ sessionId: s.sessionId, questionId: q.questionId, present });
  }
  return out;
}

/** The notices the owner can switch off in settings, by their settings key. */
export interface NoticePrefs {
  notifyStarted: boolean;
  notifyQuestion: boolean;
  notifyFailed: boolean;
  notifyTurnDone: boolean;
}

/**
 * The notices the owner wants. A preference not set yet counts as on for
 * new sessions, questions and failures, off for finished turns.
 */
export function filterNotices(items: NoticeItem[], prefs: Partial<NoticePrefs>): NoticeItem[] {
  return items.filter((item) => {
    switch (item.kind) {
      case "started":
        return prefs.notifyStarted ?? true;
      case "question":
        return prefs.notifyQuestion ?? true;
      case "failed":
        return prefs.notifyFailed ?? true;
      case "turnDone":
        return prefs.notifyTurnDone ?? false;
    }
  });
}

/** An environment's Her as it was at the last look: what it had unread, and its newest message. */
export interface HerSnapshot {
  unread: number;
  latest: string;
}

const latestKey = (l: HerLatest | undefined) => (l ? `${l.botId}\0${l.at?.seconds ?? 0}\0${l.at?.nanos ?? 0}\0${l.text}` : "");

/** Each environment's snapshot, by its machine id (docs/her-server.md §9.3). */
export function herSnapshots(machines: Machine[]): Map<string, HerSnapshot> {
  const out = new Map<string, HerSnapshot>();
  for (const m of machines) if (m.her?.enabled) out.set(m.machineId, { unread: m.her.unread, latest: latestKey(m.her.latest) });
  return out;
}

export interface HerNotice {
  machineId: string;
  botId: string;
  title: string;
  body: string;
}

/**
 * The notices since the last look: an environment whose unread count grew
 * with a newer message from an assistant. An environment seen for the first
 * time is no news (what it had before the look), nor is a muted assistant.
 * The title names the assistant, and its environment too when there is
 * more than one.
 */
export function herNotices(previous: ReadonlyMap<string, HerSnapshot>, machines: Machine[]): HerNotice[] {
  const environments = machines.filter((m) => m.her?.enabled);
  const out: HerNotice[] = [];
  for (const m of environments) {
    const before = previous.get(m.machineId);
    const her = m.her!;
    const latest = her.latest;
    if (!before || !latest || latest.muted || her.unread <= before.unread || latestKey(latest) === before.latest) continue;
    const name = latest.botName || t("notice.herName");
    out.push({
      machineId: m.machineId,
      botId: latest.botId,
      title: environments.length > 1 ? t("notice.herTitle", { name, environment: m.machineName }) : name,
      body:
        latest.kind === HerLatestKind.APPROVAL
          ? t("notice.herApproval", { text: latest.text })
          : latest.kind === HerLatestKind.QUESTION
            ? t("notice.herQuestion", { text: latest.text })
            : latest.text,
    });
  }
  return out;
}
