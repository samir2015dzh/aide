import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { agoText, dateOf } from "@aide/core";
import type { HeldMessageEntry, Unreachable } from "@aide/proto/local";
import { local } from "../api";
import { errorText, useNow } from "../store";
import "../dsh/dsh.css";

// The messages the machines hold for the owner before they reach a session (docs/agent-messages.md §15.5):
// from an agent let in less than 7 days ago, at hop 3 or more of a chain, or matching the owner's
// information boundary. Each is released to its session or discarded, here only; a day later it expires.
// Only messages: nothing here approves an action.

/** How many messages wait for the owner, looked at every half minute; 0 until known. */
export function useHeldCount(enabled: boolean): number {
  const [n, setN] = useState(0);
  useEffect(() => {
    if (!enabled) return;
    let live = true;
    const look = () =>
      local.listHeldMessages({}).then(
        (r) => live && setN(r.messages.length),
        () => {},
      );
    look();
    const i = setInterval(look, 30_000);
    return () => {
      live = false;
      clearInterval(i);
    };
  }, [enabled]);
  return n;
}

export function HeldMessages() {
  const { t } = useTranslation("common");
  const now = useNow(30_000);
  const [entries, setEntries] = useState<HeldMessageEntry[]>();
  const [unreachable, setUnreachable] = useState<Unreachable[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState("");
  const load = useCallback(() => {
    local.listHeldMessages({}).then(
      (r) => (setEntries(r.messages), setUnreachable(r.unreachable), setError("")),
      (e) => setError(errorText(e)),
    );
  }, []);
  useEffect(() => {
    load();
    const i = setInterval(load, 15_000);
    return () => clearInterval(i);
  }, [load]);
  const decide = (e: HeldMessageEntry, release: boolean) => {
    const id = e.message!.messageId;
    setBusy(id);
    local
      .releaseHeldMessage({ machineId: e.machineId, messageId: id, release })
      .then(
        () => setError(""),
        (err) => setError(errorText(err)),
      )
      .finally(() => (setBusy(""), load()));
  };
  const reason = (r: string) =>
    r === "new_agent" ? t("held.reason_new_agent") : r === "long_chain" ? t("held.reason_long_chain") : r === "boundary" ? t("held.reason_boundary") : r;
  return (
    <>
      <div className="ctool drag">
        <div className="ctitle" />
      </div>
      <div className="dsh">
        <section className="dsh-view held" aria-label={t("held.title")}>
          <h1>{t("held.title")}</h1>
          <p className="dsh-note">{t("held.note")}</p>
          {error && <p className="dsh-note warn">{error}</p>}
          {unreachable.map((u) => (
            <p key={u.machineId} className="dsh-note warn">
              {t("dsh.unreachable", { name: u.machineName || u.machineId })}
            </p>
          ))}
          {entries && entries.length === 0 && <p className="dsh-empty">{t("held.none")}</p>}
          <ul className="held-list">
            {(entries ?? []).map((e) => {
              const m = e.message!;
              const at = dateOf(m.heldAt);
              const until = dateOf(m.expiresAt);
              return (
                <li key={`${e.machineId}/${m.messageId}`} className="dsh-row held-row" aria-label={m.fromName || m.from}>
                  <span className="t">
                    <b>{m.fromName || m.from}</b>
                    <span>
                      {[reason(m.reason), t("held.to", { session: m.sessionId }), e.machineName, at ? agoText(at, now) : "", until ? t("held.expires", { time: until.toLocaleString() }) : ""]
                        .filter(Boolean)
                        .join(" · ")}
                    </span>
                    {m.detail && <span className="dsh-note">{m.detail}</span>}
                    <span className="dsh-msg user">{m.text}</span>
                  </span>
                  <span className="held-actions">
                    <button className="btn sm primary" disabled={busy === m.messageId} onClick={() => decide(e, true)}>
                      {t("held.release")}
                    </button>
                    <button className="btn sm" disabled={busy === m.messageId} onClick={() => decide(e, false)}>
                      {t("held.discard")}
                    </button>
                  </span>
                </li>
              );
            })}
          </ul>
        </section>
      </div>
    </>
  );
}
