import { useEffect, useMemo, useState, type MouseEvent as ReactMouseEvent } from "react";
import type { Persona } from "@aide/proto/identity";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { SessionStatus, type Session } from "@aide/proto/session";
import type { Machine } from "@aide/proto/local";
import { RoomDeliveryState, type Room, type RoomMessage } from "@aide/proto/machine";
import { RoomTimeline, SidebarRooms } from "./Rooms";
import {
  allBots, botConversations, changedParams, currentConversation, displayTitle, dshAgent, listTime, modeChoice, modeLabel, paramsChanged, paramsOf, projectTitle, roster, stuckWhy, updatedDate, whileReconnecting, withContext,
  type Bot, type ConvParams, type Roster,
} from "@aide/core";
import { botLook, botMood, botSeed, personaLook, reactMs, type BotMood } from "@aide/core/avatar";
import { AgentPage, projectsFor } from "../AgentList";
import { local } from "../api";
import { connDot } from "../Connections";
import { Face, grantApp, StateText, StatusDot } from "../components";
import { Icon } from "../icons";
import { Picker, type PickerOption } from "../Picker";
import { GrantDetail } from "../settings/Grants";
import { LookSheet } from "../settings/LookSheet";
import { blankPersona, lookFields, personasChanged } from "../sheets/Contacts";
import { errorText, useNow, useStore, type Sheet } from "../store";
import { Composer, attachmentsOf, useAttachments, type ComposerState } from "./Composer";
import { OwnSession, OwnSessions } from "./OwnSessions";
import { AgentSettings } from "./AgentSettings";
import "./bots.css";

// The roster (工单 #57, #63, BOTARK-REF 产品结构纲要): a bot is the unit the app is shown in, and the sidebar is bots
// alone — the ones pinned as tiles, then the personal agents, the coding agents (a group per machine, headed by its
// name, 工单 #75), and the rooms. No machine, device state or task list is there: a
// machine is said only through its bots' states, and is managed on the devices page (Settings › Machines). A click
// opens the bot's page: its conversations in the middle with the box to write to it, and its own settings beside.

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
      {dot && <i className={`dotc ${connDot(b.state.state)}`} />}
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

/** A contact's face, on its bot's page: its own look, its bot's state. */
function PersonaFace({ b, p, size }: { b: Bot; p: Persona; size: number }) {
  const seed = botSeed(b);
  const mood = useMood(b);
  return (
    <span className="bav" style={{ width: size, height: size }} aria-hidden="true">
      <Face seed={seed} look={personaLook(p, seed)} mood={mood} size={size} />
    </span>
  );
}

/** Its state as a dot and a word, the same words as everywhere (工单 #56). */
export function BotPill({ b }: { b: Bot }) {
  return (
    <span className="bot-pill" data-state={b.state.state} data-usable={b.state.usable} title={b.state.line}>
      <i className={`dotc ${connDot(b.state.state)}`} />
      {b.state.word}
    </span>
  );
}

/** A bot's right-click menu in the roster: open it, pin it as a tile or take it off, delete it from the roster. */
async function botMenu(e: ReactMouseEvent, b: Bot, pinned: boolean, pin: (key: string, on: boolean) => void, open: () => void, remove: () => void, t: TFunction<"common">) {
  e.preventDefault();
  const id = await window.aide?.popupMenu([
    { id: "open", label: t("bots.open") },
    { separator: true },
    { id: "pin", label: t(pinned ? "bots.unpin" : "bots.pin") },
    { separator: true },
    { id: "remove", label: t("bots.remove.menu") },
  ]);
  if (id === "open") open();
  if (id === "pin") pin(b.key, !pinned);
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
  const all = allBots(r);
  const sel = st.selection.kind === "bot" ? st.selection.key : "";
  const open = (b: Bot) => st.set({ selection: { kind: "bot", key: b.key } });
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
            onContextMenu={(e) => botMenu(e, b, pins.includes(b.key), pin, () => open(b), () => st.set({ sheet: removeSheet(b, st.machines, t) }), t)}
          >
            <BotFace b={b} size={28} dot />
            <span className="bot-t">
              <span className="bot-l1">
                <b>{short ? b.shortName : b.name}</b>
                <span className="bot-word" data-usable={b.state.usable}>{b.state.word}</span>
              </span>
              <span className="bot-l2">{b.doing || b.state.line}</span>
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
              onContextMenu={(e) => botMenu(e, b, true, pin, () => open(b), () => st.set({ sheet: removeSheet(b, st.machines, t) }), t)}
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
    </section>
  );
}

