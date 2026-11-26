import { useCallback, useEffect, useState } from "react";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import type { ListExecLogResponse } from "@aide/proto/local";
import { agoText, dateOf, opsCommandSummary, opsMillis, opsResult, type OpsCommandSummary, type OpsResult } from "@aide/core";
import { agentName } from "@aide/ui";
import { i18n } from "../../../shared/i18n";
import { aidedGone, dedupeLinkEntries, type LinkEntry } from "../../../shared/watchLink";
import { local } from "../api";
import { Icon } from "../icons";
import { errorText, useNow, useStore } from "../store";

/**
 * The ops log (docs/machine-ops.md): the commands agents ran on the
 * machines, or were refused, newest first, as each machine keeps them; their
 * output is kept nowhere. Read when the page opens, and again on Refresh.
 */
export function OpsLog() {
  const { t } = useTranslation("settings");
  const st = useStore();
  const now = useNow(60_000);
  const [log, setLog] = useState<ListExecLogResponse>();
  const [error, setError] = useState("");
  const [open, setOpen] = useState<Record<number, boolean>>({});
  const [copied, setCopied] = useState<number | null>(null);
  const load = useCallback(() => {
    local.listExecLog({}).then(
      (r) => (setLog(r), setError(""), setOpen({})),
      (e) => setError(errorText(e)),
    );
  }, []);
  // Up: aided answers and its watch is live, so a link restored (工单 #87) counts as aided back.
  const up = !!st.status && !st.reconnecting;
  useEffect(() => {
    if (up) load();
  }, [load, up]);
  // The links' own record (工单 #87), kept by the app: deduplicated across app/window watchers (#138 P15).
  const [links, setLinks] = useState<LinkEntry[]>([]);
  const loadLinks = useCallback(
    () => void window.aide?.linkLog().then((l) => setLinks(dedupeLinkEntries([...l].reverse())), () => {}),
    [],
  );
  useEffect(() => loadLinks(), [loadLinks, st.reconnecting]);

  const toggle = (i: number) => setOpen((prev) => ({ ...prev, [i]: !prev[i] }));
  const copy = (i: number, text: string) => {
    navigator.clipboard?.writeText(text).then(() => {
      setCopied(i);
      setTimeout(() => setCopied((cur) => (cur === i ? null : cur)), 1600);
    }, () => {});
  };

  return (
    <div className="form">
      {error && <p className="set-note warn">{error}</p>}
      <div className="ops-bar">
        <div className="fhead">{t("pages.ops")}</div>
        <button type="button" className="btn sm" onClick={() => (load(), loadLinks())}>
          <Icon name="retry" className="sm" />
          {t("ops.refresh")}
        </button>
      </div>
      {log && log.entries.length > 0 ? (
        <div className="fgroup" role="list" aria-label={t("pages.ops")}>
          {log.entries.map((e, i) => {
            const r = e.record!;
            const at = dateOf(r.startedAt);
            const secs = opsMillis(r) / 1000;
            const res = opsResult(r);
            const sum = opsCommandSummary(r.command);
            const isOpen = !!open[i];
            const hasDetail = sum.collapsible || !!r.workdir || !!r.reason;
            const sub = [
              agentName(r.clientLabel || r.clientId),
              e.machineName || e.machineId,
              at ? agoText(at, now) : "",
              secs ? t("ops.took", { seconds: secs < 10 ? secs.toFixed(1) : Math.round(secs) }) : "",
            ].filter(Boolean);
            const detailMeta = [
              at ? at.toLocaleString(i18n.language) : "",
              r.workdir ? t("ops.workdir", { dir: r.workdir }) : "",
              r.reason,
            ].filter(Boolean);
            const dotClass = res.kind === "ok" ? "on" : res.kind === "exit" || res.kind === "failed" ? "bad" : "warn";
            const outcomeClass = res.kind === "ok" ? "ok" : res.kind === "exit" || res.kind === "failed" ? "bad" : "warn";
            const scriptLabel = sum.collapsible && sum.shell !== "plain" ? shellLabel(sum.shell, sum.lineCount, t) : "";
            return (
              <div key={i} className="frow ops-row" role="listitem">
                <div className="ops-head">
                  <div className="ft">
                    <div className="ops-title">
                      <i className={`dotc ${dotClass}`} aria-hidden="true" />
                      {scriptLabel ? (
                        <span className="ops-summary">{scriptLabel}</span>
                      ) : (
                        <span className="mono ops-preview">{sum.preview}</span>
                      )}
                      <span className={`ops-outcome ${outcomeClass}`}>{outcomeText(res, t)}</span>
                    </div>
                    {scriptLabel && <div className="mono ops-preview-sub">{sum.preview}</div>}
                    <div className="fdesc">{sub.join(" · ")}</div>
                    {/* Why it was refused or failed stays on the row while it is folded, scripts too (审计 B); in full when open. */}
                    {!isOpen && r.reason && <div className="fdesc ops-reason" title={r.reason}>{r.reason}</div>}
                  </div>
                  {hasDetail && (
                    <button
                      type="button"
                      className="ops-toggle"
                      aria-expanded={isOpen}
                      onClick={() => toggle(i)}
                    >
                      {t(isOpen ? "ops.hideDetail" : "ops.showDetail")}
                    </button>
                  )}
                </div>
                {isOpen && (
                  <div className="ops-detail">
                    <pre className="ops-script sel-text">{r.command}</pre>
                    <div className="ops-detail-foot">
                      {detailMeta.length > 0 && <div className="fdesc ops-detail-meta">{detailMeta.join(" · ")}</div>}
                      <button type="button" className="btn sm" onClick={() => copy(i, r.command)}>
                        {t(copied === i ? "ops.copied" : "ops.copyCommand")}
                      </button>
                    </div>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      ) : (
        <div className="fgroup">
          <div className="frow muted">{log ? t("ops.none") : t("ops.loading")}</div>
        </div>
      )}
      {log && log.unreachable.length > 0 && (
        <p className="set-note warn">{t("ops.unreachable", { machines: log.unreachable.map((u) => u.machineName || u.machineId).join(t("listSeparator")) })}</p>
      )}
      <div className="fhead">{t("ops.link.head")}</div>
      <div className="fgroup" role="list" aria-label={t("ops.link.head")}>
        {links.length === 0 && <div className="frow muted">{t("ops.link.none")}</div>}
        {links.slice(0, 50).map((e, i) => {
          const at = new Date(e.at);
          return (
            <div key={i} className="frow ops-link-row" role="listitem">
              <div className="ft">
                <div className="ops-title">
                  <i className={`dotc ${e.kind === "back" ? "on" : "warn"}`} aria-hidden="true" />
                  <span>{linkText(e, t)}</span>
                </div>
                <div className="fdesc">{`${agoText(at, now)} (${at.toLocaleString(i18n.language)})`}</div>
              </div>
            </div>
          );
        })}
      </div>
      <p className="set-note">{t("ops.note")}</p>
    </div>
  );
}

function outcomeText(o: OpsResult, t: TFunction<"settings">): string {
  if (o.kind === "ok") return t("ops.okExit");
  if (o.kind === "exit") return t("ops.failExit", { code: o.code });
  return t(`ops.${o.kind}`);
}

function shellLabel(shell: OpsCommandSummary["shell"], lines: number, t: TFunction<"settings">): string {
  if (shell === "powershell") return t("ops.psScript", { lines });
  if (shell === "shell") return t("ops.shellScript", { lines });
  if (shell === "cmd") return t("ops.cmdScript", { lines });
  return t("ops.lines", { lines });
}

/** What an entry of the links' record says, with the suspected cause of a break and how long it lasted (工单 #87). */
function linkText(e: LinkEntry, t: TFunction<"settings">): string {
  if (e.kind === "back") {
    const s = Math.round((e.downFor ?? 0) / 1000);
    const took = s < 90 ? t("ops.link.seconds", { count: s }) : t("ops.link.minutes", { count: Math.round(s / 60) });
    return e.link === "relay" ? t("ops.link.relayBack", { took }) : t("ops.link.aidedBack", { took });
  }
  if (e.link === "relay") return t("ops.link.relayDown");
  return e.why === "hung" ? t("ops.link.aidedHung") : aidedGone(e.detail) ? t("ops.link.aidedGone") : t("ops.link.aidedLost");
}
