import type { Machine, Project } from "@aide/proto/local";
import type { AgentStatus } from "@aide/proto/machine";
import { SessionStatus, type Session } from "@aide/proto/session";
import { listedModel } from "./machines";
import type { Bot } from "./roster";
import { updatedDate } from "./sessions";

// A bot's conversations (工单 #67, the owner's six rules): behind a coding agent's bot is its coding agent's sessions
// on its machine, and each session the owner started with it is one conversation. The bot's page goes on in its
// current one (rule 1) — the one the owner chose on this device, else the one last active — until the owner starts a
// new one (rule 2), or changes its project, model, effort or mode, which starts one too, said so (rule 4). Every one
// is listed, to go back to and go on in (rule 5), unless it can't go on: then it says so, and offers a new one with its
// context. Archived ones are the machine's own archived sessions (rule 6): one source, so Aide and the agent's side
// never disagree; going on in one unarchives it first.

/** What a new conversation starts with: the composer's choices. "" is the agent's own default. */
export type ConvParams = { projectId: string; model: string; effort: string; mode: string };

/** Why a conversation can't go on now; undefined when it can. */
export type StuckWhy = "archived" | "projectGone" | "agentGone";

export type BotConversations = {
  /** The owner's own, newest activity first, not archived. */
  open: Session[];
  /** The owner's own, archived (the machine's archive). */
  archived: Session[];
  /** Work others (personal agents) handed it: listed apart, not the owner's conversations. */
  others: Session[];
};

const newest = (a: Session, b: Session) => updatedDate(b).getTime() - updatedDate(a).getTime();

/** A coding agent's bot's sessions on its machine, as conversations. */
export function botConversations(b: Pick<Bot, "machineId" | "agent">, sessions: readonly Session[]): BotConversations {
  const its = sessions.filter((s) => s.machineId === b.machineId && s.agent === b.agent);
  const mine = its.filter((s) => s.createdBy === "owner");
  return {
    open: mine.filter((s) => s.status !== SessionStatus.ARCHIVED).sort(newest),
    archived: mine.filter((s) => s.status === SessionStatus.ARCHIVED).sort(newest),
    others: its.filter((s) => s.createdBy !== "owner" && s.status !== SessionStatus.ARCHIVED).sort(newest),
  };
}

/**
 * The conversation the next message goes to (rule 1): the one chosen (a session id; "new" for a new one), else the
 * one last active. A chosen one that is gone (deleted, or no longer the owner's) falls back the same way.
 */
export function currentConversation(c: BotConversations, chosen: string | undefined): Session | "new" | undefined {
  if (chosen === "new") return "new";
  const picked = chosen ? [...c.open, ...c.archived].find((s) => s.sessionId === chosen) : undefined;
  return picked ?? c.open[0];
}

/** Why a conversation can't go on (rule 5): archived (it can, once unarchived), its project gone, its agent gone there. */
export function stuckWhy(s: Session, projects: readonly Project[], m: Machine | undefined): StuckWhy | undefined {
  if (s.status === SessionStatus.ARCHIVED) return "archived";
  if (m?.status && !m.status.agents.some((a) => a.name === s.agent)) return "agentGone";
  if (!projects.some((p) => p.machineId === s.machineId && p.projectId === s.projectId)) return "projectGone";
  return undefined;
}

/**
 * The composer's choices as a conversation has them: its project, model and effort (its mode is not kept). Its model
 * is the one it runs, as the agent's models list it (models: an alias that runs it here, 工单 #99).
 */
export function paramsOf(s: Session, models: AgentStatus["models"] = []): ConvParams {
  return { projectId: s.projectId, model: listedModel(s.model, models), effort: s.effort, mode: "" };
}

/** Whether choices differ from the conversation's own (rule 4: the next message starts a new one). */
export function paramsChanged(s: Session, p: ConvParams, models: AgentStatus["models"] = []): boolean {
  const o = paramsOf(s, models);
  return o.projectId !== p.projectId || o.model !== p.model || o.effort !== p.effort || p.mode !== "";
}

/** Which choices differ, to say what changed (rule 4). */
export function changedParams(s: Session, p: ConvParams, models: AgentStatus["models"] = []): (keyof ConvParams)[] {
  const o = paramsOf(s, models);
  return (["projectId", "model", "effort", "mode"] as const).filter((k) => o[k] !== p[k]);
}

/**
 * A new conversation that carries an old one's context (rule 5, one that can't go on): the old one's title and what
 * it came to, then the owner's new words.
 */
export function withContext(s: Session, title: string, text: string): string {
  const ended = s.result?.trim() || s.progress?.lastMessageExcerpt?.trim() || "";
  return [`接着之前的对话《${title}》继续。`, ended ? `它当时的结论：\n${ended}` : "", "", text].filter((x, i) => x !== "" || i === 2).join("\n");
}

/** A project's sessions, as the session page's list shows them (工单 #90). */
export type ProjectSessions = { projectId: string; sessions: Session[] };

/**
 * The sessions beside an open one (工单 #90, after DSH's sidebar): the same coding agent's on the same machine, by
 * project — the project with the latest activity first, each newest first — archived ones left out but the open one.
 */
export function sessionsByProject(open: Session, sessions: readonly Session[]): ProjectSessions[] {
  const by = new Map<string, Session[]>();
  for (const s of sessions) {
    if (s.machineId !== open.machineId || s.agent !== open.agent) continue;
    if (s.status === SessionStatus.ARCHIVED && s.sessionId !== open.sessionId) continue;
    by.set(s.projectId, [...(by.get(s.projectId) ?? []), s]);
  }
  if (!by.has(open.projectId)) by.set(open.projectId, [open]);
  return [...by]
    .map(([projectId, list]) => ({ projectId, sessions: list.sort(newest) }))
    .sort((a, b) => newest(a.sessions[0], b.sessions[0]) || a.projectId.localeCompare(b.projectId));
}
