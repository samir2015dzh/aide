import { Fragment, useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { agoText, allBots, dateOf, displayTitle, listTime, projectTitle, type Bot } from "@aide/core";
import { RoomDeliveryState, RoomMode, RoomRun, type Room, type RoomDelivery, type RoomMessage } from "@aide/proto/machine";
import { projectsFor } from "../AgentList";
import { local } from "../api";
import { StateText } from "../components";
import { Icon } from "../icons";
import { TimeLine } from "../session/Chat";
import { Markdown } from "../session/Markdown";
import { Check, Picker } from "../Picker";
import { errorText, useNow, useStore, type Store } from "../store";
import { BotFace, deliveryWords, useRoster } from "./Bots";
import { Composer, type ComposerState } from "./Composer";

// Rooms (ROOMS-DESIGN-2026-11-22, G2-b/c): the owner and several bots, kept on a machine of the owner's (the host).
// In the roster beside the bots, each with its last line and what the owner has not read; a room's page has its
// members, how it is run (manual: only those the owner names are woken; round table: bots hand on to each other by
// naming one, up to the round limit, and the owner can stop it), and one timeline: every message with who said it
// and what became of it, a [FINAL] reply sealed as the room's result, to copy or export.

/**
 * The rooms on the owner's machines, from their status on the Watch the app has (each machine puts the rooms it
 * hosts there), newest first: nothing is asked for while the window is idle.
 */
export function useRooms(): Room[] {
  const st = useStore();
  return useMemo(
    () =>
      st.machines
        .flatMap((m) => m.status?.rooms ?? [])
        .filter((r) => !r.direct)
        .sort((x, y) => Number(y.updatedAt?.seconds ?? 0n) - Number(x.updatedAt?.seconds ?? 0n)),
    [st.machines],
  );
}

/**
 * Replies for a room that wait at the post office while its host is away (工单 #74): what the post office counts for
 * the room (every member's, a personal agent's too: their envelopes name the room), or, from a relay that does not
 * count them yet, what the other machines say they sent it; and since when.
 */
export function waitingReplies(st: Pick<Store, "machines">, roomId: string, post?: ReadonlyMap<string, { count: number; since?: Date }>): { count: number; since?: Date } {
  let count = 0;
  let since: Date | undefined;
  for (const m of st.machines) {
    for (const o of m.status?.roomOutbox ?? []) {
      if (o.roomId !== roomId) continue;
      count += o.replies;
      const at = dateOf(o.since);
      if (at && (!since || at < since)) since = at;
    }
  }
  const p = post?.get(roomId);
  return p && p.count >= count ? p : { count, since };
}

/** What waits at the post office for these hosts (while they are away), by room; read again every 15 s. */
export function usePostWaiting(hosts: string[]): ReadonlyMap<string, { count: number; since?: Date }> {
  const [by, setBy] = useState<ReadonlyMap<string, { count: number; since?: Date }>>(new Map());
  const key = [...new Set(hosts)].sort().join(",");
  useEffect(() => {
    if (!key) return setBy(new Map());
    let gone = false;
    const read = () =>
      Promise.all(key.split(",").map((machineId) => local.listRoomWaiting({ machineId }).then((r) => r.rooms, () => []))).then((all) => {
        if (gone) return;
        const m = new Map<string, { count: number; since?: Date }>();
        for (const r of all.flat()) m.set(r.roomId, { count: r.messages, since: dateOf(r.since) });
        setBy(m);
      });
    read();
    const every = setInterval(read, 15_000);
    return () => ((gone = true), clearInterval(every));
  }, [key]);
  return by;
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
  const away = rooms.map((r) => r.hostMachineId).filter((id) => st.machines.some((m) => m.machineId === id && !m.online && !m.local));
  const post = usePostWaiting(away);
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
              return b ? <BotFace key={m.botKey} b={b} size={18} still /> : <span key={m.botKey} className="bav" style={{ width: 18, height: 18 }}><span className="bav-in" style={{ background: "#8E8E93" }} /></span>;
            })}
          </span>
          <span className="bot-t">
            <span className="bot-l1">
              <b>{r.name}</b>
              {r.shadow && <span className="room-ro-tag">{t("rooms.shadowBadge")}</span>}
            </span>
            <span className="bot-l2">{((w) => (w.count ? t("rooms.waitingShort", { count: w.count }) : roomLine(t, r)))(waitingReplies(st, r.roomId, post))}</span>
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
  const w = deliveryWords(t, d.state, name, d.reason, d.reminded);
  const failed = d.state === RoomDeliveryState.FAILED || d.state === RoomDeliveryState.REFUSED;
  return (
    <span className={`chat-state ${w.bad ? "warn" : ""}`} data-state={d.state}>
      {failed && <Icon name="alert" className="sm" />}
      {w.text}
    </span>
  );
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
 * member's naming another as a suggestion the owner can hand on. For read-only shadow conversations (#124), renders
 * sender -> recipient, timestamp, optional subject, and reply reference.
 */
