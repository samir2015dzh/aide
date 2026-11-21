import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { Session, SessionInsights } from "@aide/proto/session";
import { local } from "../api";
import { Icon } from "../icons";
import { copy } from "../settings/common";
import { useStore } from "../store";
import { Markdown } from "./Markdown";
import { Dialog } from "./markdown/Dialog";

/**
 * The session's review (docs/session-insights.md), which its machine works
 * out from the session's events when asked: shown as the Markdown that goes
 * into a delivery record, to copy whole, or just the better prompt it suggests.
 */
export function Insights({ s }: { s: Session }) {
  const { t } = useTranslation("session");
  const st = useStore();
  const [shown, setShown] = useState<SessionInsights>();
  const open = () => st.run(async () => setShown(await local.getSessionInsights({ sessionId: s.sessionId })));
  return (
    <aside className="insp" aria-label={t("insights.title")}>
      <h3 className="insp-h">{t("insights.title")}</h3>
      <ul className="insp-facts">
        <li>
          <button className="insp-pick" onClick={open}>
            <Icon name="checklist" />
            <span>{t("insights.show")}</span>
          </button>
        </li>
      </ul>
      {shown && (
        <Dialog label={t("insights.title")} width={760} onClose={() => setShown(undefined)}>
          <div className="insights">
            <Markdown text={shown.markdown} mode="document" />
            <div className="insights-actions">
              <button className="btn" onClick={() => copy(st, shown.suggestedPrompt)}>{t("insights.copyPrompt")}</button>
              <button className="btn" onClick={() => copy(st, shown.markdown)}>{t("insights.copy")}</button>
              <button className="btn" onClick={() => setShown(undefined)}>{t("insights.close")}</button>
            </div>
          </div>
        </Dialog>
      )}
    </aside>
  );
}
