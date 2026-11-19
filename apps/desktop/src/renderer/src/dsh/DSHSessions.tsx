import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { agoText, dateOf, transcript } from "@hermesh/core";
import { DSHSessionState, type DSHBridge, type DSHSession, type PrepareDSHBridgeResponse } from "@hermesh/proto/machine";
import type { DSHSessionEntry, Unreachable } from "@hermesh/proto/local";
import { EventView, type SessionEvent } from "@hermesh/proto/session";
import { local } from "../api";
import { bridgeView, newRequestId, retryKeepsId } from "../dshTakeover";
import { Switch } from "../settings/common";
import { errorText, useNow, useStore } from "../store";
import "./dsh.css";

// DeepSeek Harness's own sessions on the owner's machines: those opened in DSH Desktop, or by another
// tool, which Hermesh did not start: their list (by last activity) and what DSH wrote of each. Read
// only, until the owner takes one over (docs/dsh-takeover.md): then Hermesh sends it their messages
// through the bridge plugin in DSH Desktop, queued after a turn that runs, as DSH queues its own.

const stateClass: Record<DSHSessionState, string> = {
  [DSHSessionState.DSH_SESSION_STATE_UNSPECIFIED]: "unknown",
  [DSHSessionState.DSH_SESSION_STATE_RUNNING]: "running",
  [DSHSessionState.DSH_SESSION_STATE_IDLE]: "idle",
  [DSHSessionState.DSH_SESSION_STATE_ENDED]: "ended",
};

const pageSize = 200;

export function DSHSessions() {
  const { t } = useTranslation("common");
  const st = useStore();
  const now = useNow(30_000);
  const [entries, setEntries] = useState<DSHSessionEntry[]>();
  const [unreachable, setUnreachable] = useState<Unreachable[]>([]);
  const [bridges, setBridges] = useState<Record<string, DSHBridge | undefined>>({});
  const [error, setError] = useState("");
  const [archived, setArchived] = useState(false);
  const sel = st.selection.kind === "dsh" ? st.selection : undefined;
  const load = useCallback(() => {
    local.listDSHSessions({}).then(
      (r) => (
        setEntries(r.sessions), setUnreachable(r.unreachable), setBridges(Object.fromEntries(r.bridges.map((b) => [b.machineId, b.bridge]))), setError("")
      ),
      (e) => setError(errorText(e)),
    );
  }, []);
  useEffect(() => {
    load();
    const i = setInterval(load, 15_000);
    return () => clearInterval(i);
  }, [load]);
  const shown = (entries ?? []).filter((e) => archived || !e.session?.archived);
  const nArchived = (entries ?? []).length - shown.length;
  const chosen = sel?.id ? entries?.find((e) => e.machineId === sel.machine && e.session?.sessionId === sel.id) : undefined;
  const stateText = (s: DSHSession) =>
    s.state === DSHSessionState.DSH_SESSION_STATE_RUNNING
      ? t("dsh.running")
      : s.state === DSHSessionState.DSH_SESSION_STATE_IDLE
        ? t("dsh.idle")
        : s.state === DSHSessionState.DSH_SESSION_STATE_ENDED
          ? t(s.turnOpen ? "dsh.endedMidTurn" : "dsh.ended")
          : t("dsh.unknown");
  return (
    <>
      <div className="ctool drag">
        <div className="ctitle" />
      </div>
      <div className="dsh">
        <nav className="dsh-list" aria-label={t("dsh.title")}>
          <h1>{t("dsh.title")}</h1>
          <p className="dsh-note">{t("dsh.note")}</p>
          {error && <p className="dsh-note warn">{error}</p>}
          {unreachable.map((u) => (
            <p key={u.machineId} className="dsh-note warn">
              {t("dsh.unreachable", { name: u.machineName || u.machineId })}
            </p>
          ))}
          {entries && shown.length === 0 && <p className="dsh-empty">{t("dsh.none")}</p>}
          {shown.map((e) => {
            const s = e.session!;
            const at = dateOf(s.lastActivityAt);
            return (
              <button
                key={`${e.machineId}/${s.sessionId}`}
                className={`dsh-row ${chosen === e ? "sel" : ""}`}
                aria-current={chosen === e || undefined}
                onClick={() => st.set({ selection: { kind: "dsh", machine: e.machineId, id: s.sessionId } })}
              >
                <i className={`dsh-dot ${stateClass[s.state]}`} title={stateText(s)} aria-hidden="true" />
                <span className="t">
                  <b>{s.title || s.sessionId}</b>
                  <span>
                    {[stateText(s), at ? agoText(at, now) : "", e.machineName, baseName(s.cwd), s.archived ? t("dsh.archived") : ""].filter(Boolean).join(" · ")}
                  </span>
                </span>
              </button>
            );
          })}
          {nArchived > 0 || archived ? (
            <button className="link dsh-toggle" onClick={() => setArchived(!archived)}>
              {archived ? t("dsh.hideArchived") : t("dsh.showArchived", { count: nArchived })}
            </button>
          ) : null}
        </nav>
        <div className="dsh-view">
          {chosen ? (
            <DSHTranscript key={`${chosen.machineId}/${chosen.session!.sessionId}`} entry={chosen} stateText={stateText(chosen.session!)} bridge={bridges[chosen.machineId]} changed={load} />
          ) : (
            <p className="dsh-empty">{t("dsh.pick")}</p>
          )}
        </div>
      </div>
    </>
  );
}

