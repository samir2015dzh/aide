import type { Persona } from "@aide/proto/identity";
import type { Client, Machine } from "@aide/proto/local";
import { SessionStatus, type Session } from "@aide/proto/session";
import { agentName } from "@aide/ui";
import { codingAgentTitle } from "./agents";
import { agentEntry, clientCategory, machineConnections, type ConnRow, type ConnState, type MachineConnections } from "./connections";
import { t } from "./i18n";
import { displayTitle, isActive, updatedDate } from "./sessions";
import { agoText } from "./text";

// The roster (工单 #57, BOTARK-REF 产品结构纲要): a bot is the unit the app is
// shown in. Each coding agent a machine detected is one bot, under its
// machine; each personal agent (a grant that follows the account) is one bot,
// on the account. Nobody files them: they come from what the machines and the
// account report.
//
// And one state per bot (工单 #56): the machine page, Hand Work To, the
// counts at the top and the map all read botState, so they say the same of
// one agent, in the same words.

/** Whether work can go to it now: yes; unverified (it can be sent, but whether it is there is not confirmed); no. */
export type Usable = "yes" | "unverified" | "no";

/** Why work can't go to it, when it can't, or why it is unverified. */
export type NotReady = "offline" | "signedOut" | "paused" | "missing" | "unknown" | "checking";

export type BotState = {
  /** The connection state, as its machine's connections say (personal agents: ok online, unknown otherwise). */
  state: ConnState;
  /** Its state in a word or two, the same on every page: "Signed in", "Unknown", "Paused". */
  word: string;
  /** One sentence of what is known, the same on every page (the machine page's line under it). */
  line: string;
  usable: Usable;
  why?: NotReady;
};

/** A coding agent on a machine as one state (工单 #56); undefined: the machine reports no such agent (hidden or absent). */
export function codingBotState(m: Machine, agent: string, now: Date, c: MachineConnections = machineConnections(m, now)): BotState | undefined {
  const e = agentEntry(c, agent);
  if (!e) return undefined;
  const a = m.status?.agents.find((x) => x.name === agent);
  return stateOf(e.main, c.reachable, !!a?.pause);
}

function stateOf(main: ConnRow, reachable: boolean, paused: boolean): BotState {
  const line = main.state === "ok" || main.state === "partial" ? main.text : main.lacks || main.text;
  // Paused is the owner's own say, known whatever the machine last reported: it comes first.
  if (paused && main.state !== "missing") return { state: main.state, word: t("roster.paused"), line: t("roster.pausedLine"), usable: "no", why: "paused" };
  switch (main.state) {
    case "ok":
    case "partial":
      return { state: main.state, word: firstWord(main.text), line, usable: "yes" };
    case "checking":
      return { state: "checking", word: firstWord(main.text), line, usable: "unverified", why: "checking" };
    case "unknown":
      return reachable
        ? { state: "unknown", word: main.text, line, usable: "unverified", why: "unknown" }
        : { state: "unknown", word: main.text, line, usable: "no", why: "offline" };
    default:
      return { state: "missing", word: firstWord(main.text), line, usable: "no", why: main.fix === "install" ? "missing" : "signedOut" };
  }
}

/** "Signed in · 2.1.3" → "Signed in": the version goes on the line, not in the pill. */
function firstWord(text: string): string {
  return text.split(" · ")[0];
}

/** A personal agent's state: online now, or when it was last seen (it reports nothing else, AGENT-MESH-DESIGN §6.2). */
export function personalBotState(c: Client, now: Date): BotState {
  if (c.online) return { state: "ok", word: t("roster.online"), line: t("roster.onlineLine"), usable: "yes" };
  const s = c.lastSeen?.seconds;
  const seen = s ? new Date(Number(s) * 1000) : undefined;
  return {
    state: "unknown",
    word: t("roster.away"),
    line: seen ? t("roster.seen", { when: agoText(seen, now) }) : t("roster.neverSeen"),
    usable: "unverified",
    why: "unknown",
  };
}

export type Bot = {
  /** "m:<machine id>/<agent>" or "c:<client id>": the same bot on every page. */
  key: string;
  kind: "local" | "personal";
  /** Its one name: "DSH", "DSH · work-pc" while there are several machines, a personal agent's own name. */
  name: string;
  /** The coding agent whose logo it is drawn with ("" for a personal agent, drawn with its grant). */
  agent: string;
  machineId: string;
  /** A personal agent's grant; a coding agent's own grant, when it has one (DSH on work-pc). */
  client?: Client;
  /**
   * The owner's contacts for this coding agent on its machine (docs/personas.md): a name, an avatar, presets and the
   * session they talk in. The first is the bot's face and name; there is no Hand Work To page beside the roster.
   */
  personas: Persona[];
  state: BotState;
  /** What it is doing lately, in one line: "2 running · Fix the CI", "Last: Fix the CI · 3 hr ago", or nothing. */
  doing: string;
  running: number;
  waiting: number;
};

export type Roster = {
  personal: Bot[];
  machines: { machine: Machine; bots: Bot[] }[];
};

/** "DSH · work-pc" while there are several machines (BOTARK-REF §2: one root, the instance by its suffix). */
export function botName(agent: string, machine: Machine, machines: number): string {
  return machines > 1 ? `${codingAgentTitle(agent)} · ${machine.machineName}` : codingAgentTitle(agent);
}

