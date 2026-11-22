import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { ListExecLogResponse } from "@aide/proto/local";
import { agoText, dateOf, opsMillis, opsResult } from "@aide/core";
import { agentName } from "@aide/ui";
import { i18n } from "../../../shared/i18n";
import { local } from "../api";
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
  const load = useCallback(() => {
    local.listExecLog({}).then(
      (r) => (setLog(r), setError("")),
      (e) => setError(errorText(e)),
    );
  }, []);
  const up = !!st.status;
  useEffect(() => {
    if (up) load();
  }, [load, up]);
  const result = (r: Parameters<typeof opsResult>[0]) => {
    const o = opsResult(r);
    if (o.kind === "ok") return <span className="fv-side">{t("ops.exit", { code: 0 })}</span>;
    if (o.kind === "exit") return <span className="fv-side warn">{t("ops.exit", { code: o.code })}</span>;
    return <span className="fv-side warn">{t(`ops.${o.kind}`)}</span>;
  };
  return (
    <div className="form">
      {error && <p className="set-note warn">{error}</p>}
      {log && log.entries.length > 0 ? (
        <div className="fgroup" role="list" aria-label={t("pages.ops")}>
          {log.entries.map((e, i) => {
            const r = e.record!;
            const at = dateOf(r.startedAt);
            const secs = opsMillis(r) / 1000;
            const sub = [
              agentName(r.clientLabel || r.clientId),
              e.machineName || e.machineId,
              at ? `${agoText(at, now)} (${at.toLocaleString(i18n.language)})` : "",
              secs ? t("ops.took", { seconds: secs < 10 ? secs.toFixed(1) : Math.round(secs) }) : "",
              r.workdir,
            ].filter((s) => s);
            return (
              <div key={i} className="frow" role="listitem">
                <div className="ft">
                  <div className="mono sel-text">{r.command}</div>
                  <div className="fdesc">{sub.join(" · ")}</div>
                  {r.reason && <div className="fdesc">{r.reason}</div>}
                </div>
                {result(r)}
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
      <div className="fgroup">
        <button className="frow act" onClick={load}>{t("ops.refresh")}</button>
      </div>
      <p className="set-note">{t("ops.note")}</p>
    </div>
  );
}