export function RoomTimeline({ room, msgs, handOn, resend }: { room: Room; msgs: RoomMessage[]; handOn?: (to: string, m: RoomMessage) => void; resend?: (m: RoomMessage) => void }) {
  const st = useStore();
  const { t } = useTranslation("common");
  const now = useNow(30_000);
  const nameOf = (key: string) => room.members.find((x) => x.botKey === key)?.name ?? key;
  const byId = useMemo(() => new Map(msgs.map((x) => [x.messageId, x])), [msgs]);
  const end = useRef<HTMLDivElement>(null);
  // A block, not an expression: Chromium's scrollIntoView now returns a promise, which React would take for a cleanup.
  useEffect(() => {
    end.current?.scrollIntoView({ block: "end" });
  }, [msgs.length]);
  let prevAt: Date | undefined;
  return (
    <div className="chat-col">
      {msgs.map((m) => {
        const atDate = dateOf(m.at);
        if (m.system) return <div key={m.messageId} className="room-sys" role="note">{m.text}</div>;
        const previous = prevAt;
        if (atDate) prevAt = atDate;
        if (m.final) {
          return (
            <Fragment key={m.messageId}>
              <TimeLine at={atDate} previous={previous} />
              <FinalCard m={m} room={room} />
            </Fragment>
          );
        }
        const mine = !room.shadow && m.from === "owner";
        const failed = mine && m.deliveries.length > 0 && m.deliveries.every((d) => d.state === RoomDeliveryState.FAILED || d.state === RoomDeliveryState.REFUSED);
        const member = room.members.find((x) => x.botKey === m.from);
        const session = member?.sessionId ? st.sessions.find((s) => s.sessionId === member.sessionId) : undefined;
        const replyTarget = m.inReplyTo ? byId.get(m.inReplyTo) : undefined;
        // 轻聊 on both sides of a light exchange — the owner's message and the answer to it — not on some only (工单 #140 P27).
        const light = m.lightChat || (!!m.inReplyTo && !!byId.get(m.inReplyTo)?.lightChat);
        // One of the owner's that reached nobody it was for is marked failed above, and sent again from here (工单 #139 P02).
        const resendable = mine && !!resend && m.deliveries.some((d) => d.state === RoomDeliveryState.FAILED);
        return (
          <Fragment key={m.messageId}>
            <TimeLine at={atDate} previous={previous} />
            <div className={`chat-msg ${mine ? "mine" : ""} ${failed ? "failed" : ""} ${room.shadow ? "shadow" : ""} ${m.late ? "late" : ""}`}>
              {!mine && (
                <div className="chat-meta">
                  <span className="chat-who">
                    {room.shadow && m.toName ? `${m.fromName} → ${m.toName}` : m.fromName}
                    {room.shadow && atDate && <em>· {agoText(atDate, now)}</em>}
                    {m.late && <em>{t("rooms.late")}</em>}
                    {light && <span className="room-ro-tag">{t("rooms.light")}</span>}
                  </span>
                  {!room.shadow && atDate && <span className="chat-time">{listTime(atDate, now)}</span>}
                </div>
              )}
              {mine && (m.mentions.length > 0 || light || atDate) && (
                <div className="chat-meta">
                  {(m.mentions.length > 0 || light) && (
                    <span className="chat-who">
                      {light && <span className="room-ro-tag">{t("rooms.light")}</span>}
                      {m.mentions.map((k) => "@" + nameOf(k)).join(" ")}
                    </span>
                  )}
                  {atDate && <span className="chat-time">{listTime(atDate, now)}</span>}
                </div>
              )}
              <div className="chat-bubble">
                {room.shadow && m.subject && <div className="shadow-subj">{m.subject}</div>}
                {room.shadow && m.inReplyTo && (
                  <div className="shadow-reply">
                    {t("rooms.shadowReplyTo")} {replyTarget ? `${replyTarget.fromName}：${replyTarget.text.slice(0, 48)}` : m.inReplyTo}
                  </div>
                )}
                <Markdown text={m.text} />
              </div>
              {mine && m.deliveries.map((d) => <DeliveryLine key={d.botKey} d={d} name={nameOf(d.botKey)} />)}
              {resendable && (
                <button className="link chat-state" onClick={() => resend!(m)}>
                  <Icon name="retry" className="sm" />
                  {t("rooms.resend")}
                </button>
              )}
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
          </Fragment>
        );
      })}
      <div ref={end} />
    </div>
  );
}

