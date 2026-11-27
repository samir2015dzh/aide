import { useEffect, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type MouseEvent as ReactMouseEvent } from "react";
import type { Persona } from "@aide/proto/identity";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { SessionStatus, type Session } from "@aide/proto/session";
import type { Machine } from "@aide/proto/local";
import { RoomDeliveryState, RoomMode, type Room, type RoomMessage } from "@aide/proto/machine";
import { RoomTimeline, SidebarRooms } from "./Rooms";
import {
  allBots, botConversations, changedParams, codingAgentTitle, currentConversation, displayTitle, dshAgent, isActive, listTime, modeChoice, modeLabel, paramsChanged, paramsOf, projectTitle, roster, stateDetail, stuckWhy, updatedDate, whileReconnecting, withContext,
  type Bot, type ConvParams, type Roster,
} from "@aide/core";
import { botLook, botMood, botSeed, botSessions, personaLook, reactMs, type BotMood } from "@aide/core/avatar";
import { projectsFor } from "../AgentList";
import { local } from "../api";
import { ClaudeLogin, claudeLoginWaiting, botDot } from "../Connections";
import { Face, grantApp, StateText, StatusDot, useLeaving } from "../components";
import { Icon } from "../icons";
import { modelOptions } from "../modelOptions";
import { Picker, type PickerOption } from "../Picker";
import { LookSheet } from "../settings/LookSheet";
import { blankPersona, lookFields, personasChanged } from "../sheets/Contacts";
import { errorText, useNow, useStore, type Sheet } from "../store";
import { Composer, attachmentsOf, useAttachments, type ComposerState } from "./Composer";
import { OwnSession, OwnSessions } from "./OwnSessions";
import { BotSettings } from "./AgentSettings";
import { TaskItem, useTaskRows } from "../TasksPage";
import "./bots.css";
import { freshModels } from "../agents";

// The roster (工单 #57, #63, BOTARK-REF 产品结构纲要): a bot is the unit the app is shown in, and the sidebar is bots
// alone — the ones pinned as tiles, then the personal agents, the coding agents (a group per machine, headed by its
// name, 工单 #75), and the rooms. No machine, device state or task list is there: a
// machine is said only through its bots' states, and is managed on the devices page (Settings › Machines). A click
// opens the bot's page: its conversations in the middle with the box to write to it, its task board beside, and a
// button to its settings, a page of their own (工单 #97).

// The owner's contacts (docs/personas.md), read once for every bot page and the roster, again when one changes.
let personaCache: Persona[] = [];
const personaSubs = new Set<() => void>();
function loadPersonas() {
  local.listPersonas({}).then((r) => ((personaCache = r.personas), personaSubs.forEach((f) => f())), () => {});
}

export function usePersonas(): Persona[] {
  const [, tick] = useState(0);
  useEffect(() => {
    const f = () => tick((x) => x + 1);
    personaSubs.add(f);
    if (personaSubs.size === 1) loadPersonas();
    window.addEventListener(personasChanged, loadPersonas);
    return () => (personaSubs.delete(f), window.removeEventListener(personasChanged, loadPersonas));
  }, []);
  return personaCache;
}

export function useRoster(): Roster {
  const st = useStore();
  const now = useNow(30_000);
  const personas = usePersonas();
  // While the link to aided is being restored, every bot says so (工单 #87), not what it was last known as.
  return useMemo(() => {
    const r = roster(st.machines, st.clients, st.sessions, now, personas);
    return st.reconnecting ? whileReconnecting(r) : r;
  }, [st.machines, st.clients, st.sessions, now, personas, st.reconnecting]);
}

// Pinned bots (BotArk's tiles at the top of the sidebar): this computer's choice, kept with the window's own.
const pinKey = "pinned-bots";
const pinSubs = new Set<() => void>();
function readPins(): string[] {
  try {
    const v = JSON.parse(localStorage.getItem(pinKey) ?? "[]");
    return Array.isArray(v) ? v.filter((x) => typeof x === "string") : [];
  } catch {
    return [];
  }
}

export function usePins(): [string[], (key: string, on: boolean) => void] {
  const [pins, setPins] = useState(readPins);
  useEffect(() => {
    const f = () => setPins(readPins());
    pinSubs.add(f);
    return () => void pinSubs.delete(f);
  }, []);
  const pin = (key: string, on: boolean) => {
    const next = on ? [...readPins().filter((k) => k !== key), key] : readPins().filter((k) => k !== key);
    try {
      localStorage.setItem(pinKey, JSON.stringify(next));
    } catch {
      // Without storage the pin lasts as long as the window.
    }
    setPins(next);
    pinSubs.forEach((f) => f());
  };
  return [pins, pin];
}

/**
 * A bot's face (工单 #83): its Grok Bot character (BotArk's GrokBotMark), the same on every page — in the look the owner
 * gave it (its contact's, else its grant's), else its seed's; its app's logo on its corner where there is room. Only
 * its real state moves it (botMood: idle, at work by what it does, waiting on the owner, asleep when paused, a moment
 * of done or failed); away or unknown, it rests, faded. The dot on its corner (dot) is its state's colour. still: it
 * rests (where many show at once). edit: a click opens what changes its look (its contact, its grant's look, or a new
 * contact), and it follows the pointer: its own page.
 */
export function BotFace({ b, size = 28, dot = false, still = false, edit = false }: { b: Bot; size?: number; dot?: boolean; still?: boolean; edit?: boolean }) {
  const st = useStore();
  const { t } = useTranslation("common");
  const [look, setLook] = useState(false);
  const mood = useMood(b);
  const face = (
    <span className="bav" style={{ width: size, height: size }} aria-hidden="true">
      <Face seed={botSeed(b)} look={botLook(b)} mood={mood} size={size} app={size >= 34 ? b.agent || (b.client && grantApp(b.client)) : undefined} still={still} follow={edit} />
      {dot && <i className={`dotc ${botDot(b)}`} />}
    </span>
  );
  if (!edit) return face;
  const p = b.personas[0];
  const open = () => {
    if (p) st.set({ sheet: { kind: "persona", draft: { ...blankPersona, ...p } } });
    else if (b.key.startsWith("m:")) st.set({ sheet: { kind: "persona", draft: { ...blankPersona, ...lookFields(botLook(b)), machineId: b.machineId, agent: b.agent, name: b.name } } });
    else setLook(true);
  };
  return (
    <>
      <button className="bav-edit" title={t("bots.faceEdit")} aria-label={t("bots.faceEdit")} onClick={open}>{face}</button>
      {look && b.client && <LookSheet c={b.client} st={st} close={() => setLook(false)} />}
    </>
  );
}

