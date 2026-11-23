import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { codingAgentTitle, dateOf, listTime } from "@aide/core";
import type { ClaudeSessionEntry, DSHSessionEntry, Machine } from "@aide/proto/local";
import type { DSHBridge } from "@aide/proto/machine";
import type { Bot } from "@aide/core";
import { local } from "../api";
import { ClaudeTranscript, useClaudeLine } from "../claude/ClaudeSessions";
import { baseName, dshDot, DSHTranscript, LostSession, useDSHStateText } from "../dsh/DSHSessions";
import { Icon } from "../icons";
import { errorText, useNow, useStore } from "../store";
import "../dsh/dsh.css";

// A coding agent's own sessions (工单 #76): those it has in its own app on its machine — DSH's in DSH, Claude Code's in
// the Claude desktop app — which Aide did not start, on its bot page as its conversations are (#67: title, last
// activity, state, the archived apart), one opening there. Every coding agent has this place; where Aide can't read an
// agent's own sessions (yet), it says so in plain words rather than leaving the place out.

/** The page of an agent on a machine (this computer's first, else the first that has it): where its own sessions are. */
export function agentBotKey(machines: readonly Machine[], agent: string): string | undefined {
  const m = [...machines].sort((a, b) => Number(b.local) - Number(a.local)).find((x) => x.status?.agents.some((a) => a.name === agent));
  return m && `m:${m.machineId}/${agent}`;
}

/** Which agents' own sessions Aide reads, and how. */
export const ownReader = (agent: string): "dsh" | "claude" | undefined => (agent === "deepseek" ? "dsh" : agent === "claude" ? "claude" : undefined);

type Own =
  | { kind: "dsh"; entries: DSHSessionEntry[]; bridge?: DSHBridge; home: string }
  | { kind: "claude"; entries: ClaudeSessionEntry[]; indexes: string[] };

/** Its own sessions on its machine, read again every 15 s while its page is open. */
function useOwn(agent: string, machineId: string): { own?: Own; lost: boolean; error: string; reload: () => void } {
  const reader = ownReader(agent);
  const [own, setOwn] = useState<Own>();
  const [lost, setLost] = useState(false);
  const [error, setError] = useState("");
  const load = useCallback(() => {
    if (reader === "dsh")
      local.listDSHSessions({}).then(
        (r) => {
          const b = r.bridges.find((x) => x.machineId === machineId);
          setOwn({ kind: "dsh", entries: r.sessions.filter((e) => e.machineId === machineId), bridge: b?.bridge, home: b?.dshHome ?? "" });
          setLost(r.unreachable.some((u) => u.machineId === machineId));
          setError("");
        },
        (e) => setError(errorText(e)),
      );
    if (reader === "claude")
      local.listClaudeSessions({}).then(
        (r) => {
          setOwn({ kind: "claude", entries: r.sessions.filter((e) => e.machineId === machineId), indexes: r.indexes.find((x) => x.machineId === machineId)?.indexes ?? [] });
          setLost(r.unreachable.some((u) => u.machineId === machineId));
          setError("");
        },
        (e) => setError(errorText(e)),
      );
  }, [reader, machineId]);
  useEffect(() => {
    if (!reader) return;
    load();
    const i = setInterval(load, 15_000);
    return () => clearInterval(i);
  }, [load, reader]);
  return { own, lost, error, reload: load };
}

