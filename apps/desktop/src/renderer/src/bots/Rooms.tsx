import { useEffect, useRef, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { allBots, displayTitle, projectTitle, type Bot } from "@aide/core";
import { RoomDeliveryState, RoomMode, RoomRun, type Room, type RoomDelivery, type RoomMessage } from "@aide/proto/machine";
import { projectsFor } from "../AgentList";
import { local } from "../api";
import { StateText } from "../components";
import { Icon } from "../icons";
import { Markdown } from "../session/Markdown";
import { errorText, useStore } from "../store";
import { BotFace, useRoster } from "./Bots";

// Rooms (ROOMS-DESIGN-2026-11-22, G2-b/c): the owner and several bots, kept on a machine of the owner's (the host).
// In the roster beside the bots, each with its last line and what the owner has not read; a room's page has its
// members, how it is run (manual: only those the owner names are woken; round table: bots hand on to each other by
// naming one, up to the round limit, and the owner can stop it), and one timeline: every message with who said it
// and what became of it, a [FINAL] reply sealed as the room's result, to copy or export.

let roomCache: Room[] = [];
const roomSubs = new Set<() => void>();
export function loadRooms() {
  local.listRooms({}).then((r) => ((roomCache = r.rooms.filter((x) => !x.direct)), roomSubs.forEach((f) => f())), () => {});
}

/** The rooms on the owner's machines that answer, read every few seconds while shown. */
export function useRooms(): Room[] {
  const [, tick] = useState(0);
  useEffect(() => {
    const f = () => tick((x) => x + 1);
    roomSubs.add(f);
    loadRooms();
    const every = setInterval(loadRooms, 5000);
    return () => {
      roomSubs.delete(f);
      clearInterval(every);
    };
  }, []);
  return roomCache;
}

/** A room's state in a line, as the roster and its page say it. */
export function roomLine(t: TFunction<"common">, r: Room): string {
  switch (r.run) {
    case RoomRun.RUNNING:
      // A round is a hand-on from bot to bot: none yet while the first ones answer the owner.
      if (r.mode !== RoomMode.ROUNDTABLE) return t("rooms.run.waiting");
      return r.round === 0 ? t("rooms.run.tableStart", { max: r.maxRounds }) : t("rooms.run.table", { round: r.round, max: r.maxRounds });
    case RoomRun.STOPPED_LIMIT:
      return t("rooms.run.limit");
    case RoomRun.STOPPED_OWNER:
      return t("rooms.run.stopped");
  }
  return r.lastLine || t("rooms.run.idle");
}

/** The roster's rooms: one row each, and a button to make one. */
export function SidebarRooms() {
  const st = useStore();
  const { t } = useTranslation("common");
  const rooms = useRooms();
  const bots = allBots(useRoster());
  const key = "bots:rooms";
  const open = !st.collapsed.includes(key);
  const sel = st.selection.kind === "room" ? st.selection.roomId : "";
  return (
    <div role="group" aria-label={t("rooms.title")}>
      <div className="grp-h-row">
        <button className={`grp-h ${open ? "" : "shut"}`} aria-expanded={open} onClick={() => st.set({ collapsed: open ? [...st.collapsed, key] : st.collapsed.filter((c) => c !== key) })}>
          {t("rooms.title")}
          <Icon name="caret" className="sm" />
        </button>
        <button className="tb" title={t("rooms.new")} aria-label={t("rooms.new")} onClick={() => st.set({ sheet: { kind: "newRoom" } })}>
          <Icon name="plus" className="sm" />
        </button>
      </div>
      {open && rooms.length === 0 && <div className="side-none">{t("rooms.none")}</div>}
      {open && rooms.map((r) => (
        <button key={r.roomId} className={`bot-row ${sel === r.roomId ? "sel" : ""}`} aria-current={sel === r.roomId || undefined} onClick={() => st.set({ selection: { kind: "room", machineId: r.hostMachineId, roomId: r.roomId } })}>
          <span className="room-faces" aria-hidden="true">
            {r.members.slice(0, 3).map((m) => {
              const b = bots.find((x) => x.key === m.botKey);
              return b ? <BotFace key={m.botKey} b={b} size={18} /> : <span key={m.botKey} className="av sm">{m.name.slice(0, 1)}</span>;
            })}
          </span>
          <span className="bot-t">
            <span className="bot-l1"><b>{r.name}</b></span>
            <span className="bot-l2">{roomLine(t, r)}</span>
          </span>
          {r.unread > 0 && <span className="cnt-w">{r.unread}</span>}
        </button>
      ))}
    </div>
  );
}

/** What became of the owner's message for a member, in words. */
export function DeliveryLine({ d, name }: { d: RoomDelivery; name: string }) {
  const { t } = useTranslation("common");
  const word = {
    [RoomDeliveryState.SENT]: t("bots.delivery.sent", { name }),
    [RoomDeliveryState.REPLIED]: t("bots.delivery.replied", { name }),
    [RoomDeliveryState.REFUSED]: d.reason,
    [RoomDeliveryState.FAILED]: d.reason,
    [RoomDeliveryState.UNSPECIFIED]: "",
  }[d.state];
  const bad = d.state === RoomDeliveryState.REFUSED || d.state === RoomDeliveryState.FAILED;
  return <span className={`chat-state ${bad ? "warn" : ""}`} data-state={RoomDeliveryState[d.state]}>{word}</span>;
}

/** A sealed [FINAL] reply: the room's agreed result, to copy or export as Markdown. */
function FinalCard({ m, room }: { m: RoomMessage; room: Room }) {
  const st = useStore();
  const { t } = useTranslation("common");
  const body = m.text.replace(/^\s*\[FINAL\]\s*/, "");
  const exportIt = () => {
    const a = document.createElement("a");
    a.href = URL.createObjectURL(new Blob([`# ${room.name}\n\n${body}\n\n— ${m.fromName}\n`], { type: "text/markdown" }));
    a.download = `${room.name}.md`;
    a.click();
    URL.revokeObjectURL(a.href);
  };
  return (
    <div className="room-final" role="article" aria-label={t("rooms.final")}>
      <div className="room-final-h">
        <Icon name="check" className="sm" />
        <b>{t("rooms.final")}</b>
        <span className="muted">{t("rooms.finalFrom", { name: m.fromName })}</span>
      </div>
      <Markdown text={body} />
      <div className="room-final-acts">
        <button className="btn sm" onClick={() => navigator.clipboard.writeText(body).then(() => st.show(t("rooms.copied")))}>{t("rooms.copy")}</button>
        <button className="btn sm" onClick={exportIt}>{t("rooms.export")}</button>
        <span className="muted mono" title={m.finalSha256}>{t("rooms.sealed")}</span>
      </div>
    </div>
  );
}

/**
 * A room's timeline: the owner's messages (with what became of them for each member woken), the members' (by name,
 * their coding agent's session to open), the host's own lines, a [FINAL] reply as its card, and in manual mode a
 * member's naming another as a suggestion the owner can hand on.
 */
export function RoomTimeline({ room, msgs, handOn }: { room: Room; msgs: RoomMessage[]; handOn?: (to: string, m: RoomMessage) => void }) {
  const st = useStore();
  const { t } = useTranslation("common");
  const nameOf = (key: string) => room.members.find((x) => x.botKey === key)?.name ?? key;
  const end = useRef<HTMLDivElement>(null);
  // A block, not an expression: Chromium's scrollIntoView now returns a promise, which React would take for a cleanup.
  useEffect(() => {
    end.current?.scrollIntoView({ block: "end" });
  }, [msgs.length]);
  return (
    <>
      {msgs.map((m) => {
        if (m.system) return <div key={m.messageId} className="room-sys" role="note">{m.text}</div>;
        if (m.final) return <FinalCard key={m.messageId} m={m} room={room} />;
        const mine = m.from === "owner";
        const member = room.members.find((x) => x.botKey === m.from);
        const session = member?.sessionId ? st.sessions.find((s) => s.sessionId === member.sessionId) : undefined;
        return (
          <div key={m.messageId} className={`chat-msg ${mine ? "mine" : ""} ${m.late ? "late" : ""}`}>
            {!mine && (
              <span className="chat-who">
                {m.fromName}
                {m.late && <em>{t("rooms.late")}</em>}
              </span>
            )}
            {mine && m.mentions.length > 0 && <span className="chat-who">{m.mentions.map((k) => "@" + nameOf(k)).join(" ")}</span>}
            <div className="chat-bubble"><Markdown text={m.text} /></div>
            {mine && m.deliveries.map((d) => <DeliveryLine key={d.botKey} d={d} name={nameOf(d.botKey)} />)}
            {!mine && m.deliveries.length > 0 && <span className="chat-state">{t("rooms.handedOn", { names: m.deliveries.map((d) => nameOf(d.botKey)).join("、") })}</span>}
            {!mine && session && (
              <button className="link chat-state" onClick={() => st.select(session.sessionId)}>
                {t("rooms.itsSession")}{displayTitle(session)} · <StateText s={session} />
              </button>
            )}
            {m.asks.map((k) => (
              <span key={k} className="room-ask">
                {t("rooms.asks", { from: m.fromName, to: nameOf(k) })}
                {handOn && <button className="btn sm" onClick={() => handOn(k, m)}>{t("rooms.handOn", { to: nameOf(k) })}</button>}
              </span>
            ))}
          </div>
        );
      })}
      <div ref={end} />
    </>
  );
}

/** A room's page (selection "room"). */
export function RoomPage({ machineId, roomId }: { machineId: string; roomId: string }) {
  const st = useStore();
  const { t } = useTranslation("common");
  const bots = allBots(useRoster());
  const [room, setRoom] = useState<Room>();
  const [msgs, setMsgs] = useState<RoomMessage[]>([]);
  const [error, setError] = useState("");
  const [text, setText] = useState("");
  const [mentions, setMentions] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [key, setKey] = useState(() => crypto.randomUUID());
  const ref = { machineId, roomId };
  const read = () =>
    local.listRoomMessages({ machineId, roomId, limit: 300 }).then(
      (r) => (setRoom(r.room), setMsgs(r.messages), setError("")),
      (e) => setError(errorText(e)),
    );
  useEffect(() => {
    read();
    const every = setInterval(read, 3000);
    return () => clearInterval(every);
  }, [machineId, roomId]);
  // Read up to the newest: what the roster counts as not read goes.
  const last = msgs[msgs.length - 1]?.messageId;
  useEffect(() => {
    if (last && room && room.seenMessageId !== last) local.markRoomSeen(ref).then(loadRooms, () => {});
  }, [last]);
  if (!room) return <div className="bot-page"><div className="bot-empty">{error || t("rooms.loading")}</div></div>;
  const table = room.mode === RoomMode.ROUNDTABLE;
  const update = (patch: { name?: string; mode?: RoomMode; maxRounds?: number }) =>
    local.updateRoom({ machineId, room: { roomId, ...patch } }).then((r) => (setRoom(r), loadRooms()), (e) => st.show(errorText(e)));
  const send = async (e?: FormEvent, override?: { text: string; mentions: string[] }) => {
    e?.preventDefault();
    const body = override?.text ?? text;
    if (!body.trim() || busy) return;
    setBusy(true);
    try {
      const m = await local.postRoomMessage({ machineId, roomId, text: body, mentions: override?.mentions ?? mentions, idempotencyKey: override ? crypto.randomUUID() : key });
      setMsgs((x) => [...x.filter((y) => y.messageId !== m.messageId), m]);
      if (!override) (setText(""), setMentions([]), setKey(crypto.randomUUID()));
      read();
    } catch (err) {
      st.show(errorText(err));
    }
    setBusy(false);
  };
  const woken = mentions.length > 0 ? mentions : table && room.members[0] ? [room.members[0].botKey] : [];
  const names = (keys: string[]) => keys.map((k) => room.members.find((x) => x.botKey === k)?.name ?? k).join("、");
  const host = st.machines.find((m) => m.machineId === room.hostMachineId);
  return (
    <div className="bot-page room-page" role="region" aria-label={room.name}>
      <header className="bot-head drag">
        <div className="bot-head-t">
          <div className="bot-head-l1">
            <h1>{room.name}</h1>
            <button className="link" onClick={() => { const n = prompt(t("rooms.rename"), room.name); if (n?.trim()) update({ name: n.trim() }); }}>{t("rooms.renameShort")}</button>
            <span className="bot-pill" data-usable={room.run === RoomRun.RUNNING ? "yes" : ""}>{roomLine(t, room)}</span>
          </div>
          <div className="room-members" role="list" aria-label={t("rooms.members")}>
            {room.members.map((m) => {
              const b = bots.find((x) => x.key === m.botKey);
              return (
                <span key={m.botKey} className="room-member" role="listitem" title={b?.state.line}>
                  {b ? <BotFace b={b} size={20} /> : <span className="av sm">{m.name.slice(0, 1)}</span>}
                  {m.name}
                  {b && <em data-usable={b.state.usable}>{b.state.word}</em>}
                </span>
              );
            })}
          </div>
        </div>
        <div className="room-ctl">
          <div className="seg" role="group" aria-label={t("rooms.mode")}>
            <button className={!table ? "on" : ""} aria-pressed={!table} onClick={() => update({ mode: RoomMode.MANUAL })}>{t("rooms.manual")}</button>
            <button className={table ? "on" : ""} aria-pressed={table} onClick={() => update({ mode: RoomMode.ROUNDTABLE })}>{t("rooms.table")}</button>
          </div>
          {table && (
            <label className="muted">
              {t("rooms.maxRounds")}
              <select className="field sm" value={room.maxRounds} aria-label={t("rooms.maxRounds")} onChange={(e) => update({ maxRounds: Number(e.target.value) })}>
                {Array.from({ length: 20 }, (_, i) => i + 1).map((x) => <option key={x} value={x}>{x}</option>)}
              </select>
            </label>
          )}
          {room.run === RoomRun.RUNNING && (
            <button className="btn sm" onClick={() => local.stopRoom(ref).then((r) => (setRoom(r), read(), loadRooms()), (e) => st.show(errorText(e)))}>{t("rooms.stop")}</button>
          )}
        </div>
      </header>
      <main className="bot-main bot-chat">
        <div className="bot-sec-h">
          {t("rooms.timeline")}
          {host && <span className="muted">{t("bots.chatHost", { machine: host.machineName })}</span>}
        </div>
        <div className="bot-stream chat-log" role="log" aria-label={t("rooms.timeline")}>
          {msgs.length === 0 && <div className="bot-empty">{t(table ? "rooms.emptyTable" : "rooms.emptyManual")}</div>}
          <RoomTimeline room={room} msgs={msgs} handOn={(to, m) => send(undefined, { text: t("rooms.handOnText", { from: m.fromName, text: m.text }), mentions: [to] })} />
        </div>
        <form className="bot-compose" onSubmit={send}>
          <div className="room-mentions" role="group" aria-label={t("rooms.mention")}>
            {room.members.map((m) => {
              const on = mentions.includes(m.botKey);
              return (
                <button type="button" key={m.botKey} className={`chip ${on ? "on" : ""}`} aria-pressed={on} onClick={() => setMentions(on ? mentions.filter((k) => k !== m.botKey) : [...mentions, m.botKey])}>
                  @{m.name}
                </button>
              );
            })}
          </div>
          <textarea className="field" rows={3} value={text} placeholder={t("rooms.placeholder")} aria-label={t("rooms.placeholder")}
            onChange={(e) => setText(e.target.value)} onKeyDown={(e) => e.key === "Enter" && (e.metaKey || e.ctrlKey) && void send(e)} />
          <div className="bot-compose-bar">
            <span className="muted" role="status">{woken.length ? t("rooms.wakes", { names: names(woken) }) : t("rooms.wakesNone")}</span>
            <button type="submit" className="btn primary sm" disabled={!text.trim() || busy}>{busy ? t("bots.sending") : t("bots.chatSend")}</button>
          </div>
        </form>
      </main>
    </div>
  );
}

/** Making a room: its name, the bots in it (a coding agent with the project it works in), how it is run. */
export function NewRoomSheet() {
  const st = useStore();
  const { t } = useTranslation("common");
  const bots = allBots(useRoster()).filter((b) => b.kind === "personal" || (b.agent && b.key.startsWith("m:")));
  const [name, setName] = useState("");
  const [picked, setPicked] = useState<Record<string, string>>({}); // bot key → project ("" for a personal agent)
  const [mode, setMode] = useState(RoomMode.MANUAL);
  const [maxRounds, setMaxRounds] = useState(6);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const projectsOf = (b: Bot) => projectsFor(st.projects.filter((p) => p.machineId === b.machineId), b.agent);
  const toggle = (b: Bot) => {
    const next = { ...picked };
    if (b.key in next) delete next[b.key];
    else next[b.key] = b.kind === "personal" ? "" : (projectsOf(b)[0]?.projectId ?? "");
    setPicked(next);
  };
  const keys = Object.keys(picked);
  const ready = name.trim() !== "" && keys.length > 0 && keys.every((k) => k.startsWith("c:") || picked[k] !== "");
  const create = async (e: FormEvent) => {
    e.preventDefault();
    if (!ready || busy) return;
    setBusy(true);
    setError("");
    try {
      const r = await local.createRoom({
        name: name.trim(), mode, maxRounds,
        members: keys.map((k) => ({ botKey: k, projectId: picked[k], name: bots.find((b) => b.key === k)?.name ?? "" })),
      });
      loadRooms();
      st.set({ sheet: null, selection: { kind: "room", machineId: r.hostMachineId, roomId: r.roomId } });
    } catch (err) {
      setError(errorText(err));
    }
    setBusy(false);
  };
  return (
    <form onSubmit={create}>
      <h3 id="sheet-title">{t("rooms.new")}</h3>
      <p className="sub">{t("rooms.newSub")}</p>
      <div className="fgroup">
        <div className="frow">
          <label className="fl" htmlFor="room-name">{t("rooms.name")}</label>
          <span className="fv"><input id="room-name" className="field" style={{ width: 260 }} maxLength={64} value={name} onChange={(e) => setName(e.target.value)} /></span>
        </div>
        <div className="frow">
          <span className="fl">{t("rooms.mode")}</span>
          <span className="fv room-mode">
            <div className="seg" role="group" aria-label={t("rooms.mode")}>
              <button type="button" className={mode === RoomMode.MANUAL ? "on" : ""} aria-pressed={mode === RoomMode.MANUAL} onClick={() => setMode(RoomMode.MANUAL)}>{t("rooms.manual")}</button>
              <button type="button" className={mode === RoomMode.ROUNDTABLE ? "on" : ""} aria-pressed={mode === RoomMode.ROUNDTABLE} onClick={() => setMode(RoomMode.ROUNDTABLE)}>{t("rooms.table")}</button>
            </div>
            {mode === RoomMode.ROUNDTABLE && (
              <select className="field sm room-rounds" value={maxRounds} aria-label={t("rooms.maxRounds")} onChange={(e) => setMaxRounds(Number(e.target.value))}>
                {Array.from({ length: 20 }, (_, i) => i + 1).map((x) => <option key={x} value={x}>{t("rooms.roundsN", { count: x })}</option>)}
              </select>
            )}
          </span>
        </div>
      </div>
      <p className="sheet-note muted">{t(mode === RoomMode.ROUNDTABLE ? "rooms.tableNote" : "rooms.manualNote")}</p>
      <div className="fhead">{t("rooms.pick")}</div>
      <div className="room-pick" role="group" aria-label={t("rooms.pick")}>
        {bots.map((b) => {
          const on = b.key in picked;
          const projects = b.kind === "personal" ? [] : projectsOf(b);
          return (
            <div key={b.key} className={`room-pick-row ${on ? "on" : ""}`}>
              <label>
                <input type="checkbox" checked={on} onChange={() => toggle(b)} aria-label={b.name} />
                <BotFace b={b} size={22} />
                <b>{b.name}</b>
                <span className="muted">{b.state.word}</span>
              </label>
              {on && b.kind !== "personal" && (
                projects.length ? (
                  <select className="field sm" value={picked[b.key]} aria-label={t("rooms.projectOf", { name: b.name })} onChange={(e) => setPicked({ ...picked, [b.key]: e.target.value })}>
                    {projects.map((p) => <option key={p.projectId} value={p.projectId}>{projectTitle(p)}</option>)}
                  </select>
                ) : <span className="warn">{t("rooms.noProject")}</span>
              )}
            </div>
          );
        })}
      </div>
      {error && <p className="err" role="alert">{error}</p>}
      <div className="sheet-foot">
        <button type="button" className="btn" onClick={() => st.set({ sheet: null })}>{t("rooms.cancel")}</button>
        <button type="submit" className="btn primary" disabled={!ready || busy}>{busy ? t("rooms.creating") : t("rooms.create")}</button>
      </div>
    </form>
  );
}