/** A bot's face's state now; a moment of done or failed is read again once it is over. */
function useMood(b: Bot): BotMood {
  const st = useStore();
  const [, tick] = useState(0);
  const mood = botMood(b, st.sessions, Date.now());
  const brief = mood.state === "finished" || mood.state === "failed";
  useEffect(() => {
    if (!brief) return;
    const id = setTimeout(() => tick((x) => x + 1), reactMs);
    return () => clearTimeout(id);
  }, [brief]);
  return mood;
}

/** A contact's face, on its bot's settings: its own look, its bot's state. */
export function PersonaFace({ b, p, size }: { b: Bot; p: Persona; size: number }) {
  const seed = botSeed(b);
  const mood = useMood(b);
  return (
    <span className="bav" style={{ width: size, height: size }} aria-hidden="true">
      <Face seed={seed} look={personaLook(p, seed)} mood={mood} size={size} />
    </span>
  );
}

/** Its state as a dot and a word, the same words as everywhere (工单 #56 / #135 P25). */
export function BotPill({ b }: { b: Bot }) {
  const d = botDot(b);
  return (
    <span className="bot-pill" data-state={b.state.state} data-usable={b.state.usable} data-dot={d} title={b.state.line}>
      <i className={`dotc ${d}`} />
      {b.state.word}
    </span>
  );
}

/** Bot 右键菜单（DESIGN-107 §0.5c #6B / §15A）：打开 / 设置… / 置顶 / 改名和换头像… / 从名册删除… */
async function botMenu(
  e: ReactMouseEvent,
  b: Bot,
  pinned: boolean,
  pin: (key: string, on: boolean) => void,
  open: () => void,
  openSettings: () => void,
  editLook: () => void,
  remove: () => void,
  t: TFunction<"common">,
) {
  e.preventDefault();
  const id = await window.aide?.popupMenu([
    { id: "open", label: t("bots.open") },
    { id: "settings", label: t("bots.settingsMenu") },
    { separator: true },
    { id: "pin", label: t(pinned ? "bots.unpin" : "bots.pin") },
    { id: "look", label: t("bots.renameAndFace") },
    { separator: true },
    { id: "remove", label: t("bots.remove.menu") },
  ]);
  if (id === "open") open();
  if (id === "settings") openSettings();
  if (id === "pin") pin(b.key, !pinned);
  if (id === "look") editLook();
  if (id === "remove") remove();
}

/**
 * Deleting a bot from the roster (工单 #82), the owner's alone, asked first with what it does. A coding agent its
 * machine found is hidden there: off the roster, its sessions untouched, shown again from the devices page. An agent
 * with a grant of its own (a personal agent, or a coding agent known by its grant alone) has it revoked: one connected
 * now is disconnected first; the server then forgets it (its grant, card, inbox), and it can't connect again unless
 * it is connected anew. The sessions it handed out stay on their machines.
 */
function removeSheet(b: Bot, machines: readonly Machine[], t: TFunction<"common">): Sheet {
  const name = b.name;
  if (b.key.startsWith("m:")) {
    const machine = machines.find((m) => m.machineId === b.machineId)?.machineName ?? "";
    return {
      kind: "confirm",
      title: t("bots.remove.localTitle", { name }),
      message: t("bots.remove.localMessage", { name, machine }),
      button: t("bots.remove.localButton"),
      done: t("bots.remove.localDone", { name }),
      run: () => local.setAgentHidden({ machineId: b.machineId, agent: b.agent, hidden: true }),
    };
  }
  const c = b.client!;
  return {
    kind: "confirm",
    title: t("bots.remove.title", { name }),
    message: t(c.online ? "bots.remove.onlineMessage" : "bots.remove.offlineMessage", { name }),
    button: t(c.online ? "bots.remove.onlineButton" : "bots.remove.button"),
    done: t("bots.remove.done", { name }),
    run: () => local.revokeClient({ grantId: c.grantId }),
  };
}

/**
 * The sidebar's one content (工单 #63): the pinned bots as tiles, the personal agents, the coding agents, the rooms —
 * groups that fold, dense enough that the usual bots show without scrolling.
 */
