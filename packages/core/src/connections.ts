import type { Client, Machine } from "@aide/proto/local";
import { AgentLoginState, AgentProblem, DSHBridgeInstallState, DSHBridgeState, type AgentStatus, type DSHBridge } from "@aide/proto/machine";
import { kindNamed } from "@aide/ui";
import { codingAgentTitle } from "./agents";
import { t } from "./i18n";
import { agoText } from "./text";

// Connections (工单 #49): one account of what a machine's coding agents are
// connected to, worked out from what the machine itself last reported
// (MachineStatus over end to end), so every screen that says "connected or
// not" — the overview's map, Connect Agent, the machine's page, the phone —
// says the same thing. No screen keeps its own idea of it.
//
// And the two kinds of agent (所有者 2026-11-22): a personal agent (Muse,
// Grok Bot) is the owner's and follows the account, shown wherever the
// account is; a coding agent on a machine (DSH, Claude Desktop, Claude Code,
// Codex, WorkBuddy) follows that machine and is listed under it only.

/** A finding older than this is not passed off as now: it shows as unknown, with when it was checked. */
export const STALE_AFTER_MS = 15 * 60_000;

/** ok: works; partial: works, with something missing; missing: does not work until fixed; unknown: not known now. */
export type ConnState = "ok" | "partial" | "missing" | "unknown" | "checking";

/** What a fix takes the owner to. */
export type ConnFix = "prepareBridge" | "installBridge" | "openDSH" | "claudeCLI" | "claudeLogin" | "signIn" | "install";

export type ConnRow = {
  /** "claude-desktop", "claude-cli", "deepseek", "deepseek-bridge", or the agent's name. */
  id: string;
  /** The agent whose logo it is drawn with. */
  agent: string;
  title: string;
  state: ConnState;
  /** What it is, in a few words: "Signed in", "Not installed". */
  text: string;
  /** What is missing and what that means for the owner, when anything is. */
  lacks?: string;
  /** Where to fix it. */
  fix?: ConnFix;
  /** When it was checked; unset: never. */
  checkedAt?: Date;
  /** What it works with, apart from its state: DSH's DeepSeek models (DSH is the agent, DeepSeek its account's models, 工单 #53). */
  note?: string;
};

export type MachineConnections = {
  /** The machine answers now (its findings are current, unless stale). */
  reachable: boolean;
  rows: ConnRow[];
  /** The worst of the rows: what the machine's card shows in one word. */
  state: ConnState;
  /** One line for a card: "DSH connected · Claude Desktop signed in · Claude Code not signed in". */
  summary: string;
};

const order: ConnState[] = ["missing", "unknown", "partial", "checking", "ok"];

/** The DSH bridge plugin's state as the owner tells it apart (工单 #46). */
export type BridgeView = "unprepared" | "prepared" | "installed" | "notRunning" | "outdated" | "unknown" | "installing" | "installFailed" | "installRestart";

/**
 * The bridge as the owner tells it apart (工单 #46), and the install handed to DSH while it is not answering yet
 * (工单 #59): DSH at it, it failed (and why), or installed and waiting for DSH Desktop to restart.
 */
export function bridgeView(b: DSHBridge | undefined): BridgeView {
  if (!b) return "unknown";
  if (b.state !== DSHBridgeState.DSH_BRIDGE_STATE_RUNNING) {
    switch (b.install?.state) {
      case DSHBridgeInstallState.DSH_BRIDGE_INSTALL_STATE_RUNNING:
        return "installing";
      case DSHBridgeInstallState.DSH_BRIDGE_INSTALL_STATE_FAILED:
        return "installFailed";
      case DSHBridgeInstallState.DSH_BRIDGE_INSTALL_STATE_RESTART:
        return "installRestart";
    }
  }
  switch (b.state) {
    case DSHBridgeState.DSH_BRIDGE_STATE_RUNNING:
      return b.latestVersion && b.version && newer(b.latestVersion, b.version) ? "outdated" : "installed";
    case DSHBridgeState.DSH_BRIDGE_STATE_INCOMPATIBLE:
      return "outdated";
    case DSHBridgeState.DSH_BRIDGE_STATE_NOT_RUNNING:
      return "notRunning";
    case DSHBridgeState.DSH_BRIDGE_STATE_NOT_INSTALLED:
      return b.packagePath ? "prepared" : "unprepared";
    default:
      return "unknown";
  }
}