/** A room's page (selection "room"). */
export function RoomPage({ machineId, roomId }: { machineId: string; roomId: string }) {
  const st = useStore();
  const { t } = useTranslation("common");
  const now = useNow(30_000);
  const bots = allBots(useRoster());
  const [room, setRoom] = useState<Room>();
  const [msgs, setMsgs] = useState<RoomMessage[]>([]);
  const [error, setError] = useState("");
  const [text, setText] = useState("");
  const [mentions, setMentions] = useState<string[]>([]);
  const [state, setState] = useState<ComposerState>({ kind: "idle" });
  const [key, setKey] = useState(() => crypto.randomUUID());
  const [renaming, setRenaming] = useState<string | null>(null);
  const ref = { machineId, roomId };
  // What was read last stays shown while the host can't be read (工单 #74): the room is not gone, its host is away.
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
    if (last && room && room.seenMessageId !== last) local.markRoomSeen(ref).catch(() => {});
  }, [last]);
  // Tells the app's notifications this room is in front (工单 #69: no notification for what is read here).
  useEffect(() => {
    window.aide?.roomOpen?.(roomId);
    return () => window.aide?.roomOpen?.("");
  }, [roomId]);
  const host = st.machines.find((m) => m.machineId === machineId);
  const away = !!host && !host.online && !host.local;
  const post = usePostWaiting(away || error ? [machineId] : []);
  const waiting = waitingReplies(st, roomId, post);
  const awayBanner = (away || (error && waiting.count > 0)) && (
    <div className="room-away" role="status">
      <Icon name="wifi" className="sm" />
      <span>
        {t("rooms.hostAway", { machine: host?.machineName ?? "" })}
        {waiting.count > 0 && <> {t("rooms.waitingReplies", { count: waiting.count, since: waiting.since ? agoText(waiting.since, now) : "" })}</>}
      </span>
    </div>
  );
  if (!room)
    return (
      <div className="bot-page">
        {awayBanner}
        <div className="bot-empty" role={error ? "alert" : undefined}>{error || t("rooms.loading")}</div>
      </div>
    );
  const shadow = !!room.shadow;
  const table = room.mode === RoomMode.ROUNDTABLE;
  const light = room.mode === RoomMode.LIGHT;
  const update = (patch: { name?: string; mode?: RoomMode; maxRounds?: number }) =>
    local.updateRoom({ machineId, room: { roomId, ...patch } }).then(setRoom, (e) => setState({ kind: "error", text: errorText(e) }));
  const send = async (override?: { text: string; mentions: string[] }) => {
    const body = override?.text ?? text;
    if (shadow || !body.trim() || state.kind === "sending") return;
    setState({ kind: "sending" });
    try {
      const m = await local.postRoomMessage({ machineId, roomId, text: body, mentions: override?.mentions ?? mentions, idempotencyKey: override ? crypto.randomUUID() : key, lightChat: light });
      setMsgs((x) => [...x.filter((y) => y.messageId !== m.messageId), m]);
      if (!override) (setText(""), setMentions([]), setKey(crypto.randomUUID()));
      setState({ kind: "idle" });
      read();
    } catch (err) {
      setState({ kind: "error", text: errorText(err), retry: () => void send(override) });
    }
  };
  const woken = mentions.length > 0 ? mentions : table && room.members[0] ? [room.members[0].botKey] : [];
  const names = (keys: string[]) => keys.map((k) => room.members.find((x) => x.botKey === k)?.name ?? k).join("、");
  const finishRename = (keep: boolean) => {
    const n = renaming?.trim();
    setRenaming(null);
    if (keep && n && n !== room.name) void update({ name: n });
  };
  return (
    <div className="bot-page room-page" role="region" aria-label={room.name}>
      <header className="bot-head drag">
        <div className="bot-head-t">
          <div className="bot-head-l1">
            {!shadow && renaming !== null ? (
              <input
                className="field room-rename"
                autoFocus
                aria-label={t("rooms.rename")}
                value={renaming}
                maxLength={64}
                onChange={(e) => setRenaming(e.target.value)}
                onKeyDown={(e) => (e.key === "Enter" && !e.nativeEvent.isComposing ? finishRename(true) : e.key === "Escape" && finishRename(false))}
                onBlur={() => finishRename(true)}
              />
            ) : (
              <>
                <h1>{room.name}</h1>
                {!shadow && <button className="tb" title={t("rooms.rename")} aria-label={t("rooms.rename")} onClick={() => setRenaming(room.name)}><Icon name="edit" /></button>}
              </>
            )}
            <span className="bot-pill" data-usable={room.run === RoomRun.RUNNING ? "yes" : ""}>
              {shadow ? t("rooms.shadowTitle") : roomLine(t, room)}
            </span>
          </div>
          <div className="room-members" role="list" aria-label={t("rooms.members")}>
            {room.members.map((m) => {
              const b = bots.find((x) => x.key === m.botKey);
              return (
                <span key={m.botKey} className="room-member" role="listitem" title={b?.state.line}>
                  {b ? <BotFace b={b} size={20} /> : <span className="bav" style={{ width: 20, height: 20 }}><span className="bav-in" style={{ background: "#8E8E93" }} /></span>}
                  {m.name}
                  {b && <em data-usable={b.state.usable}>{b.state.word}</em>}
                </span>
              );
            })}
          </div>
        </div>
        {!shadow && (
          <div className="room-ctl">
            <div className="seg" role="group" aria-label={t("rooms.mode")}>
              <button className={!table && !light ? "on" : ""} aria-pressed={!table && !light} onClick={() => update({ mode: RoomMode.MANUAL })}>{t("rooms.manual")}</button>
              <button className={table ? "on" : ""} aria-pressed={table} onClick={() => update({ mode: RoomMode.ROUNDTABLE })}>{t("rooms.table")}</button>
              <button className={light ? "on" : ""} aria-pressed={light} onClick={() => update({ mode: RoomMode.LIGHT })}>{t("rooms.light")}</button>
            </div>
            {table && (
              <Picker size="sm" label={t("rooms.maxRounds")} value={room.maxRounds} onChange={(v) => update({ maxRounds: v })}
                options={Array.from({ length: 20 }, (_, i) => ({ value: i + 1, label: t("rooms.roundsN", { count: i + 1 }) }))} />
            )}
            {room.run === RoomRun.RUNNING && (
              <button className="btn sm" onClick={() => local.stopRoom(ref).then((r) => (setRoom(r), read()), (e) => setState({ kind: "error", text: errorText(e) }))}>{t("rooms.stop")}</button>
            )}
            <button className="btn sm" onClick={() => st.set({ sheet: { kind: "newRoom" } })}>
              <Icon name="plus" className="sm" />
              {t("rooms.newChat")}
            </button>
            <button className="tb" title={t("rooms.delete")} aria-label={t("rooms.delete")} onClick={() => st.set({ sheet: { kind: "deleteRoom", machineId, roomId, name: room.name, members: room.members.filter((m) => m.sessionId).map((m) => ({ name: m.name, sessionId: m.sessionId })) } })}>
              <Icon name="x" />
            </button>
          </div>
        )}
      </header>
      {awayBanner}
      <main className="bot-main bot-chat">
        <div className="bot-stream chat-log" role="log" aria-label={t("rooms.timeline")}>
          {msgs.length === 0 && <div className="bot-empty">{t(shadow ? "rooms.shadowEmpty" : table ? "rooms.emptyTable" : light ? "rooms.emptyLight" : "rooms.emptyManual")}</div>}
          <RoomTimeline
            room={room}
            msgs={msgs}
            handOn={shadow ? undefined : (to, m) => void send({ text: t("rooms.handOnText", { from: m.fromName, text: m.text }), mentions: [to] })}
            resend={shadow ? undefined : (m) => void send({ text: m.text, mentions: m.mentions })}
          />
          {/* Replies that wait for the host to come back (工单 #74): in the timeline, where they will come in. */}
          {waiting.count > 0 && <div className="room-sys" role="note">{t("rooms.waitingInline", { count: waiting.count })}</div>}
        </div>
        {shadow ? (
          <div className="shadow-ro-foot" role="note">{t("rooms.shadowReadOnly")}</div>
        ) : (
          <Composer
            value={text}
            onChange={(v) => (setText(v), state.kind === "error" && setState({ kind: "idle" }))}
            onSend={() => void send()}
            placeholder={t("rooms.placeholder")}
            state={state}
            disabled={away}
            why={{ text: woken.length ? t("rooms.wakes", { names: names(woken) }) : t("rooms.wakesNone"), tone: "quiet" }}
            mentions={
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
            }
          />
        )}
      </main>
    </div>
  );
}