export function SidebarRoster() {
  const st = useStore();
  const { t } = useTranslation("common");
  const r = useRoster();
  const [pins, pin] = usePins();
  const [lookBot, setLookBot] = useState<Bot | null>(null);
  const all = allBots(r);
  const sel = st.selection.kind === "bot" ? st.selection.key : "";
  const open = (b: Bot) => st.set({ selection: { kind: "bot", key: b.key } });
  const openSettings = (b: Bot) => st.set({ selection: { kind: "bot", key: b.key, pane: "settings" } });
  const editLook = (b: Bot) => {
    const p = b.personas[0];
    if (p) st.set({ sheet: { kind: "persona", draft: { ...blankPersona, ...p } } });
    else if (b.key.startsWith("m:")) st.set({ sheet: { kind: "persona", draft: { ...blankPersona, ...lookFields(botLook(b)), machineId: b.machineId, agent: b.agent, name: b.name } } });
    else if (b.client) setLookBot(b);
  };
  const pinned = pins.map((k) => all.find((b) => b.key === k)).filter((b): b is Bot => !!b);
  const group = (id: string, title: string, bots: Bot[], short = false) => {
    const key = `bots:${id}`;
    const shown = !st.collapsed.includes(key);
    if (bots.length === 0) return null;
    return (
      <div key={id} role="group" aria-label={title}>
        <button className={`grp-h ${shown ? "" : "shut"}`} aria-expanded={shown} onClick={() => st.set({ collapsed: shown ? [...st.collapsed, key] : st.collapsed.filter((c) => c !== key) })}>
          {title}
          <Icon name="caret" className="sm" />
        </button>
        {shown && bots.map((b) => (
          <button
            key={b.key}
            className={`bot-row ${sel === b.key ? "sel" : ""}`}
            aria-current={sel === b.key || undefined}
            onClick={() => open(b)}
            onContextMenu={(e) => botMenu(e, b, pins.includes(b.key), pin, () => open(b), () => openSettings(b), () => editLook(b), () => st.set({ sheet: removeSheet(b, st.machines, t) }), t)}
          >
            <BotFace b={b} size={32} dot />
            <span className="bot-t">
              <span className="bot-l1">
                <b>{short ? b.shortName : b.name}</b>
                <span className="bot-word" data-usable={b.state.usable} data-dot={botDot(b)}>{b.state.word}</span>
              </span>
              {(b.doing || stateDetail(b.state)) && <span className="bot-l2">{b.doing || stateDetail(b.state)}</span>}
            </span>
            {b.waiting > 0 && <span className="cnt-w">{b.waiting}</span>}
          </button>
        ))}
      </div>
    );
  };
  return (
    <section className="side-bots" aria-label={t("bots.title")}>
      {pinned.length > 0 && (
        <div className="bot-tiles" role="list" aria-label={t("bots.pinned")}>
          {pinned.map((b) => (
            <button
              key={b.key}
              role="listitem"
              className={`bot-tile ${sel === b.key ? "sel" : ""}`}
              title={`${b.name} · ${b.state.word}`}
              onClick={() => open(b)}
              onContextMenu={(e) => botMenu(e, b, true, pin, () => open(b), () => openSettings(b), () => editLook(b), () => st.set({ sheet: removeSheet(b, st.machines, t) }), t)}
            >
              <BotFace b={b} size={34} dot />
              <span>{b.name}</span>
            </button>
          ))}
        </div>
      )}
      {group("personal", t("bots.personal"), r.personal)}
      {/* The coding agents by machine (工单 #75): the head is the machine's name, so a row names the agent alone. */}
      {r.machines.map((g) => group(`m:${g.machine.machineId}`, g.machine.machineName, g.bots, true))}
      <SidebarRooms />
      {all.length === 0 && st.status && (
        <div className="side-empty">
          <b>{t("bots.noneTitle")}</b>
          {t("bots.noneBody")}
          <button className="link" onClick={() => window.aide?.openSettings("connect")}>{t("bots.noneConnect")}</button>
        </div>
      )}
      {lookBot?.client && <LookSheet c={lookBot.client} st={st} close={() => setLookBot(null)} />}
    </section>
  );
}

/**
 * A bot's own page (工单 #57, #63, #97): who it is and how it stands at the top, with the way to its settings; its
 * conversations in the middle; beside them its task board. settings: its settings page instead (AgentSettings.tsx).
 */
export function BotPage({ botKey, native, settings = false }: { botKey: string; native?: string; settings?: boolean }) {
  const st = useStore();
  const { t } = useTranslation("common");
  const r = useRoster();
  const [pins, pin] = usePins();
  const b = allBots(r).find((x) => x.key === botKey);
  const { set } = st;
  // Gone (its machine forgot it, its grant was revoked): back to the overview.
  useEffect(() => {
    if (!b && st.status) set({ selection: { kind: "overview" } });
  }, [b, set, st.status]);
  if (!b) return null;
  const m = st.machines.find((x) => x.machineId === b.machineId);
  const coding = !!m && !!b.agent && !b.key.startsWith("c:");
  if (settings) return <BotSettings b={b} m={m} />;
  const pinned = pins.includes(b.key);
  // Not usable for a reason its machine can fix: the way there, with its state (not a setting of its own).
  const fix = coding && b.state.usable === "no" && b.state.why !== "paused";
  // Claude Code's command line not signed in, or unconfirmed: signed in from here, on its machine (工单 #51, #108).
  const login = coding && b.state.fix === "claudeLogin";
  return (
    <div className="bot-page" role="region" aria-label={b.name}>
      <header className="bot-head drag">
        <BotFace b={b} size={44} dot edit />
        <div className="bot-head-t">
          <div className="bot-head-l1">
            <h1>{b.name}</h1>
            <BotPill b={b} />
          </div>
          <div className="bot-head-l2">
            <span>{stateDetail(b.state)}</span>
            {fix && !login && <button className="link" onClick={() => window.aide?.openSettings(`machines/${m!.machineId}`)}>{t("bots.fixOnDevices")}</button>}
          </div>
          {login && <ClaudeLogin m={m!} waiting={claudeLoginWaiting(m!)} />}
        </div>
        <button className="btn sm bot-settings" onClick={() => set({ selection: { kind: "bot", key: b.key, pane: "settings" } })}>
          <Icon name="gear" className="sm" />
          {t("bots.settings")}
        </button>
        <button className={`tb bot-pin ${pinned ? "on" : ""}`} aria-pressed={pinned} title={t(pinned ? "bots.unpin" : "bots.pin")} aria-label={t(pinned ? "bots.unpin" : "bots.pin")} onClick={() => pin(b.key, !pinned)}>
          <Icon name="pin" />
        </button>
      </header>
      <div className="bot-body">
        {coding && native ? <OwnSession b={b} m={m!} id={native} /> : coding ? <Conversations b={b} m={m!} /> : <DirectChat b={b} />}
        <TaskBoard b={b} />
      </div>
    </div>
  );
}

/**
 * Its task board (工单 #97 补充二): what it is working on — the work handed to it, or that it handed out (a personal
 * agent's) — waiting on the owner or needing a look first, then in progress, then done lately, each with its state.
 * The rows are 任务动态's own (useTaskRows) narrowed to this bot's sessions as the roster counts them, so the two say
 * the same of a task (工单 #102).
 */