/** a is a later version than b ("0.3.0" > "0.2.9"). */
function newer(a: string, b: string): boolean {
  const pa = a.split(".").map((x) => parseInt(x, 10) || 0);
  const pb = b.split(".").map((x) => parseInt(x, 10) || 0);
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    if ((pa[i] ?? 0) !== (pb[i] ?? 0)) return (pa[i] ?? 0) > (pb[i] ?? 0);
  }
  return false;
}

function checkedDate(a: AgentStatus): Date | undefined {
  const s = a.checkedAt?.seconds;
  return s ? new Date(Number(s) * 1000) : undefined;
}

/**
 * What a machine's coding agents are connected to, row by row: Claude
 * Desktop and the Claude Code command line apart (工单 #44), DSH and its
 * bridge plugin apart (工单 #46), then each other agent found. Hidden
 * agents are left out, as everywhere.
 */
export function machineConnections(m: Machine, now: Date): MachineConnections {
  const reachable = m.online && !m.unreachable;
  const rows: ConnRow[] = [];
  for (const a of m.status?.agents ?? []) {
    if (a.hidden) continue;
    if (a.name === "claude") rows.push(...claudeRows(a));
    else if (a.name === "deepseek") rows.push(...dshRows(a));
    else if (a.available || a.problem !== AgentProblem.NOT_FOUND || a.checking) rows.push(agentRow(a));
  }
  for (const r of rows) {
    // Not heard from now, or checked too long ago: what it last said is not what is.
    if (r.state === "checking") continue;
    const old = !r.checkedAt || now.getTime() - r.checkedAt.getTime() > STALE_AFTER_MS;
    if (!reachable || old) {
      const when = r.checkedAt ? agoText(r.checkedAt, now) : t("conn.never");
      r.lacks = !reachable ? t("conn.machineAway", { text: r.text, when }) : t("conn.stale", { text: r.text, when });
      r.state = "unknown";
      r.text = t("conn.unknown");
      r.fix = undefined;
    }
  }
  const state = rows.length ? order.find((s) => rows.some((r) => r.state === s))! : reachable ? "ok" : "unknown";
  const summary = rows.map((r) => `${r.title} ${r.text}`).join(" · ");
  return { reachable, rows, state, summary };
}

/**
 * One agent's entry on a machine's single list (工单 #53): its rows (Claude: Desktop and the command line; DSH: itself
 * and its bridge plugin; any other: its one row) under one name and one state. The state is its main row's — the
 * command line for Claude (what Aide runs), DSH itself for DSH (partial while its plugin is not there) — as the phone
 * has it, so the desktop and the phone never say two things of one agent (工单 #56).
 */
export function agentEntry(c: MachineConnections, agent: string): { rows: ConnRow[]; main: ConnRow; state: ConnState; text: string } | undefined {
  const rows = c.rows.filter((r) => r.agent === agent);
  if (!rows.length) return undefined;
  const main = rows.find((r) => r.id === agent || r.id === `${agent}-cli`) ?? rows[0];
  return { rows, main, state: main.state, text: main.text };
}

