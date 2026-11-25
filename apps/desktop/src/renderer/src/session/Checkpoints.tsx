import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { SessionStatus, type ListCheckpointsResponse, type Session } from "@aide/proto/session";
import { clockText, dateOf, displayTitle } from "@aide/core";
import { local } from "../api";
import { Icon } from "../icons";
import { useStore } from "../store";
import { confirmRollback } from "./actions";

/**
 * Per-turn workspace checkpoints stored under hidden git refs (工单 #113).
 * Shows each turn's checkpoint (turn number, timestamp, diff summary), allows
 * paired conversation + workspace rollback ("回到这个点") when supported by
 * the provider, explains explicitly when disabled (non-git / in-place) or
 * refused (provider lacks conversation rollback), and lets the owner clear
 * retained checkpoints manually.
 */
export function Checkpoints({ s }: { s: Session }) {
  const { t } = useTranslation("session");
  const st = useStore();
  const [data, setData] = useState<ListCheckpointsResponse>();
  const [busy, setBusy] = useState(false);

  const load = useCallback(() => {
    let active = true;
    local.listSessionCheckpoints({ sessionId: s.sessionId }).then(
      (res) => active && setData(res),
      () => {},
    );
    return () => {
      active = false;
    };
  }, [s.sessionId]);

  useEffect(() => load(), [load, s.progress?.turnCount, s.status]);

  const lastTurn = s.progress?.turnCount ?? 0;
  const idle = s.status === SessionStatus.IDLE || s.status === SessionStatus.CRASHED;

  const clearAll = async () => {
    setBusy(true);
    try {
      await st.run(async () => {
        const res = await local.clearSessionCheckpoints({ sessionId: s.sessionId, keepLatest: 0 });
        setData(await local.listSessionCheckpoints({ sessionId: s.sessionId }));
        st.show(t("checkpoints.cleared", { count: res.removed }));
      });
    } finally {
      setBusy(false);
    }
  };

  return (
    <aside className="insp" aria-label={t("checkpoints.title")}>
      <h3 className="insp-h">{t("checkpoints.title")}</h3>
      {!data ? (
        <p className="dim">{t("checkpoints.loading")}</p>
      ) : !data.enabled ? (
        <p className="dim" data-testid="checkpoints-disabled">{data.disabledReason || t("checkpoints.disabled")}</p>
      ) : (
        <>
          {!data.canRollback && data.rollbackRefusedReason && (
            <p className="dim" data-testid="checkpoints-refused">{data.rollbackRefusedReason}</p>
          )}
          {data.checkpoints.length === 0 ? (
            <p className="dim">{t("checkpoints.empty")}</p>
          ) : (
            <ul className="insp-facts">
              {[...data.checkpoints].reverse().map((cp) => {
                const at = cp.createdAt ? dateOf(cp.createdAt) : undefined;
                const diff = cp.diffStat;
                const summary = diff && (diff.filesChanged > 0 || diff.insertions > 0 || diff.deletions > 0)
                  ? `+${diff.insertions} -${diff.deletions}`
                  : cp.changed ? t("checkpoints.changed") : t("checkpoints.unchanged");
                const canGo = data.canRollback && idle && cp.turn < lastTurn;
                return (
                  <li key={cp.ref || cp.turn} style={{ alignItems: "flex-start", justifyContent: "space-between", gap: 8 }}>
                    <div style={{ minWidth: 0, flex: 1 }}>
                      <div>
                        <b>{t("checkpoints.turn", { turn: cp.turn })}</b>
                        {at && <span className="dim"> · {clockText(at.getTime())}</span>}
                        <span className="dim"> · {summary}</span>
                      </div>
                      {cp.promptExcerpt && <div className="dim" title={cp.promptExcerpt}>{cp.promptExcerpt}</div>}
                    </div>
                    {cp.turn < lastTurn && (
                      <button
                        className="btn"
                        disabled={!canGo}
                        title={!data.canRollback ? data.rollbackRefusedReason : !idle ? t("checkpoints.waitIdle") : t("checkpoints.restore")}
                        onClick={() => confirmRollback(st, s.sessionId, displayTitle(s), cp.turn, lastTurn)}
                      >
                        {t("checkpoints.restore")}
                      </button>
                    )}
                  </li>
                );
              })}
            </ul>
          )}
          <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 8, marginTop: 8 }}>
            {data.policy && (
              <span className="dim">
                {t("checkpoints.policy", { count: data.policy.maxCount, days: data.policy.maxAgeDays })}
              </span>
            )}
            {data.checkpoints.length > 0 && (
              <button className="link" disabled={busy} onClick={clearAll}>
                <Icon name="trash" className="sm" /> {t("checkpoints.clear")}
              </button>
            )}
          </div>
        </>
      )}
    </aside>
  );
}