function TaskBoard({ b }: { b: Bot }) {
  const st = useStore();
  const { t } = useTranslation("common");
  const now = useNow(5_000);
  const all = useTaskRows();
  const rows = useMemo(() => {
    const mine = new Set(botSessions(b, st.sessions).map((s) => s.sessionId));
    return all.filter((r) => mine.has(r.session.sessionId));
  }, [all, b, st.sessions]);
  const groups = [
    { id: "waiting" as const, rows: rows.filter((r) => r.rank === 0) },
    { id: "running" as const, rows: rows.filter((r) => r.rank === 1) },
    { id: "done" as const, rows: rows.filter((r) => r.rank >= 2) },
  ].filter((g) => g.rows.length > 0);
  // Nothing on it: no column of two sentences (工单 #140 P26: 320 × 919 for "none yet"); 任务动态 still has them all.
  if (groups.length === 0) return null;
  return (
    <aside className="bot-bench bot-board" aria-label={t("bots.board.title")}>
      <div className="bot-sec-h">
        {t("bots.board.title")}
        <button className="link" onClick={() => st.set({ selection: { kind: "tasks" } })}>{t("bots.board.all")}</button>
      </div>
      {groups.map((g) => (
        <section key={g.id} aria-label={t(`bots.board.${g.id}`)}>
          <div className="bot-sec-h">{t(`bots.board.${g.id}`)} · {g.rows.length}</div>
          <ul className="held-list">
            {g.rows.map((r) => <TaskItem key={r.session.sessionId} r={r} now={now} />)}
          </ul>
        </section>
      ))}
    </aside>
  );
}

function WorkRow({ s }: { s: Session }) {
  const st = useStore();
  const now = useNow(30_000);
  return (
    <button className="frow bot-work" role="listitem" onClick={() => st.select(s.sessionId)}>
      <StatusDot s={s} />
      <span className="bot-work-t">
        <b>{displayTitle(s)}</b>
        <span><StateText s={s} /> · {s.projectId}</span>
      </span>
      <span className="bot-work-when">{listTime(updatedDate(s), now)}</span>
    </button>
  );
}

// The conversation the owner chose for each bot on this computer (工单 #67 rule 1): a session id, or "new".
const convKey = (bot: string) => `bot-conv:${bot}`;
function readChoice(bot: string): string | undefined {
  try {
    return localStorage.getItem(convKey(bot)) ?? undefined;
  } catch {
    return undefined;
  }
}
function saveChoice(bot: string, v: string) {
  try {
    localStorage.setItem(convKey(bot), v);
  } catch {
    // Without storage the choice lasts as long as the page.
  }
}

/**
 * A coding agent's conversations (工单 #67) and the box to write to it (工单 #66): which one the next message goes on in
 * (its title and when it started, rule 3), a button for a new one (rule 2), every one to go back to (rule 5), the
 * archived ones apart (rule 6), and the work others handed it.
 */