function claudeRows(a: AgentStatus): ConnRow[] {
  const checkedAt = checkedDate(a);
  if (a.checking && !checkedAt) return [{ id: "claude-cli", agent: "claude", title: t("conn.claudeCLI"), state: "checking", text: t("conn.checking") }];
  const rows: ConnRow[] = [];
  const d = a.desktop;
  if (d?.installed || a.problem === AgentProblem.APP_WITHOUT_CLI) {
    rows.push({
      id: "claude-desktop",
      agent: "claude",
      title: "Claude Desktop",
      state: d?.signedIn ? "ok" : "missing",
      text: d?.signedIn ? t("conn.signedIn") : t("conn.signedOut"),
      lacks: d?.signedIn ? undefined : t("conn.desktopSignedOut"),
      fix: d?.signedIn ? undefined : "signIn",
      checkedAt,
    });
  }
  const cli: ConnRow = { id: "claude-cli", agent: "claude", title: t("conn.claudeCLI"), state: "ok", text: "", checkedAt };
  if (a.available && a.loggedIn) {
    cli.text = a.version ? t("conn.signedInVersion", { version: a.version }) : t("conn.signedIn");
  } else if (a.available && a.login?.state === AgentLoginState.WAITING) {
    // Signing in from Aide (工单 #51): the browser is open on that machine.
    cli.state = "checking";
    cli.text = t("conn.loginWaiting");
    cli.lacks = t("conn.loginWaitingHint");
    cli.fix = "claudeLogin";
  } else if (a.available) {
    cli.state = "missing";
    cli.text = t("conn.signedOut");
    const ended = a.login?.state;
    cli.lacks =
      ended === AgentLoginState.FAILED
        ? t("conn.loginFailed", { why: a.login?.detail || t("conn.loginNoReason") })
        : ended === AgentLoginState.TIMED_OUT
          ? t("conn.loginTimedOut")
          : ended === AgentLoginState.CANCELLED
            ? t("conn.loginCancelled")
            : d?.signedIn
              ? t("conn.cliSignedOutDesktopIn")
              : t("conn.cliSignedOut");
    cli.fix = "claudeLogin";
  } else if (a.problem === AgentProblem.APP_WITHOUT_CLI) {
    cli.state = "missing";
    cli.text = t("conn.notInstalled");
    cli.lacks = t("conn.appWithoutCLI");
    cli.fix = "claudeCLI";
  } else {
    cli.state = "missing";
    cli.text = a.problem === AgentProblem.NOT_FOUND ? t("conn.notInstalled") : t("conn.broken");
    cli.lacks = a.problem === AgentProblem.NOT_FOUND ? t("conn.cliNotFound") : a.detail;
    cli.fix = "install";
  }
  rows.push(cli);
  return rows;
}

function dshRows(a: AgentStatus): ConnRow[] {
  const checkedAt = checkedDate(a);
  const title = "DSH";
  if (a.checking && !checkedAt) return [{ id: "deepseek", agent: "deepseek", title, state: "checking", text: t("conn.checking") }];
  if (!a.available) {
    return [{ id: "deepseek", agent: "deepseek", title, state: "missing", checkedAt, fix: "install",
      text: a.problem === AgentProblem.NOT_FOUND ? t("conn.notInstalled") : t("conn.broken"),
      lacks: a.problem === AgentProblem.NOT_FOUND ? undefined : a.detail }];
  }
  const models = [...new Set(a.models.map((x) => x.name || x.id).filter(Boolean))];
  const rows: ConnRow[] = [{ id: "deepseek", agent: "deepseek", title, state: "ok", checkedAt,
    text: a.version ? t("conn.detectedVersion", { version: a.version }) : t("conn.detected"),
    note: models.length ? t("conn.dshModels", { models: models.slice(0, 3).join(" · ") }) : undefined }];
  const b = a.dshBridge;
  const v = bridgeView(b);
  const bridge: ConnRow = { id: "deepseek-bridge", agent: "deepseek", title: t("conn.bridge"), state: "partial", text: t(`conn.bridgeState.${v}`, { version: b?.version ?? "", latest: b?.latestVersion ?? "" }), checkedAt };
  switch (v) {
    case "installed":
      bridge.state = "ok";
      break;
    case "notRunning":
      bridge.lacks = t("conn.bridgeNotRunning");
      bridge.fix = "openDSH";
      break;
    case "unprepared":
      bridge.lacks = t("conn.bridgeGap");
      bridge.fix = "prepareBridge";
      break;
    case "prepared":
      bridge.lacks = t("conn.bridgeGap");
      bridge.fix = "installBridge";
      break;
    case "outdated":
      bridge.lacks = t("conn.bridgeOutdated");
      bridge.fix = "prepareBridge";
      break;
    case "installing":
      bridge.state = "checking";
      bridge.lacks = t("conn.bridgeInstalling");
      bridge.fix = "installBridge";
      break;
    case "installFailed":
      bridge.lacks = b?.install?.reason || t("conn.bridgeInstallFailed");
      bridge.fix = "installBridge";
      break;
    case "installRestart":
      bridge.lacks = [t("conn.bridgeInstallRestart"), b?.install?.reason].filter(Boolean).join(" ");
      bridge.fix = "openDSH";
      break;
    default:
      bridge.state = "unknown";
  }
  rows.push(bridge);
  // DSH works only partly while its plugin is not there: say so on DSH's own line too.
  if (bridge.state !== "ok") rows[0].state = "partial";
  return rows;
}

