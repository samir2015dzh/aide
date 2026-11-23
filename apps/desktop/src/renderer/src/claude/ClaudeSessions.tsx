import { useCallback, useEffect, useState } from "react";
import { ToolRow } from "../dsh/ToolRow";
import { MachineTag } from "../MachineTag";
import { useTranslation } from "react-i18next";
import { agoText, dateOf, lostLine, transcript } from "@aide/core";
import type { ClaudeSessionEntry } from "@aide/proto/local";
import { EventView, type SessionEvent } from "@aide/proto/session";
import { local } from "../api";
import { errorText, useNow } from "../store";
import "../dsh/dsh.css";

// The Claude Code sessions the Claude desktop app lists on the owner's machines, which Aide did not start
// (docs/claude-sessions.md), listed on Claude Code's bot page (工单 #76, bots/OwnSessions.tsx) by title and
// last activity, with the GitHub repository a session works in when its folder's own origin says so; here
// what Claude Code wrote of each. Read only. A machine that can't be reached keeps its sessions as last
// read, lost.

const pageSize = 200;

/** A Claude session's line: the app's word after its last turn (lost: as last known), when, the folder or repository. */
export function useClaudeLine(): (e: ClaudeSessionEntry, withTime: boolean) => string {
  const { t } = useTranslation("common");
  const now = useNow(30_000);
  const statusText = (status: string) => (status === "completed" ? t("claude.completed") : status === "blocked" ? t("claude.blocked") : status ? status : "");
  return (e, withTime) => {
    const s = e.session!;
    const at = dateOf(s.lastActivityAt);
    const read = dateOf(e.readAt);
    return [
      e.lost ? lostLine(statusText(s.status) || t("dsh.unknown"), read ? now.getTime() - read.getTime() : 0) : statusText(s.status),
      withTime && at ? agoText(at, now) : "",
      s.repo || baseName(s.cwd),
      s.archived ? t("dsh.archived") : "",
    ]
      .filter(Boolean)
      .join(" · ");
  };
}

function baseName(p: string): string {
  const parts = p.split(/[\\/]/).filter(Boolean);
  return parts[parts.length - 1] ?? "";
}

/** What Claude Code wrote of one session, newest page first; earlier pages on request. Lost: what was last read only. */
export function ClaudeTranscript({ entry, line }: { entry: ClaudeSessionEntry; line: string }) {
  const { t } = useTranslation("common");
  const s = entry.session!;
  const [events, setEvents] = useState<SessionEvent[]>([]);
  const [more, setMore] = useState(false);
  const [error, setError] = useState(s.error);
  const [read, setRead] = useState(false);
  const page = useCallback(
    (beforeSeq: bigint) =>
      local.readClaudeSession({ machineId: entry.machineId, sessionId: s.sessionId, beforeSeq, limit: pageSize, view: EventView.FULL }).then(
        (r) => {
          setEvents((old) => (beforeSeq > 0n ? [...r.events, ...old] : r.events));
          setMore(r.hasMoreBefore);
          setError(r.session?.error ?? "");
          setRead(true);
        },
        (e) => (setError(errorText(e)), setRead(true)),
      ),
    [entry.machineId, s.sessionId],
  );
  useEffect(() => {
    if (!entry.lost) page(0n);
  }, [page, entry.lost, s.lastActivityAt?.seconds]);
  const items = transcript(events, false);
  return (
    <>
      <div className="dsh-head">
        <div className="dsh-head-row">
          <h2>{s.title || s.sessionId}</h2>
          <MachineTag machineId={entry.machineId} />
        </div>
        <span>{[line, s.model, s.cwd].filter(Boolean).join(" · ")}</span>
        {/* Where an agent's message to it goes, once this machine's aide hooks reported it (docs/agent-messages.md §15.4). */}
        <span className={s.address ? "mono sel-text" : undefined}>{s.address ? t("claude.address", { address: s.address }) : t("claude.noAddress")}</span>
      </div>
      {entry.lost ? (
        <p className="dsh-empty">{t("dsh.lostNote", { name: entry.machineName || entry.machineId })}</p>
      ) : (
        <>
          {error && <p className="dsh-msg end err">{error}</p>}
          {more && (
            <button className="btn sm" onClick={() => events.length && page(events[0].seq)}>
              {t("dsh.earlier")}
            </button>
          )}
          {items.map((it) => {
            switch (it.kind) {
              case "user":
                return (
                  <div key={String(it.seq)} className="dsh-msg user">
                    {it.text}
                  </div>
                );
              case "agent":
                return (
                  <div key={String(it.seq)} className="dsh-msg">
                    {it.text}
                  </div>
                );
              case "tool":
                return <ToolRow key={String(it.seq)} call={it.call} result={it.result} />;
              default:
                return null;
            }
          })}
          {read && items.length === 0 && !error && <p className="dsh-empty">{t("dsh.empty")}</p>}
        </>
      )}
    </>
  );
}