/**
 * Deleting a room (工单 #73): what becomes of its members' sessions is said and chosen, never done quietly — the room
 * alone (the sessions stay in their projects), or the sessions made for this room too, listed.
 */
export function DeleteRoomSheet({ machineId, roomId, name, members }: { machineId: string; roomId: string; name: string; members: { name: string; sessionId: string }[] }) {
  const st = useStore();
  const { t } = useTranslation("common");
  const [withSessions, setWithSessions] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const sessions = members.map((m) => ({ ...m, s: st.sessions.find((x) => x.sessionId === m.sessionId) }));
  const go = async () => {
    setBusy(true);
    setError("");
    try {
      await local.deleteRoom({ machineId, roomId });
      const failed: string[] = [];
      if (withSessions) {
        for (const m of sessions) await local.deleteSession({ sessionId: m.sessionId }).catch(() => failed.push(m.name));
      }
      st.set({ sheet: null, selection: { kind: "overview" } });
      st.show(failed.length ? t("rooms.deletedSomeKept", { name, names: failed.join("、") }) : t(withSessions ? "rooms.deletedWith" : "rooms.deleted", { name, count: sessions.length }));
    } catch (e) {
      setError(errorText(e));
      setBusy(false);
    }
  };
  return (
    <>
      <h3 id="sheet-title">{t("rooms.deleteTitle", { name })}</h3>
      <p className="sub">{t("rooms.deleteSub")}</p>
      <div className="fgroup" role="radiogroup" aria-label={t("rooms.deleteChoice")}>
        <button type="button" className="frow choice" role="radio" aria-checked={!withSessions} onClick={() => setWithSessions(false)}>
          <Check on={!withSessions} label={t("rooms.deleteRoomOnly")} set={() => setWithSessions(false)} />
          <span className="ft">
            <span>{t("rooms.deleteRoomOnly")}</span>
            <span className="fdesc">{t("rooms.deleteRoomOnlyDesc", { count: sessions.length })}</span>
          </span>
        </button>
        <button type="button" className="frow choice" role="radio" aria-checked={withSessions} disabled={sessions.length === 0} onClick={() => setWithSessions(true)}>
          <Check on={withSessions} label={t("rooms.deleteWithSessions")} set={() => setWithSessions(true)} disabled={sessions.length === 0} />
          <span className="ft">
            <span>{t("rooms.deleteWithSessions")}</span>
            <span className="fdesc">{t("rooms.deleteWithSessionsDesc", { count: sessions.length })}</span>
          </span>
        </button>
      </div>
      {sessions.length > 0 && (
        <>
          <div className="fhead">{t("rooms.deleteAffected", { count: sessions.length })}</div>
          <div className="fgroup" role="list">
            {sessions.map((m) => (
              <div key={m.sessionId} className="frow" role="listitem">
                <span className="ft">
                  <span>{m.name}</span>
                  <span className="fdesc">{m.s ? `${displayTitle(m.s)} · ${m.s.projectId}` : t("rooms.sessionElsewhere")}</span>
                </span>
              </div>
            ))}
          </div>
        </>
      )}
      {error && <p className="err" role="alert">{error}</p>}
      <div className="sheet-foot">
        <button type="button" className="btn" onClick={() => st.set({ sheet: null })}>{t("rooms.cancel")}</button>
        <button type="button" className="btn danger" disabled={busy} onClick={() => void go()}>{busy ? t("rooms.deleting") : t(withSessions ? "rooms.deleteWithButton" : "rooms.deleteButton")}</button>
      </div>
    </>
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
              <Picker size="sm" label={t("rooms.maxRounds")} value={maxRounds} onChange={setMaxRounds}
                options={Array.from({ length: 20 }, (_, i) => ({ value: i + 1, label: t("rooms.roundsN", { count: i + 1 }) }))} />
            )}
          </span>
        </div>
      </div>
      <p className="sheet-note muted">{t(mode === RoomMode.ROUNDTABLE ? "rooms.tableNote" : "rooms.manualNote")}</p>
      <div className="fhead">{t("rooms.pick")}</div>
      <div className="fgroup room-pick" role="group" aria-label={t("rooms.pick")}>
        {bots.map((b) => {
          const on = b.key in picked;
          const projects = b.kind === "personal" ? [] : projectsOf(b);
          return (
            <div key={b.key} className={`frow room-pick-row ${on ? "on" : ""}`}>
              <Check on={on} label={b.name} set={() => toggle(b)} />
              <BotFace b={b} size={24} still />
              <span className="ft" onClick={() => toggle(b)}>
                <span>{b.name}</span>
                <span className="fdesc">{b.state.word}</span>
              </span>
              {on && b.kind !== "personal" && (
                projects.length ? (
                  <Picker size="sm" label={t("rooms.projectOf", { name: b.name })} value={picked[b.key]} onChange={(v) => setPicked({ ...picked, [b.key]: v })}
                    options={projects.map((p) => ({ value: p.projectId, label: projectTitle(p) }))} />
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