function baseName(p: string): string {
  const parts = p.split(/[\\/]/).filter(Boolean);
  return parts[parts.length - 1] ?? "";
}

/**
 * What DSH wrote of one session, newest page first; earlier pages on request. Taken over, a box to send
 * it messages and a way to stop its turn. changed: the list should be read again.
 */
function DSHTranscript({ entry, stateText, bridge, changed }: { entry: DSHSessionEntry; stateText: string; bridge: DSHBridge | undefined; changed: () => void }) {
  const { t } = useTranslation("common");
  const s = entry.session!;
  const [events, setEvents] = useState<SessionEvent[]>([]);
  const [more, setMore] = useState(false);
  const [error, setError] = useState(s.error);
  const read = useCallback(
    (beforeSeq: bigint) =>
      local.readDSHSession({ machineId: entry.machineId, sessionId: s.sessionId, beforeSeq, limit: pageSize, view: EventView.FULL }).then(
        (r) => {
          setEvents((old) => (beforeSeq > 0n ? [...r.events, ...old] : r.events));
          setMore(r.hasMoreBefore);
          setError(r.session?.error ?? "");
        },
        (e) => setError(errorText(e)),
      ),
    [entry.machineId, s.sessionId],
  );
  useEffect(() => {
    read(0n);
  }, [read, s.lastActivityAt?.seconds]);
  const items = transcript(events, s.state === DSHSessionState.DSH_SESSION_STATE_RUNNING);
  const [takeover, setTakeover] = useState(s.takeover);
  const [asking, setAsking] = useState(false);
  const [switchError, setSwitchError] = useState("");
  useEffect(() => setTakeover(s.takeover), [s.takeover]);
  const setOn = (on: boolean) => {
    setSwitchError("");
    local.setDSHTakeover({ machineId: entry.machineId, sessionId: s.sessionId, on }).then(
      (r) => (setTakeover(r.session?.takeover ?? on), setAsking(false), changed()),
      (e) => setSwitchError(errorText(e)),
    );
  };
  return (
    <>
      <div className="dsh-head">
        <div className="dsh-head-row">
          <h2>{s.title || s.sessionId}</h2>
          <label className="dsh-takeover">
            {t("dsh.takeover")}
            <Switch on={takeover} label={t("dsh.takeover")} set={(on) => (on ? setAsking(true) : (setAsking(false), setOn(false)))} />
          </label>
        </div>
        <span>{[stateText, entry.machineName, s.cwd].filter(Boolean).join(" · ")}</span>
      </div>
      {asking && !takeover && (
        <div className="dsh-confirm" role="dialog" aria-label={t("dsh.takeoverAsk")}>
          <b>{t("dsh.takeoverAsk")}</b>
          <ul>
            <li>{t("dsh.takeoverNote1")}</li>
            <li>{t("dsh.takeoverNote2")}</li>
            <li>{t("dsh.takeoverNote3")}</li>
          </ul>
          <div className="dsh-confirm-acts">
            <button className="btn sm" onClick={() => setAsking(false)}>
              {t("dsh.cancel")}
            </button>
            <button className="btn sm primary" onClick={() => setOn(true)}>
              {t("dsh.takeoverOn")}
            </button>
          </div>
        </div>
      )}
      {switchError && <p className="dsh-msg end err">{switchError}</p>}
      {error && <p className="dsh-msg end err">{error}</p>}
      {more && (
        <button className="btn sm" onClick={() => events.length && read(events[0].seq)}>
          {t("dsh.earlier")}
        </button>
      )}
      {items.map((it) => {
        switch (it.kind) {
          case "user":
            // Every message here is the owner's; those sent from Hermesh (taken over) say so.
            return (
              <div key={String(it.seq)} className="dsh-msg user">
                {it.from === "owner" && <span className="dsh-via">{t("dsh.viaHermesh")}</span>}
                {it.text}
              </div>
            );
          case "agent":
            return <div key={String(it.seq)} className="dsh-msg">{it.text}</div>;
          case "tool":
            return (
              <div key={String(it.seq)} className="dsh-msg tool">
                <details>
                  <summary>
                    {it.call.name} {it.call.summary}
                  </summary>
                  {it.result && <pre>{it.result.summary}</pre>}
                </details>
              </div>
            );
          case "turnEnd":
            return it.end.isError ? (
              <div key={String(it.seq)} className="dsh-msg end err">
                {it.end.failure?.message || t("dsh.turnFailed")}
              </div>
            ) : null;
          default:
            return null;
        }
      })}
      {items.length === 0 && !error && <p className="dsh-empty">{t("dsh.empty")}</p>}
      {takeover && <Composer entry={entry} bridge={bridge} sent={() => (changed(), setTimeout(() => read(0n), 1500))} failed={changed} />}
    </>
  );
}

