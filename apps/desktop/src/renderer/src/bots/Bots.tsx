import { useEffect, useMemo, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { SessionStatus, type Session } from "@aide/proto/session";
import type { Machine } from "@aide/proto/local";
import { agentEntry, allBots, botCounts, botCountsText, displayTitle, listTime, machineConnections, projectTitle, roster, updatedDate, type Bot, type Roster } from "@aide/core";
import { AgentTile } from "@aide/ui";
import { AgentPage, projectsFor } from "../AgentList";
import { local } from "../api";
import { connDot, ConnRowView } from "../Connections";
import { GrantAvatar, StateText, StatusDot } from "../components";
import { Icon } from "../icons";
import { GrantDetail } from "../settings/Grants";
import { errorText, useNow, useStore } from "../store";
import "./bots.css";

// The roster (工单 #57, BOTARK-REF 产品结构纲要): a bot is the unit the app is shown in. The sidebar lists every
// bot, synced from the account (personal agents) and the machines (their coding agents), one row each: its face, its
// one name, its state as a dot and a word, and a line of what it does lately. A click opens the bot's own page: its
// work in the middle, with a box to hand it work, and beside it its workbench — how it is connected, its projects,
// pause, hiding, its grant and kind — all under it, not in the machine, settings and task drawers.

export function useRoster(): Roster {
  const st = useStore();
  const now = useNow(30_000);
  return useMemo(() => roster(st.machines, st.clients, st.sessions, now), [st.machines, st.clients, st.sessions, now]);
}

/** A bot's face: its coding agent's logo, or its grant's picture. */
export function BotFace({ b, size = 28 }: { b: Bot; size?: number }) {
  if (b.client && (b.kind === "personal" || !b.agent)) return <GrantAvatar c={b.client} size={size >= 40 ? "lg" : size >= 30 ? "md" : "sm"} />;
  return <AgentTile agent={b.agent} size={size} />;
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

/** The roster in the sidebar, first thing under the pages: the personal agents, then each machine's. */
export function SidebarRoster() {
  const st = useStore();
  const { t } = useTranslation("common");
  const r = useRoster();
  const counts = botCounts(allBots(r));
  const sel = st.selection.kind === "bot" ? st.selection.key : "";
  const group = (id: string, title: string, bots: Bot[], m?: Machine) => {
    const key = `bots:${id}`;
    const open = !st.collapsed.includes(key);
    return (
      <div key={id} role="group" aria-label={title}>
        <button className={`grp-h ${open ? "" : "shut"}`} aria-expanded={open} onClick={() => st.set({ collapsed: open ? [...st.collapsed, key] : st.collapsed.filter((c) => c !== key) })}>
          {m && <i className={`dotc ${m.online || m.local ? "on" : ""} bot-mdot`} aria-hidden="true" />}
          {title}
          <Icon name="caret" className="sm" />
        </button>
        {open && bots.length === 0 && <div className="side-none">{t("bots.noneHere")}</div>}
        {open && bots.map((b) => (
          <button key={b.key} className={`bot-row ${sel === b.key ? "sel" : ""}`} aria-current={sel === b.key || undefined} onClick={() => st.set({ selection: { kind: "bot", key: b.key } })}>
            <span className="bot-face">
              <BotFace b={b} size={26} />
              <i className={`dotc ${connDot(b.state.state)}`} aria-hidden="true" />
            </span>
            <span className="bot-t">
              <span className="bot-l1">
                <b>{b.name}</b>
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
  if (!r.personal.length && !r.machines.length) return null;
  return (
    <section className="side-bots" aria-label={t("bots.title")}>
      <div className="side-h">
        <span>{t("bots.title")}</span>
        <span className="bot-counts" title={botCountsText(counts)}>{botCountsText(counts)}</span>
      </div>
      {r.personal.length > 0 && group("personal", t("bots.personal"), r.personal)}
      {r.machines.map((g) => group(g.machine.machineId, g.machine.local ? t("bots.thisComputer", { name: g.machine.machineName }) : g.machine.machineName, g.bots, g.machine))}
    </section>
  );
}

/** A bot's own page (工单 #57): who it is and how it stands at the top; its work in the middle; its workbench beside. */
export function BotPage({ botKey }: { botKey: string }) {
  const st = useStore();
  const { t } = useTranslation("common");
  const r = useRoster();
  const b = allBots(r).find((x) => x.key === botKey);
  const { set } = st;
  // Gone (its machine forgot it, its grant was revoked): back to the overview.
  useEffect(() => {
    if (!b && st.status) set({ selection: { kind: "overview" } });
  }, [b, set, st.status]);
  if (!b) return null;
  const m = st.machines.find((x) => x.machineId === b.machineId);
  const work = st.sessions
    .filter((s) => s.status !== SessionStatus.ARCHIVED && (b.kind === "local" && b.agent && !b.key.startsWith("c:") ? s.machineId === b.machineId && s.agent === b.agent : s.createdBy === b.client?.clientId))
    .sort((x, y) => updatedDate(y).getTime() - updatedDate(x).getTime());
  const where = m ? (m.local ? t("bots.thisComputer", { name: m.machineName }) : m.machineName) : t("bots.follows");
  return (
    <div className="bot-page" role="region" aria-label={b.name}>
      <header className="bot-head drag">
        <BotFace b={b} size={44} />
        <div className="bot-head-t">
          <div className="bot-head-l1">
            <h1>{b.name}</h1>
            <BotPill b={b} />
          </div>
          <div className="bot-head-l2">
            <span className="bot-where"><Icon name={m ? "laptop" : "user"} className="sm" />{where}</span>
            <span>{b.state.line}</span>
          </div>
        </div>
      </header>
      <div className="bot-body">
        <main className="bot-main">
          <div className="bot-sec-h">{t("bots.work")}</div>
          <div className="bot-stream" role="list" aria-label={t("bots.work")}>
            {work.length === 0 && <div className="bot-empty">{t(b.kind === "personal" ? "bots.noWorkPersonal" : "bots.noWork", { name: b.name })}</div>}
            {work.map((s) => <WorkRow key={s.sessionId} s={s} />)}
          </div>
          {m && b.agent && !b.key.startsWith("c:") ? <Compose b={b} m={m} /> : <div className="bot-chat-soon">{t("bots.chatSoon", { name: b.name })}</div>}
        </main>
        <aside className="bot-bench" aria-label={t("bots.bench")}>
          {m && b.agent && !b.key.startsWith("c:") && <LocalBench b={b} m={m} />}
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

function WorkRow({ s }: { s: Session }) {
  const st = useStore();
  const now = useNow(30_000);
  return (
    <button className="bot-work" role="listitem" onClick={() => st.select(s.sessionId)}>
      <StatusDot s={s} />
      <span className="bot-work-t">
        <b>{displayTitle(s)}</b>
        <span><StateText s={s} /> · {s.projectId}</span>
      </span>
      <span className="bot-work-when">{listTime(updatedDate(s), now)}</span>
    </button>
  );
}

/** Its connections (Claude Desktop and the command line, DSH's bridge plugin), then what the machine page managed. */
function LocalBench({ b, m }: { b: Bot; m: Machine }) {
  const { t } = useTranslation("common");
  const now = useNow(30_000);
  const e = agentEntry(machineConnections(m, now), b.agent);
  const recheck = () => void local.checkAgents({ machineId: m.local ? "" : m.machineId }).catch(() => {});
  return (
    <>
      <section>
        <div className="bot-sec-h">
          {t("bots.connection")}
          <button className="link" onClick={() => window.aide?.openSettings(`machines/${m.machineId}`)}>{t("bots.device")}</button>
        </div>
        <div className="ag-conn">
          {e?.rows.map((r) => <ConnRowView key={r.id} r={r} m={m} recheck={recheck} logo={false} part />)}
        </div>
        <button className="btn sm" onClick={recheck} disabled={!m.online && !m.local}>{t("bots.recheck")}</button>
      </section>
      <section>
        <div className="bot-sec-h">{t("bots.manage")}</div>
        <AgentPage m={m} agent={b.agent} bare />
      </section>
    </>
  );
}

/** Hand it work from its page: a project, the task, and it starts there (as Hand Work To does). */
function Compose({ b, m }: { b: Bot; m: Machine }) {
  const st = useStore();
  const { t } = useTranslation("common");
  const projects = projectsFor(st.projects.filter((p) => p.machineId === m.machineId), b.agent);
  const [project, setProject] = useState("");
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [key, setKey] = useState(() => crypto.randomUUID());
  const proj = projects.find((p) => p.projectId === project) ?? projects[0];
  const can = b.state.usable !== "no" && !!proj && !!text.trim() && !busy;
  const send = async (e: FormEvent) => {
    e.preventDefault();
    if (!can) return;
    setBusy(true);
    try {
      const res = await local.startSession({ machineId: m.machineId, session: { idempotencyKey: key, agent: b.agent, projectId: proj!.projectId, prompt: text } });
      st.show(t("bots.started", { name: b.name, title: displayTitle(res.session!) }));
      setText("");
      setKey(crypto.randomUUID());
    } catch (err) {
      st.show(errorText(err));
    }
    setBusy(false);
  };
  return (
    <form className="bot-compose" onSubmit={send}>
      {b.state.usable === "no" ? (
        <div className="bot-compose-why">{t("bots.cantSend", { word: b.state.word, line: b.state.line })}</div>
      ) : b.state.usable === "unverified" ? (
        <div className="bot-compose-why quiet">{t("bots.unverified")}</div>
      ) : null}
      {projects.length === 0 ? (
        <div className="bot-compose-why quiet">{t("bots.noProject")}</div>
      ) : (
        <>
          <textarea className="field" rows={3} value={text} placeholder={t("bots.placeholder", { name: b.name })} aria-label={t("bots.placeholder", { name: b.name })}
            onChange={(e) => setText(e.target.value)} onKeyDown={(e) => e.key === "Enter" && (e.metaKey || e.ctrlKey) && void send(e)} />
          <div className="bot-compose-bar">
            {projects.length > 1 ? (
              <select className="field sm" value={proj?.projectId ?? ""} aria-label={t("bots.project")} onChange={(e) => setProject(e.target.value)}>
                {projects.map((p) => <option key={p.projectId} value={p.projectId}>{projectTitle(p)}</option>)}
              </select>
            ) : (
              <span className="muted">{projectTitle(proj!)}</span>
            )}
            <button type="submit" className="btn primary sm" disabled={!can}>{busy ? t("bots.sending") : t("bots.send")}</button>
          </div>
        </>
      )}
    </form>
  );
}