function Conversations({ b, m }: { b: Bot; m: Machine }) {
  const st = useStore();
  const { t } = useTranslation("common");
  const now = useNow(30_000);
  const [chosen, setChosen] = useState(() => readChoice(b.key));
  const choose = (v: string) => (saveChoice(b.key, v), setChosen(v));
  const convs = useMemo(() => botConversations(b, st.sessions), [b, st.sessions]);
  const cur = currentConversation(convs, chosen);
  const current = cur === "new" ? undefined : cur;
  const projects = projectsFor(st.projects.filter((p) => p.machineId === m.machineId), b.agent);
  const persona = b.personas[0];
  const a = m.status?.agents.find((x) => x.name === b.agent);
  // The composer's choices: the current conversation's own, else the contact's presets, else the first project.
  const base = (): ConvParams =>
    current ? paramsOf(current, a?.models) : { projectId: persona?.projectId || projects[0]?.projectId || "", model: persona?.model ?? "", effort: persona?.effort ?? "", mode: persona?.mode ?? "" };
  const [params, setParams] = useState<ConvParams>(base);
  const curId = current?.sessionId ?? cur ?? "";
  // Again once it says the model it runs (工单 #99: read back as it starts), so that is no change.
  useEffect(() => setParams(base()), [curId, current?.model]); // eslint-disable-line react-hooks/exhaustive-deps
  const [text, setText] = useState("");
  const [key, setKey] = useState(() => crypto.randomUUID());
  const [state, setState] = useState<ComposerState>({ kind: "idle" });
  const attach = useAttachments();
  const [showArchived, setShowArchived] = useState(false);
  const [listOpen, setListOpen] = useState(false);
  const totalConvs = convs.open.length + convs.archived.length + convs.others.length;
  const stuck = current ? stuckWhy(current, st.projects, m) : undefined;
  const fresh = !current || paramsChanged(current, params, a?.models);
  const changed = current ? changedParams(current, params, a?.models) : [];
  const usable = b.state.usable !== "no";
  // DSH's permission next to the project's (工单 #89): its default named, a choice above it said before sending.
  const permission = modeChoice(b.agent, params.mode, projects.find((p) => p.projectId === params.projectId), a?.modes ?? []);
  const send = async (opts: { withContextOf?: Session } = {}) => {
    const body = text.trim();
    if ((!body && attach.files.length === 0) || state.kind === "sending") return;
    setState({ kind: "sending" });
    try {
      const attachments = await attachmentsOf(attach.files);
      let to: Session | undefined;
      if (current && !fresh && !opts.withContextOf) {
        if (current.status === SessionStatus.ARCHIVED) await local.unarchiveSession({ sessionId: current.sessionId }); // rule 6
        to = (await local.sendMessage({ sessionId: current.sessionId, text: body, attachments })).session;
        setState({ kind: "done", text: t("composer.sentIn", { title: displayTitle(current) }) });
      } else {
        const prompt = opts.withContextOf ? withContext(opts.withContextOf, displayTitle(opts.withContextOf), body) : body;
        const res = await local.startSession({
          machineId: m.machineId,
          session: { idempotencyKey: key, agent: b.agent, projectId: params.projectId, prompt, model: params.model, effort: params.effort, mode: params.mode, attachments },
        });
        to = res.session;
        if (to) choose(to.sessionId);
        // Rule 4: a new one because a choice changed is said so, here and now.
        setState({ kind: "done", text: current && changed.length ? t("composer.newBecause", { what: changed.map((k) => t(`composer.param.${k}`)).join("、") }) : t("composer.newStarted") });
      }
      setText("");
      attach.clear();
      setKey(crypto.randomUUID());
    } catch (err) {
      setState({ kind: "error", text: errorText(err), retry: () => void send(opts) });
    }
  };
  const archive = (s: Session, on: boolean) =>
    (on ? local.archiveSession({ sessionId: s.sessionId }) : local.unarchiveSession({ sessionId: s.sessionId })).then(
      (res) => setState({ kind: "done", text: t(!on ? "conv.unarchived" : res.session?.workspaceKept ? "conv.archivedKept" : "conv.archived", { title: displayTitle(s) }) }),
      (e) => setState({ kind: "error", text: errorText(e) }),
    );
  // What the next message does, said over the box before it is sent.
  const why = !usable
    ? { text: t("bots.cantSend", { word: b.state.word, line: b.state.line }), tone: "warn" as const }
    : stuck === "archived"
      ? { text: t("conv.archivedNote"), tone: "quiet" as const, action: { label: t("conv.unarchive"), run: () => void archive(current!, false) } }
      : stuck
        ? { text: t(stuck === "projectGone" ? "conv.stuckProject" : "conv.stuckAgent"), tone: "warn" as const }
        : projects.length === 0
          ? { text: t("bots.noProject"), tone: "quiet" as const, action: { label: t("bots.settings"), run: () => st.set({ selection: { kind: "bot", key: b.key, pane: "settings" } }) } }
          : permission.above
            ? { text: permission.above, tone: "warn" as const }
            : b.state.usable === "unverified"
              ? { text: t("bots.unverified"), tone: "quiet" as const }
              : current && fresh
                ? { text: t("conv.willStartNew", { what: changed.map((k) => t(`composer.param.${k}`)).join("、") }), tone: "quiet" as const }
                : undefined;
  // 工单 #98: an ACP agent (DSH) says its models only in a session: before its first here, the list says so.
  const modelsLater = a && a.models.length === 0 && b.agent !== "claude" && b.agent !== "codex"
    ? t(st.sessions.some((s) => s.machineId === m.machineId && s.agent === b.agent) ? "composer.modelsNone" : "composer.modelsAfterFirst", { agent: codingAgentTitle(b.agent) })
    : undefined;
  const models: PickerOption<string>[] = [...modelOptions(t, a, params.model), ...(modelsLater ? [{ value: "\0later", label: modelsLater, disabled: true }] : [])];
  const efforts = a?.models.find((x) => x.id === params.model)?.efforts ?? [];
  const modes: PickerOption<string>[] = [{ value: "", label: permission.defaultLabel ?? t("composer.defaultMode") }, ...(a?.modes ?? []).map((x) => ({ value: x.id, label: modeLabel(b.agent, x) }))];
  // The same four, in the same order, for every coding agent (工单 #140 P11: three on one page, four on another), each
  // saying what it is; what an agent does not let Aide choose shows as its own default, not as a missing slot.
  const fixed = (label: string) => (
    <span className="picker chip sm picker-fixed" title={t("composer.notChosen", { agent: codingAgentTitle(b.agent) })}>
      <span className="picker-k">{label}</span>
      <span className="picker-v">{t("composer.agentsOwn", { agent: codingAgentTitle(b.agent) })}</span>
    </span>
  );
  const modeLabelText = t(b.agent === dshAgent ? "composer.permission" : "composer.param.mode");
  const chips = (
    <>
      {projects.length > 0 && (
        <Picker chip showLabel size="sm" label={t("composer.param.projectId")} value={params.projectId} onChange={(v) => setParams({ ...params, projectId: v })}
          options={projects.map((p) => ({ value: p.projectId, label: projectTitle(p), icon: <Icon name="folder" className="sm" /> }))} />
      )}
      {(a?.models.length ?? 0) > 0 || modelsLater
        ? <Picker chip showLabel size="sm" label={t("composer.param.model")} value={params.model} options={models} onChange={(v) => setParams({ ...params, model: v, effort: "" })} onOpen={() => freshModels(m)} />
        : fixed(t("composer.param.model"))}
      {efforts.length > 0 ? (
        <Picker chip showLabel size="sm" label={t("composer.param.effort")} value={params.effort} onChange={(v) => setParams({ ...params, effort: v })}
          options={[{ value: "", label: t("composer.defaultEffort") }, ...efforts.map((e) => ({ value: e, label: t(`firstrun:prepare.efforts.${e}`, { defaultValue: e }) }))]} />
      ) : fixed(t("composer.param.effort"))}
      {(a?.modes.length ?? 0) > 0
        ? <Picker chip showLabel size="sm" label={modeLabelText} value={params.mode} options={modes} onChange={(v) => setParams({ ...params, mode: v })} />
        : fixed(modeLabelText)}
    </>
  );
  const row = (s: Session, archived = false) => {
    const isCur = s.sessionId === current?.sessionId;
    const why = stuckWhy(s, st.projects, m);
    return (
      <div key={s.sessionId} className={`conv-row ${isCur ? "cur" : ""}`} role="listitem" aria-current={isCur || undefined}>
        <button className="conv-main" onClick={() => (choose(s.sessionId), closeList())} title={t("conv.goOnTip")}>
          <StatusDot s={s} />
          <span className="conv-t">
            <b>{displayTitle(s)}</b>
            <span>
              <StateText s={s} /> · {listTime(updatedDate(s), now)}
              {why && why !== "archived" && <em className="conv-stuck"> · {t("conv.readOnly")}</em>}
            </span>
          </span>
          {isCur && <span className="conv-badge">{t("conv.current")}</span>}
        </button>
        <button className="tb" title={t("conv.open")} aria-label={t("conv.open")} onClick={() => (setListOpen(false), st.select(s.sessionId))}><Icon name="expand" /></button>
        <button className="tb" title={t(archived ? "conv.unarchive" : "conv.archive")} aria-label={t(archived ? "conv.unarchive" : "conv.archive")} onClick={() => void archive(s, !archived)}>
          <Icon name={archived ? "unarchive" : "archive"} />
        </button>
      </div>
    );
  };
  const curArchived = current?.status === SessionStatus.ARCHIVED;
  const curLive = !!current && (isActive(current) || current.status === SessionStatus.WAITING_INPUT);
  const curPrefixKey = curArchived ? "conv.archivedIs" : curLive ? "conv.currentIs" : "conv.selectedIs";
  const countDetail = [
    convs.open.length > 0 ? t("conv.partOpen", { count: convs.open.length }) : "",
    convs.archived.length > 0 ? t("conv.partArchived", { count: convs.archived.length }) : "",
    convs.others.length > 0 ? t("conv.partOthers", { count: convs.others.length }) : "",
  ].filter(Boolean).join(" · ");
  // The archived stay apart in their group, folded (工单 #66/#67 rule 6); the count under the card says how many
  // there are of each (#135 P33), so the list need not unfold to match it.
  const toggleList = () => setListOpen((v) => !v);
  const flyout = useLeaving(listOpen ? true : null);
  const flyoutRef = useRef<HTMLDivElement>(null);
  const allBtnRef = useRef<HTMLButtonElement>(null);
  // Closed from inside (×, a conversation chosen), focus goes back to the button that opened it, not to the page.
  const closeList = () => {
    if (flyoutRef.current?.contains(document.activeElement)) allBtnRef.current?.focus();
    setListOpen(false);
  };
  useEffect(() => {
    if (!listOpen) return;
    const outside = (e: MouseEvent) => {
      const el = e.target as Node;
      if (!flyoutRef.current?.contains(el) && !allBtnRef.current?.contains(el)) setListOpen(false);
    };
    const esc = (e: KeyboardEvent) => {
      if (e.key !== "Escape" || e.defaultPrevented) return;
      // Esc in a sheet or dialog opened over the page closes that, not this too.
      const dialog = (e.target as Element | null)?.closest?.('[role="dialog"], [role="alertdialog"]');
      if ((dialog && dialog !== flyoutRef.current) || document.querySelector('[aria-modal="true"]')) return;
      e.preventDefault();
      setListOpen(false);
      allBtnRef.current?.focus();
    };
    document.addEventListener("mousedown", outside);
    document.addEventListener("keydown", esc);
    return () => {
      document.removeEventListener("mousedown", outside);
      document.removeEventListener("keydown", esc);
    };
  }, [listOpen]);
  const onFlyoutKey = (e: ReactKeyboardEvent) => {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    const items = [...(flyoutRef.current?.querySelectorAll<HTMLElement>(".conv-main, .bot-work, .conv-group:not(.static)") ?? [])];
    if (items.length === 0) return;
    e.preventDefault();
    const i = items.indexOf(document.activeElement as HTMLElement);
    const next = e.key === "ArrowDown" ? (i + 1) % items.length : i <= 0 ? items.length - 1 : i - 1;
    items[next]?.focus();
  };
  const recentOthers = current ? convs.open.filter((s) => s.sessionId !== current.sessionId).slice(0, 3) : convs.open.slice(0, 3);
  const shownCount = (current ? 1 : 0) + recentOthers.length;
  const excerpt = current?.progress?.lastMessageExcerpt?.trim() ?? "";
  return (
    <main className="bot-main">
      {/* 对话条（DESIGN-107 §0.5c #4A / #135 P03/P04/P33）：前缀与当前会话真实状态一致，计数与列表口径对齐 */}
      <div className="conv-head" role="status">
        {current ? (
          <>
            <span className="conv-head-k">{t(curPrefixKey)}</span>
            <button className="link conv-head-t" onClick={() => st.select(current.sessionId)}>{displayTitle(current)}</button>
            <span className="muted">{t("conv.startedAt", { when: listTime(new Date(Number(current.createdAt?.seconds ?? 0n) * 1000), now) })}</span>
          </>
        ) : (
          <span className="conv-head-k">{t(convs.open.length ? "conv.nextIsNew" : "conv.firstIsNew")}</span>
        )}
        <button
          ref={allBtnRef}
          className={`btn sm conv-all-btn ${listOpen ? "primary" : ""}`}
          aria-expanded={listOpen}
          aria-haspopup="dialog"
          onClick={toggleList}
        >
          {totalConvs > 0 ? t("bots.allConvsCount", { count: totalConvs }) : t("bots.allConvs")}
        </button>
        <button className="btn sm conv-new" disabled={cur === "new" || !current} onClick={() => (choose("new"), setListOpen(false), setState({ kind: "idle" }))}>
          <Icon name="plus" className="sm" />
          {t("conv.new")}
        </button>
      </div>
      {flyout.shown && (
        <div ref={flyoutRef} className={`conv-flyout ${flyout.leaving ? "leaving" : ""}`} inert={flyout.leaving} role="dialog" aria-label={t("bots.allConvs")} onKeyDown={onFlyoutKey}>
          <div className="conv-flyout-h">
            <span>{totalConvs > 0 ? t("bots.allConvsCount", { count: totalConvs }) : t("bots.allConvs")}</span>
            <button className="tb" aria-label={t("bots.closeList")} onClick={closeList}>
              <Icon name="x" className="sm" />
            </button>
          </div>
          <div role="list" aria-label={t("conv.list")}>
            {convs.open.length === 0 && convs.archived.length === 0 && <div className="bot-empty">{t("bots.noWork", { name: b.name })}</div>}
            {convs.open.map((s) => row(s))}
            {convs.archived.length > 0 && (
              <button className="conv-group" aria-expanded={showArchived} onClick={() => setShowArchived(!showArchived)}>
                {t("conv.archivedGroup", { count: convs.archived.length })}
                <Icon name="caret" className={`sm ${showArchived ? "" : "shut"}`} />
              </button>
            )}
            {showArchived && convs.archived.map((s) => row(s, true))}
            {convs.others.length > 0 && <div className="conv-group static">{t("conv.others")}</div>}
            {convs.others.map((s) => <WorkRow key={s.sessionId} s={s} />)}
            <OwnSessions b={b} m={m} />
          </div>
        </div>
      )}
      <div className="bot-stream" role="list" aria-label={t("conv.list")}>
        {current ? (
          <>
            <div className="conv-current-card" role="listitem" aria-current="true">
              <div className="conv-current-top">
                <StatusDot s={current} />
                <b>{displayTitle(current)}</b>
                {!curArchived && <span className="conv-badge">{t("conv.current")}</span>}
              </div>
              <div className="conv-current-meta">
                <span><StateText s={current} /></span>
                <span>· {listTime(updatedDate(current), now)}</span>
                {current.projectId && <span>· {current.projectId}</span>}
                {stuck && stuck !== "archived" && <em className="conv-stuck">· {t("conv.readOnly")}</em>}
              </div>
              {excerpt && <p className="conv-current-excerpt">{excerpt}</p>}
              <div className="conv-current-acts">
                <button className="btn sm primary" onClick={() => st.select(current.sessionId)}>
                  <Icon name="expand" className="sm" />
                  {t("conv.open")}
                </button>
                {/* Unarchiving is where the owner meets it, under the composer (工单 #137 P13): not here too. */}
                {!curArchived && (
                  <button className="btn sm" onClick={() => void archive(current, true)}>
                    {t("conv.archive")}
                  </button>
                )}
              </div>
            </div>
            {recentOthers.length > 0 && (
              <div className="conv-recent">
                <div className="conv-recent-h">{t("conv.recentSwitch")}</div>
                {recentOthers.map((s) => row(s))}
              </div>
            )}
            {totalConvs > 1 && countDetail && (
              <div className="conv-count-hint">{t("conv.countHint", { shown: shownCount, total: totalConvs, detail: countDetail })}</div>
            )}
          </>
        ) : recentOthers.length > 0 ? (
          <div className="conv-recent">
            <div className="conv-recent-h">{t("conv.recentSwitch")}</div>
            {recentOthers.map((s) => row(s))}
            {totalConvs > 1 && countDetail && (
              <div className="conv-count-hint">{t("conv.countHint", { shown: shownCount, total: totalConvs, detail: countDetail })}</div>
            )}
          </div>
        ) : (
          <div className="bot-empty">{t("bots.noWork", { name: b.name })}</div>
        )}
      </div>
      <Composer
        value={text}
        onChange={(v) => (setText(v), state.kind !== "sending" && state.kind !== "idle" && setState({ kind: "idle" }))}
        onSend={() => void send()}
        placeholder={t("composer.placeholder", { name: b.name })}
        state={state}
        disabled={!usable || projects.length === 0 || (!!stuck && stuck !== "archived")}
        why={
          stuck && stuck !== "archived" && current
            ? { ...why!, action: { label: t("conv.newWithContext"), run: () => void send({ withContextOf: current }) } }
            : why
        }
        chips={chips}
        attach={attach}
        commands={[
          { id: "plan", name: "plan", label: t("composer.cmd.plan"), run: () => setText("!plan ") },
          { id: "implement", name: "implement", label: t("composer.cmd.implement"), run: () => setText("!implement ") },
          ...(current ? [{ id: "new", name: "new", label: t("composer.cmd.new"), run: () => choose("new") }, { id: "open", name: "open", label: t("composer.cmd.open"), run: () => st.select(current.sessionId) }] : []),
        ]}
        plus={[
          { id: "new", label: t("conv.new"), icon: "plus", run: () => choose("new"), disabled: !current },
          ...(current ? [{ id: "open", label: t("conv.open"), icon: "expand", run: () => st.select(current.sessionId) }] : []),
        ]}
      />
    </main>
  );
}