/** The coding agent a grant's label names ("DSH on work-pc" → deepseek), to put the grant on that bot. */
function grantAgent(label: string): string | undefined {
  const l = label.toLowerCase();
  if (/\b(dsh|deepseek)\b/.test(l)) return "deepseek";
  if (/\bclaude\b/.test(l)) return "claude";
  if (/\bcodex\b/.test(l)) return "codex";
  return undefined;
}

/** Every bot: the personal agents, then each machine's coding agents (this computer first, then online, then by name). */
export function roster(machines: readonly Machine[], clients: readonly Client[], sessions: readonly Session[], now: Date, personas: readonly Persona[] = []): Roster {
  const sorted = [...machines].sort((a, b) => Number(b.local) - Number(a.local) || Number(b.online) - Number(a.online) || a.machineName.localeCompare(b.machineName));
  const personal: Bot[] = [];
  const localGrants = new Map<string, Client[]>();
  for (const c of clients) {
    if (c.revoked || c.herMachineId) continue;
    const k = clientCategory(c, machines);
    if (k.category === "personal") {
      const mine = sessions.filter((s) => s.createdBy === c.clientId);
      personal.push({ key: `c:${c.clientId}`, kind: "personal", name: agentName(c.label), agent: "", machineId: "", client: c, personas: [], state: personalBotState(c, now), ...doing(mine, now) });
      continue;
    }
    const id = machines.find((m) => m.machineName === k.machine)?.machineId ?? "";
    localGrants.set(id, [...(localGrants.get(id) ?? []), c]);
  }
  personal.sort((a, b) => Number(b.client!.online) - Number(a.client!.online) || a.name.localeCompare(b.name));
  const out = sorted.map((m) => {
    const c = machineConnections(m, now);
    const grants = [...(localGrants.get(m.machineId) ?? [])];
    const bots: Bot[] = [];
    for (const agent of [...new Set(c.rows.map((r) => r.agent))]) {
      const state = codingBotState(m, agent, now, c)!;
      const gi = grants.findIndex((g) => grantAgent(g.label) === agent);
      const client = gi >= 0 ? grants.splice(gi, 1)[0] : undefined;
      const mine = sessions.filter((s) => s.machineId === m.machineId && s.agent === agent);
      const own = personas.filter((p) => p.machineId === m.machineId && p.agent === agent);
      bots.push({ key: `m:${m.machineId}/${agent}`, kind: "local", name: own[0]?.name || botName(agent, m, machines.length), agent, machineId: m.machineId, client, personas: own, state, ...doing(mine, now) });
    }
    // A coding agent with a grant of its own that its machine does not list (another kind, or not detected): its own bot.
    for (const g of grants) {
      bots.push({ key: `c:${g.clientId}`, kind: "local", name: agentName(g.label), agent: grantAgent(g.label) ?? "", machineId: m.machineId, client: g, personas: [], state: personalBotState(g, now), ...doing(sessions.filter((s) => s.createdBy === g.clientId), now) });
    }
    return { machine: m, bots };
  });
  // Grants of coding agents on a machine not known here: with the personal ones, rather than nowhere.
  for (const g of localGrants.get("") ?? []) {
    personal.push({ key: `c:${g.clientId}`, kind: "local", name: agentName(g.label), agent: grantAgent(g.label) ?? "", machineId: "", client: g, personas: [], state: personalBotState(g, now), ...doing(sessions.filter((s) => s.createdBy === g.clientId), now) });
  }
  return { personal, machines: out };
}

function doing(list: readonly Session[], now: Date): { doing: string; running: number; waiting: number } {
  const live = list.filter((s) => s.status !== SessionStatus.ARCHIVED);
  const running = live.filter(isActive);
  const waiting = live.filter((s) => s.status === SessionStatus.WAITING_INPUT).length;
  const latest = [...live].sort((a, b) => updatedDate(b).getTime() - updatedDate(a).getTime())[0];
  const parts: string[] = [];
  if (waiting) parts.push(t("roster.waiting", { count: waiting }));
  if (running.length) parts.push(t("roster.running", { count: running.length }));
  const text = latest
    ? parts.length
      ? [...parts, displayTitle(running[0] ?? latest)].join(" · ")
      : t("roster.last", { title: displayTitle(latest), when: agoText(updatedDate(latest), now) })
    : "";
  return { doing: text, running: running.length, waiting };
}

/** Every bot, one list. */
export function allBots(r: Roster): Bot[] {
  return [...r.personal, ...r.machines.flatMap((g) => g.bots)];
}

/** The counts at the top (工单 #56): ready ones (usable now), unknown ones apart, of all. */
export function botCounts(bots: readonly Bot[]): { ready: number; unknown: number; total: number } {
  return {
    ready: bots.filter((b) => b.state.usable === "yes").length,
    unknown: bots.filter((b) => b.state.state === "unknown" || b.state.state === "checking").length,
    total: bots.length,
  };
}

/** The counts in words: "1 ready · 3 unknown · of 4". */
export function botCountsText(c: { ready: number; unknown: number; total: number }): string {
  return [t("roster.countReady", { count: c.ready }), c.unknown ? t("roster.countUnknown", { count: c.unknown }) : "", t("roster.countOf", { count: c.total })].filter(Boolean).join(" · ");
}