/** A bot's own page (工单 #57, #63): who it is and how it stands at the top; its conversations in the middle; its own settings beside. */
export function BotPage({ botKey, native }: { botKey: string; native?: string }) {
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
  const pinned = pins.includes(b.key);
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
            <span>{b.state.line}</span>
          </div>
        </div>
        <button className={`tb bot-pin ${pinned ? "on" : ""}`} aria-pressed={pinned} title={t(pinned ? "bots.unpin" : "bots.pin")} aria-label={t(pinned ? "bots.unpin" : "bots.pin")} onClick={() => pin(b.key, !pinned)}>
          <Icon name="pin" />
        </button>
      </header>
      <div className="bot-body">
        {coding && native ? <OwnSession b={b} m={m!} id={native} /> : coding ? <Conversations b={b} m={m!} /> : <DirectChat b={b} />}
        <aside className="bot-bench" aria-label={t("bots.bench")}>
          {coding && <LocalBench b={b} m={m!} />}
          {!coding && <HandedOut b={b} />}
          {b.client && (
            <section>
              <div className="bot-sec-h">{t("bots.grant")}</div>
              <GrantDetail c={b.client} />
            </section>
          )}
        </aside>
      </div>
    </div>
  );
}

/** A personal agent's work it handed out (its sessions on the owner's machines). */
function HandedOut({ b }: { b: Bot }) {
  const st = useStore();
  const { t } = useTranslation("common");
  const work = st.sessions.filter((s) => s.status !== SessionStatus.ARCHIVED && s.createdBy === b.client?.clientId).sort((x, y) => updatedDate(y).getTime() - updatedDate(x).getTime());
  return (
    <section>
      <div className="bot-sec-h">{t("bots.handedOut")}</div>
      <div className="fgroup bot-list" role="list" aria-label={t("bots.handedOut")}>
        {work.length === 0 && <div className="frow muted">{t("bots.noWorkPersonal", { name: b.name })}</div>}
        {work.map((s) => <WorkRow key={s.sessionId} s={s} />)}
      </div>
    </section>
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

/**
 * A coding agent's own settings on its page (工单 #63): how it stands in one line (what its machine reports in full,
 * versions, checks and fixes, is on the devices page), its names and faces, its projects and how it runs.
 */
function LocalBench({ b, m }: { b: Bot; m: Machine }) {
  const { t } = useTranslation("common");
  const fix = b.state.usable === "no" && b.state.why !== "paused";
  return (
    <>
      <section>
        <div className="bot-sec-h">{t("bots.connection")}</div>
        <div className="fgroup">
          <div className="frow bot-state-row">
            <span className="conn-state" data-state={b.state.state}>
              <i className={`dotc ${connDot(b.state.state)}`} />
              {b.state.word}
            </span>
            <span className="muted">{b.state.line}</span>
          </div>
          {fix && (
            <button className="frow act" onClick={() => window.aide?.openSettings(`machines/${m.machineId}`)}>
              {t("bots.fixOnDevices")}
              <Icon name="chevR" className="sm chev" />
            </button>
          )}
        </div>
      </section>
      <section aria-label={t("agentSettings.title")}>
        <div className="bot-sec-h">{t("agentSettings.title")}</div>
        <AgentSettings b={b} m={m} />
      </section>
      <Identity b={b} m={m} />
      <section>
        <div className="bot-sec-h">{t("bots.manage")}</div>
        <AgentPage m={m} agent={b.agent} bare />
      </section>
    </>
  );
}

/**
 * Its names and faces (docs/personas.md): the owner can give this agent a name, an avatar and presets, the first one
 * its name and face in the roster. Each is its name, with a pencil to change it (工单 #65); none yet is a short line
 * and a button, no sentence standing in for a name.
 */
function Identity({ b, m }: { b: Bot; m: Machine }) {
  const st = useStore();
  const { t } = useTranslation("common");
  return (
    <section aria-label={t("bots.identity")}>
      <div className="bot-sec-h">{t("bots.identity")}</div>
      <div className="fgroup">
        {b.personas.map((p) => (
          <div key={p.personaId} className="frow bot-persona">
            <PersonaFace b={b} p={p} size={28} />
            <span className="bot-persona-t">
              <b>{p.name}</b>
              <span className="muted">{[p.projectId, p.model, p.effort, p.mode].filter(Boolean).join(" · ")}</span>
            </span>
            <button className="tb" title={t("bots.identityEditTip", { name: p.name })} aria-label={t("bots.identityEditTip", { name: p.name })} onClick={() => st.set({ sheet: { kind: "persona", draft: { ...blankPersona, ...p } } })}>
              <Icon name="edit" />
            </button>
          </div>
        ))}
        <button className="frow act" title={t("bots.identityAddTip")} onClick={() => st.set({ sheet: { kind: "persona", draft: { ...blankPersona, ...(b.personas.length ? {} : lookFields(botLook(b))), machineId: m.machineId, agent: b.agent, name: b.personas.length ? "" : b.name } } })}>
          <Icon name="plus" className="sm" />
          {t(b.personas.length ? "bots.identityAdd" : "bots.identityFirst")}
        </button>
      </div>
    </section>
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
    current ? paramsOf(current) : { projectId: persona?.projectId || projects[0]?.projectId || "", model: persona?.model ?? "", effort: persona?.effort ?? "", mode: persona?.mode ?? "" };
  const [params, setParams] = useState<ConvParams>(base);
  const curId = current?.sessionId ?? cur ?? "";
  useEffect(() => setParams(base()), [curId]); // eslint-disable-line react-hooks/exhaustive-deps
  const [text, setText] = useState("");
  const [key, setKey] = useState(() => crypto.randomUUID());
  const [state, setState] = useState<ComposerState>({ kind: "idle" });
  const attach = useAttachments();
  const [showArchived, setShowArchived] = useState(false);
  const stuck = current ? stuckWhy(current, st.projects, m) : undefined;
  const fresh = !current || paramsChanged(current, params);
  const changed = current ? changedParams(current, params) : [];
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
      () => setState({ kind: "done", text: t(on ? "conv.archived" : "conv.unarchived", { title: displayTitle(s) }) }),
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
          ? { text: t("bots.noProject"), tone: "quiet" as const }
          : permission.above
            ? { text: permission.above, tone: "warn" as const }
            : b.state.usable === "unverified"
              ? { text: t("bots.unverified"), tone: "quiet" as const }
              : current && fresh
                ? { text: t("conv.willStartNew", { what: changed.map((k) => t(`composer.param.${k}`)).join("、") }), tone: "quiet" as const }
                : undefined;
  const models: PickerOption<string>[] = [{ value: "", label: t("composer.defaultModel") }, ...(a?.models ?? []).map((x) => ({ value: x.id, label: x.name || x.id }))];
  const efforts = a?.models.find((x) => x.id === params.model)?.efforts ?? [];
  const modes: PickerOption<string>[] = [{ value: "", label: permission.defaultLabel ?? t("composer.defaultMode") }, ...(a?.modes ?? []).map((x) => ({ value: x.id, label: modeLabel(b.agent, x) }))];
  const chips = (
    <>
      {projects.length > 0 && (
        <Picker chip size="sm" label={t("composer.param.projectId")} value={params.projectId} onChange={(v) => setParams({ ...params, projectId: v })}
          options={projects.map((p) => ({ value: p.projectId, label: projectTitle(p), icon: <Icon name="folder" className="sm" /> }))} />
      )}
      {(a?.models.length ?? 0) > 0 && <Picker chip size="sm" label={t("composer.param.model")} value={params.model} options={models} onChange={(v) => setParams({ ...params, model: v, effort: "" })} />}
      {efforts.length > 0 && (
        <Picker chip size="sm" label={t("composer.param.effort")} value={params.effort} onChange={(v) => setParams({ ...params, effort: v })}
          options={[{ value: "", label: t("composer.defaultEffort") }, ...efforts.map((e) => ({ value: e, label: e }))]} />
      )}
      {(a?.modes.length ?? 0) > 0 && <Picker chip size="sm" label={t(b.agent === dshAgent ? "composer.permission" : "composer.param.mode")} value={params.mode} options={modes} onChange={(v) => setParams({ ...params, mode: v })} />}
    </>
  );
  const row = (s: Session, archived = false) => {
    const isCur = s.sessionId === current?.sessionId;
    const why = stuckWhy(s, st.projects, m);
    return (
      <div key={s.sessionId} className={`conv-row ${isCur ? "cur" : ""}`} role="listitem" aria-current={isCur || undefined}>
        <button className="conv-main" onClick={() => choose(s.sessionId)} title={t("conv.goOnTip")}>
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
        <button className="tb" title={t("conv.open")} aria-label={t("conv.open")} onClick={() => st.select(s.sessionId)}><Icon name="expand" /></button>
        <button className="tb" title={t(archived ? "conv.unarchive" : "conv.archive")} aria-label={t(archived ? "conv.unarchive" : "conv.archive")} onClick={() => void archive(s, !archived)}>
          <Icon name={archived ? "unarchive" : "archive"} />
        </button>
      </div>
    );
  };
  return (
    <main className="bot-main">
      {/* Rule 3: which conversation the next message goes on in. */}
      <div className="conv-head" role="status">
        {current ? (
          <>
            <span className="conv-head-k">{t("conv.currentIs")}</span>
            <button className="link conv-head-t" onClick={() => st.select(current.sessionId)}>{displayTitle(current)}</button>
            <span className="muted">{t("conv.startedAt", { when: listTime(new Date(Number(current.createdAt?.seconds ?? 0n) * 1000), now) })}</span>
          </>
        ) : (
          <span className="conv-head-k">{t(convs.open.length ? "conv.nextIsNew" : "conv.firstIsNew")}</span>
        )}
        <button className="btn sm conv-new" disabled={cur === "new" || !current} onClick={() => (choose("new"), setState({ kind: "idle" }))}>
          <Icon name="plus" className="sm" />
          {t("conv.new")}
        </button>
      </div>
      <div className="bot-stream" role="list" aria-label={t("conv.list")}>
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

/** What became of the owner's message for a member, in words (工单 #71: sent → it took it → it answered). */
export function deliveryWords(t: TFunction<"common">, state: RoomDeliveryState, name: string, reason: string, reminded = false): { text: string; bad: boolean } {
  switch (state) {
    case RoomDeliveryState.SENT:
      return { text: reminded ? t("bots.delivery.quiet", { name }) : t("bots.delivery.sent", { name }), bad: reminded };
    case RoomDeliveryState.TAKEN:
      return { text: reminded ? t("bots.delivery.quiet", { name }) : t("bots.delivery.taken", { name }), bad: reminded };
    case RoomDeliveryState.QUEUED:
      return { text: t("bots.delivery.queued", { name }), bad: false };
    case RoomDeliveryState.REPLIED:
      return { text: t("bots.delivery.replied", { name }), bad: false };
    case RoomDeliveryState.REFUSED:
    case RoomDeliveryState.FAILED:
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
  const { t } = useTranslation("common");
  const [room, setRoom] = useState<Room>();
  const [msgs, setMsgs] = useState<RoomMessage[]>([]);
  // The room as last read with its messages: its member's name as its connection's now (工单 #101).
  const [fresh, setFresh] = useState<Room>();
  const [error, setError] = useState("");
  const [text, setText] = useState("");
  const [state, setState] = useState<ComposerState>({ kind: "idle" });
  const [key, setKey] = useState(() => crypto.randomUUID());
  const clientId = b.client!.clientId;
  useEffect(() => {
    let gone = false;
    local.openDirectRoom({ clientId }).then((r) => !gone && (setRoom(r.room), setError("")), (e) => !gone && setError(errorText(e)));
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
  const send = async () => {
    if (!room || !text.trim() || state.kind === "sending") return;
    setState({ kind: "sending" });
    try {
      const m = await local.postRoomMessage({ machineId: room.hostMachineId, roomId: room.roomId, text, idempotencyKey: key });
      setMsgs((x) => [...x.filter((y) => y.messageId !== m.messageId), m]);
      setText("");
      setKey(crypto.randomUUID());
      setState({ kind: "idle" });
    } catch (err) {
      setState({ kind: "error", text: errorText(err), retry: () => void send() });
    }
  };
  return (
    <main className="bot-main bot-chat">
      <div className="bot-stream chat-log" role="log" aria-label={t("bots.chat")}>
        {error && <div className="bot-empty warn" role="alert">{error}</div>}
        {!error && room && msgs.length === 0 && <div className="bot-empty">{t("bots.chatEmpty", { name: b.name })}</div>}
        {room && <RoomTimeline room={fresh ?? room} msgs={msgs} />}
      </div>
      <Composer value={text} onChange={setText} onSend={() => void send()} placeholder={t("composer.placeholder", { name: b.name })} state={state} disabled={!room} />
    </main>
  );
}