function agentRow(a: AgentStatus): ConnRow {
  const checkedAt = checkedDate(a);
  const r: ConnRow = { id: a.name, agent: a.name, title: codingAgentTitle(a.name), state: "ok", text: "", checkedAt };
  if (a.checking && !checkedAt) return { ...r, state: "checking", text: t("conn.checking") };
  if (!a.available) {
    return { ...r, state: "missing", text: a.problem === AgentProblem.NOT_FOUND ? t("conn.notInstalled") : t("conn.broken"), lacks: a.detail || undefined, fix: "install" };
  }
  if (!a.loggedIn) return { ...r, state: "missing", text: t("conn.signedOut"), lacks: t("conn.agentSignedOut", { agent: r.title }), fix: "signIn" };
  return { ...r, text: a.version ? t("conn.readyVersion", { version: a.version }) : t("conn.ready") };
}

// The two kinds of agent.

export type AgentCategory = "personal" | "local";

/** Words that name a coding agent that runs on a machine, as a grant's label may (DSH on X, Codex, …). */
const localWords = /\b(dsh|deepseek|claude code|codex|workbuddy|codebuddy|gemini cli|copilot cli|opencode|qwen code|kimi cli|aide cli)\b/i;

/**
 * Which kind of agent a grant is, by its label (the owner's or the agent's
 * own name for it), with the machine it runs on when the label says:
 * "DSH on work-pc" or "client@work-pc" is a coding agent on the machine
 * work-pc; "Muse", "Grok Bot (cursor.com)" are personal. A coding agent's
 * name (DSH, Codex, …) is local even without its machine; anything else
 * is personal.
 */
export function grantCategory(label: string, machineNames: readonly string[]): { category: AgentCategory; machine?: string } {
  const tail = /^(.+?)\s+on\s+(.+)$/i.exec(label)?.[2] ?? /^[^@\s]+@(\S+)$/.exec(label)?.[1];
  const machine = tail ? machineNames.find((n) => n.toLowerCase() === tail.trim().toLowerCase()) : undefined;
  if (machine) return { category: "local", machine };
  if (kindNamed(label)) return { category: "personal" };
  if (localWords.test(label) || /^client@/i.test(label)) return { category: "local", machine: tail?.trim() };
  return { category: "personal" };
}

/**
 * Which kind of agent a client is (工单 #52): what the owner set, kept with
 * its grant, else what its name tells (grantCategory). Every screen asks
 * this, so the map, the machine page and Hand Work To never disagree.
 */
export function clientCategory(c: Pick<Client, "label" | "category" | "categoryMachineId">, machines: readonly Machine[]): { category: AgentCategory; machine?: string; set: boolean } {
  if (c.category === "personal" || c.category === "local") {
    return { category: c.category, machine: machines.find((m) => m.machineId === c.categoryMachineId)?.machineName, set: true };
  }
  return { ...grantCategory(c.label, machines.map((m) => m.machineName)), set: false };
}

/** The grants split by kind: personal ones (the account's), and coding agents by the machine id they run on ("" when it is not known). */
export function grantsByCategory(clients: readonly Client[], machines: readonly Machine[]): { personal: Client[]; local: Map<string, Client[]> } {
  const personal: Client[] = [];
  const local = new Map<string, Client[]>();
  for (const c of clients) {
    if (c.revoked) continue;
    const k = clientCategory(c, machines);
    if (k.category === "personal") {
      personal.push(c);
      continue;
    }
    const id = machines.find((m) => m.machineName === k.machine)?.machineId ?? "";
    local.set(id, [...(local.get(id) ?? []), c]);
  }
  return { personal, local };
}