/**
 * What became of the owner's message for a member, in words (工单 #71: sent → it took it → it answered); still not
 * taken a day on, that it may not arrive (工单 #160).
 */
export function deliveryWords(t: TFunction<"common">, state: RoomDeliveryState, name: string, reason: string, reminded = false, untaken = false): { text: string; bad: boolean } {
  switch (state) {
    case RoomDeliveryState.SENT:
      if (untaken) return { text: t("bots.delivery.untaken", { name }), bad: true };
      return { text: reminded ? t("bots.delivery.quiet", { name }) : t("bots.delivery.sent", { name }), bad: reminded };
    case RoomDeliveryState.TAKEN:
      return { text: reminded ? t("bots.delivery.quiet", { name }) : t("bots.delivery.taken", { name }), bad: reminded };
    case RoomDeliveryState.QUEUED:
      return { text: t("bots.delivery.queued", { name }), bad: false };
    case RoomDeliveryState.REPLIED:
      return { text: t("bots.delivery.replied", { name }), bad: false };
    case RoomDeliveryState.REFUSED:
    case RoomDeliveryState.FAILED:
    case RoomDeliveryState.SKIPPED: // 工单 #159: not answered in time at a round table, the reason says how long
      return { text: reason, bad: true };
  }
  return { text: "", bad: false };
}