/** Its own sessions, under its conversations on its page: each opens there (open), the archived apart. */
export function OwnSessions({ b, m }: { b: Bot; m: Machine }) {
  const st = useStore();
  const { t } = useTranslation("common");
  const now = useNow(30_000);
  const app = codingAgentTitle(b.agent);
  const { own, lost, error } = useOwn(b.agent, m.machineId);
  const dshState = useDSHStateText();
  const claudeLine = useClaudeLine();
  const key = `own:${b.key}`;
  const shown = !st.collapsed.includes(key);
  const [archived, setArchived] = useState(false);
  const open = (id: string) => st.set({ selection: { kind: "bot", key: b.key, native: id } });
  const reader = ownReader(b.agent);
  type Row = { id: string; title: string; line: string; dot: string; archived: boolean };
  const rows: Row[] =
    own?.kind === "dsh"
      ? own.entries.map((e) => {
          const s = e.session!;
          const at = dateOf(s.lastActivityAt);
          return { id: s.sessionId, title: s.title || s.sessionId, line: [dshState(e), at ? listTime(at, now) : "", baseName(s.cwd)].filter(Boolean).join(" · "), dot: dshDot(e), archived: s.archived };
        })
      : own?.kind === "claude"
        ? own.entries.map((e) => ({ id: e.session!.sessionId, title: e.session!.title || e.session!.sessionId, line: claudeLine(e, true), dot: e.lost ? "lost" : "ended", archived: e.session!.archived }))
        : [];
  const open_ = rows.filter((r) => !r.archived);
  const shut = rows.filter((r) => r.archived);
  const row = (r: Row) => (
    <div key={r.id} className="conv-row" role="listitem">
      <button className="conv-main" onClick={() => open(r.id)}>
        <i className={`dsh-dot ${r.dot}`} aria-hidden="true" />
        <span className="conv-t">
          <b>{r.title}</b>
          <span>{r.line}</span>
        </span>
      </button>
    </div>
  );
  return (
    <section className="own" aria-label={t("own.title")}>
      <button className="conv-group" aria-expanded={shown} onClick={() => st.set({ collapsed: shown ? [...st.collapsed, key] : st.collapsed.filter((c) => c !== key) })}>
        {t("own.title")}
        {reader && own && <span className="muted">{open_.length}</span>}
        <Icon name="caret" className={`sm ${shown ? "" : "shut"}`} />
      </button>
      {shown && (
        <div role="list" aria-label={t("own.title")}>
          {!reader && <p className="own-note">{t("own.cant", { app })}</p>}
          {reader && <p className="own-note">{t(reader === "dsh" ? "own.noteDsh" : "own.noteClaude")}</p>}
          {error && <p className="own-note warn">{error}</p>}
          {lost && <p className="own-note warn">{t(rows.length ? "dsh.unreachableLastKnown" : "dsh.unreachable", { name: m.machineName })}</p>}
          {own && rows.length === 0 && (
            <p className="own-note">
              {t("own.none", { app })}
              {own.kind === "dsh" && own.home && <> {t("dsh.readFrom", { name: m.machineName })} <span className="mono sel-text">{own.home}</span> {t("dsh.homeHint")}</>}
              {own.kind === "claude" && own.indexes.length === 0 && <> {t("claude.noApp", { name: m.machineName })}</>}
            </p>
          )}
          {open_.map(row)}
          {shut.length > 0 && (
            <button className="conv-group" aria-expanded={archived} onClick={() => setArchived(!archived)}>
              {t("conv.archivedGroup", { count: shut.length })}
              <Icon name="caret" className={`sm ${archived ? "" : "shut"}`} />
            </button>
          )}
          {archived && shut.map(row)}
        </div>
      )}
    </section>
  );
}

/** One of its own sessions, opened on its page: what it wrote (DSH's: with taking it over), and the way back. */
export function OwnSession({ b, m, id }: { b: Bot; m: Machine; id: string }) {
  const st = useStore();
  const { t } = useTranslation("common");
  const { own, reload } = useOwn(b.agent, m.machineId);
  const dshState = useDSHStateText();
  const claudeLine = useClaudeLine();
  const back = () => st.set({ selection: { kind: "bot", key: b.key } });
  const dsh = own?.kind === "dsh" ? own.entries.find((e) => e.session?.sessionId === id) : undefined;
  const claude = own?.kind === "claude" ? own.entries.find((e) => e.session?.sessionId === id) : undefined;
  return (
    <main className="bot-main own-session">
      <button className="link own-back" onClick={back}>
        <Icon name="chevL" className="sm" />
        {t("own.back")}
      </button>
      <div className="dsh-view">
        {dsh ? (
          dsh.lost ? (
            <LostSession entry={dsh} stateText={dshState(dsh)} />
          ) : (
            <DSHTranscript key={id} entry={dsh} stateText={dshState(dsh)} bridge={own?.kind === "dsh" ? own.bridge : undefined} changed={reload} />
          )
        ) : claude ? (
          <ClaudeTranscript key={id} entry={claude} line={claudeLine(claude, false)} />
        ) : (
          <p className="dsh-empty">{own ? t("own.gone") : ""}</p>
        )}
      </div>
    </main>
  );
}