/**
 * Installing the bridge plugin: DSH Desktop's own plugin manager does it (Plugins page), checking it
 * against the DSH there, restoring the profile when it fails, and turning it off or removing it later.
 * Hermesh prepares: it backs up DSH's configuration and writes the plugin's package, whose path it shows.
 */
function Install({ machineId }: { machineId: string }) {
  const { t } = useTranslation("common");
  const [prepared, setPrepared] = useState<PrepareDSHBridgeResponse>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [copied, setCopied] = useState(false);
  const prepare = () => {
    setBusy(true);
    setError("");
    local.prepareDSHBridge({ machineId }).then(
      (r) => (setPrepared(r), setBusy(false)),
      (e) => (setError(errorText(e)), setBusy(false)),
    );
  };
  if (!prepared) {
    return (
      <div className="dsh-install">
        <button className="btn sm" onClick={prepare} disabled={busy}>
          {busy ? t("dsh.install.preparing") : t("dsh.install.prepare")}
        </button>
        {error && <span className="err" role="alert">{error}</span>}
      </div>
    );
  }
  return (
    <div className="dsh-install" role="region" aria-label={t("dsh.install.title")}>
      <b>{t("dsh.install.title")}</b>
      <ol>
        <li>{t("dsh.install.backedUp", { count: prepared.backedUp.length, dir: prepared.backupDir })}</li>
        {!prepared.desktopProfile && <li className="warn">{t("dsh.install.noProfile")}</li>}
        <li>
          {t("dsh.install.step")}
          <span className="dsh-path">
            <span className="mono sel-text">{prepared.packagePath}</span>
            <button className="btn sm" onClick={() => navigator.clipboard.writeText(prepared.packagePath).then(() => setCopied(true))}>
              {copied ? t("dsh.install.copied") : t("dsh.install.copy")}
            </button>
          </span>
        </li>
        <li>{t("dsh.install.restart")}</li>
      </ol>
    </div>
  );
}

/**
 * Taken over: the owner's message to the session, sent through the bridge plugin in DSH Desktop. It
 * waits for the next turn when one runs (as in DSH). A failure says what to do and keeps the text; a
 * timeout keeps the message's id too, so sending again can't make two.
 */
function Composer({ entry, bridge, sent, failed }: { entry: DSHSessionEntry; bridge: DSHBridge | undefined; sent: () => void; failed: () => void }) {
  const { t } = useTranslation("common");
  const s = entry.session!;
  const running = s.state === DSHSessionState.DSH_SESSION_STATE_RUNNING;
  const b = bridgeView(bridge);
  const [text, setText] = useState("");
  const reqId = useRef("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const send = () => {
    if (!text.trim() || busy) return;
    reqId.current ||= newRequestId();
    setBusy(true);
    setError("");
    setNotice("");
    local.sendDSHMessage({ machineId: entry.machineId, sessionId: s.sessionId, text, requestId: reqId.current }).then(
      () => {
        reqId.current = "";
        setText("");
        setNotice(t(running ? "dsh.queued" : "dsh.sent"));
        setBusy(false);
        sent();
      },
      (e) => {
        if (!retryKeepsId(e)) reqId.current = "";
        setError(t("dsh.notSent", { reason: errorText(e) }));
        setBusy(false);
        failed(); // the plugin's state, as it is now, above the box
      },
    );
  };
  const interrupt = () => {
    setError("");
    setNotice("");
    local.interruptDSHSession({ machineId: entry.machineId, sessionId: s.sessionId }).then(
      (r) => (setNotice(t(r.interrupted ? "dsh.interrupted" : "dsh.notRunningHere")), sent()),
      (e) => setError(errorText(e)),
    );
  };
  return (
    <div className="dsh-compose">
      {!b.ok && (
        <p className="dsh-bridge warn" role="status">
          {t(`dsh.bridge.${b.key}`)}
          {b.key === "incompatible" && b.detail && <span>{b.detail}</span>}
        </p>
      )}
      {(b.key === "notInstalled" || b.key === "incompatible") && <Install machineId={entry.machineId} />}
      <textarea
        aria-label={t("dsh.message")}
        placeholder={t(running ? "dsh.placeholderRunning" : "dsh.placeholder")}
        value={text}
        disabled={busy}
        onChange={(e) => ((reqId.current = ""), setText(e.target.value))}
        onKeyDown={(e) => e.key === "Enter" && (e.metaKey || e.ctrlKey) && (e.preventDefault(), send())}
      />
      <div className="dsh-compose-acts">
        {error && <span className="err" role="alert">{error}</span>}
        {notice && !error && <span className="ok" role="status">{notice}</span>}
        {running && (
          <button className="btn sm" onClick={interrupt} disabled={!b.ok}>
            {t("dsh.interrupt")}
          </button>
        )}
        <button className="btn sm primary" onClick={send} disabled={busy || !text.trim() || !b.ok}>
          {busy ? t("dsh.sending") : t("dsh.send")}
        </button>
      </div>
    </div>
  );
}