/**
 * The owner's conversation with a personal agent (ROOMS-DESIGN-2026-11-22, G2-a): its direct room, kept on a machine
 * of the owner's (the host). The owner writes; the host sends it the words (unless the owner's information boundary
 * keeps them from its kind) and puts its answers here. Each of the owner's messages says what became of it.
 */
function DirectChat({ b }: { b: Bot }) {
  const st = useStore();
  const { t } = useTranslation("common");
  const [room, setRoom] = useState<Room>();
  const [msgs, setMsgs] = useState<RoomMessage[]>([]);
  // The room as last read with its messages: its member's name as its connection's now (工单 #101).
  const [fresh, setFresh] = useState<Room>();
  const [error, setError] = useState("");
  const [text, setText] = useState("");
  const [light, setLight] = useState(false);
  const [creating, setCreating] = useState(false);
  const [state, setState] = useState<ComposerState>({ kind: "idle" });
  const [key, setKey] = useState(() => crypto.randomUUID());
  const clientId = b.client!.clientId;
  // All direct conversations for this bot (newest first) so old rooms stay reviewable (工单 #123).
  const directRooms = useMemo(
    () =>
      st.machines
        .flatMap((m) => m.status?.rooms ?? [])
        .filter((r) => r.direct && !r.shadow && r.members.some((mem) => mem.address === clientId))
        .sort((x, y) => Number(y.updatedAt?.seconds ?? 0n) - Number(x.updatedAt?.seconds ?? 0n)),
    [st.machines, clientId],
  );
  // Read-only agent-to-agent shadow conversations involving this bot (工单 #124).
  const peerShadows = useMemo(
    () =>
      st.machines
        .flatMap((m) => m.status?.rooms ?? [])
        .filter((r) => r.shadow && r.members.some((mem) => mem.botKey === b.key))
        .sort((x, y) => Number(y.updatedAt?.seconds ?? 0n) - Number(x.updatedAt?.seconds ?? 0n)),
    [st.machines, b.key],
  );
  useEffect(() => {
    let gone = false;
    local.openDirectRoom({ clientId }).then((r) => {
      if (gone || !r.room) return;
      setRoom(r.room);
      setLight(r.room.mode === RoomMode.LIGHT);
      setError("");
    }, (e) => !gone && setError(errorText(e)));
    return () => void (gone = true);
  }, [clientId]);
  // Its answers come in on the host: read again every few seconds while the page is open.
  useEffect(() => {
    if (!room) return;
    let gone = false;
    const read = () => local.listRoomMessages({ machineId: room.hostMachineId, roomId: room.roomId, limit: 200 }).then((r) => !gone && (setMsgs(r.messages), setFresh(r.room)), () => {});
    read();
    const every = setInterval(read, 3000);
    return () => ((gone = true), clearInterval(every));
  }, [room]);
  const startNewChat = async () => {
    if (creating) return;
    setCreating(true);
    try {
      const stamp = new Date().toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
      const created = await local.createRoom({
        name: `${b.name} · ${stamp}`,
        members: [{ botKey: b.key, name: b.name }],
        mode: light ? RoomMode.LIGHT : RoomMode.MANUAL,
        maxRounds: 0,
      });
      setRoom(created);
      setFresh(created);
      setMsgs([]);
      setText("");
      setKey(crypto.randomUUID());
      setError("");
    } catch (err) {
      setError(errorText(err));
    } finally {
      setCreating(false);
    }
  };
  const toggleMode = (nextLight: boolean) => {
    const was = light;
    setLight(nextLight);
    if (room) {
      // Not switched: why is said by the composer, and the mode shown goes back (审计 I).
      local.updateRoom({ machineId: room.hostMachineId, room: { roomId: room.roomId, mode: nextLight ? RoomMode.LIGHT : RoomMode.MANUAL } }).then(setRoom, (e) => {
        setLight(was);
        setState({ kind: "error", text: errorText(e) });
      });
    }
  };
  const send = async () => {
    if (!room || !text.trim() || state.kind === "sending") return;
    setState({ kind: "sending" });
    try {
      const m = await local.postRoomMessage({ machineId: room.hostMachineId, roomId: room.roomId, text, idempotencyKey: key, lightChat: light });
      setMsgs((x) => [...x.filter((y) => y.messageId !== m.messageId), m]);
      setText("");
      setKey(crypto.randomUUID());
      setState({ kind: "idle" });
    } catch (err) {
      setState({ kind: "error", text: errorText(err), retry: () => void send() });
    }
  };
  // A message that reached nobody, sent again as it was (工单 #139 P02): a new message, the failed one kept as it went.
  const resend = async (m: RoomMessage) => {
    if (!room || state.kind === "sending") return;
    setState({ kind: "sending" });
    try {
      const again = await local.postRoomMessage({ machineId: room.hostMachineId, roomId: room.roomId, text: m.text, idempotencyKey: crypto.randomUUID(), lightChat: m.lightChat });
      setMsgs((x) => [...x.filter((y) => y.messageId !== again.messageId), again]);
      setState({ kind: "idle" });
    } catch (err) {
      setState({ kind: "error", text: errorText(err), retry: () => void resend(m) });
    }
  };
  const activeRoom = fresh ?? room;
  // A message can't go to it now (its inbox not set up, 工单 #161): said over the box before one is written, not found
  // out by sending.
  const cant = b.state.usable === "no" ? { text: b.state.line, tone: "warn" as const } : undefined;
  return (
    <main className="bot-main bot-chat">
      <div className="bot-chat-bar">
        {directRooms.length > 1 && activeRoom ? (
          <Picker
            size="sm"
            label={t("rooms.historyChats")}
            value={activeRoom.roomId}
            onChange={(id) => {
              const picked = directRooms.find((x) => x.roomId === id);
              if (picked) {
                setRoom(picked);
                setFresh(picked);
                setLight(picked.mode === RoomMode.LIGHT);
                setState({ kind: "idle" });
              }
            }}
            options={directRooms.map((r) => ({ value: r.roomId, label: r.name }))}
          />
        ) : (
          <span className="bot-chat-title">{activeRoom?.name ?? b.name}</span>
        )}
        <div className="bot-chat-acts">
          <div className="seg" role="group" aria-label={t("rooms.chatMode")}>
            {/* What each mode does, said where it is chosen (工单 #140 P27). */}
            <button type="button" className={!light ? "on" : ""} aria-pressed={!light} title={t("rooms.taskModeHint")} onClick={() => toggleMode(false)}>{t("rooms.taskMode")}</button>
            <button type="button" className={light ? "on" : ""} aria-pressed={light} title={t("rooms.lightHint")} onClick={() => toggleMode(true)}>{t("rooms.light")}</button>
          </div>
          <button type="button" className="btn sm" disabled={creating} onClick={() => void startNewChat()}>
            <Icon name="plus" className="sm" />
            {t("rooms.newChat")}
          </button>
        </div>
      </div>
      {peerShadows.length > 0 && (
        <div className="bot-peer-strip" role="region" aria-label={t("rooms.peerChats")}>
          <span className="muted">{t("rooms.peerChats")}：</span>
          {peerShadows.map((sr) => (
            <button key={sr.roomId} type="button" className="chip" onClick={() => st.set({ selection: { kind: "room", machineId: sr.hostMachineId, roomId: sr.roomId } })}>
              {sr.name} · {t("rooms.shadowBadge")}
            </button>
          ))}
        </div>
      )}
      <div className="bot-stream chat-log" role="log" aria-label={t("bots.chat")}>
        {error && <div className="bot-empty warn" role="alert">{error}</div>}
        {!error && activeRoom && msgs.length === 0 && <div className="bot-empty">{t("bots.chatEmpty", { name: b.name })}</div>}
        {activeRoom && <RoomTimeline room={activeRoom} msgs={msgs} resend={(m) => void resend(m)} />}
      </div>
      <p className="bot-mode-note">{t(light ? "rooms.lightHint" : "rooms.taskModeHint")}</p>
      <Composer value={text} onChange={setText} onSend={() => void send()} placeholder={t("composer.placeholder", { name: b.name })} state={state} disabled={!room || !!cant} why={cant} />
    </main>
  );
}
