import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { Client } from "@aide/proto/local";
import { agentName } from "@aide/ui";
import { grantIcon } from "@aide/core/avatar";
import { local } from "../api";
import { GrantFaceAt, grantFace, LookEditor } from "../components";
import type { Store } from "../store";

/**
 * Name and look, changed together and signed once: the name the owner knows an agent by, and its character (工单 #83)
 * — its seed's (自动), or the shape and colour picked in the one avatar editor.
 */
export function LookSheet({ c, st, close }: { c: Client; st: Store; close: () => void }) {
  const { t } = useTranslation("settings");
  const shown = agentName(c.label);
  const [name, setName] = useState(shown);
  const [icon, setIcon] = useState(c.icon);
  const [busy, setBusy] = useState(false);
  // An unchanged name keeps its label whole: "Cursor (cursor.com)" stays vouched for by its domain.
  const label = name.trim() === shown ? c.label : name.trim();
  const face = grantFace({ ...c, icon });
  const save = async () => {
    if (!name.trim() || busy) return;
    if (label === c.label && icon === c.icon) return close();
    setBusy(true);
    await st.run(() => local.renameClient({ grantId: c.grantId, label, icon, image: icon === "image" ? c.image : undefined })); // failures show as a toast
    close();
  };
  return (
    <>
      <div className="sheet-scrim" />
      <div className="sheet" role="dialog" aria-label={t("grants.renameTitle")} style={{ width: 460 }}>
        <h3>{t("grants.renameTitle")}</h3>
        <div className="gl-id">
          <GrantFaceAt c={{ ...c, icon }} size={52} />
          <label>
            {t("grants.agentName")}
            <input
              className="field"
              value={name}
              maxLength={64}
              autoFocus
              spellCheck={false}
              onChange={(e) => setName(e.target.value)}
              onKeyDown={(e) => (e.key === "Enter" ? save() : e.key === "Escape" && close())}
            />
          </label>
        </div>
        <div className="fhead gl-head">{t("grants.icon")}</div>
        <LookEditor seed={face.seed} look={face.look} onChange={(l) => setIcon(grantIcon(l))} />
        <p className="sheet-note muted">
          {t("grants.renameMessage")}
          {icon.startsWith("bot:") && (
            <>
              {" "}
              <button className="link" onClick={() => setIcon("")}>{t("grants.iconAuto")}</button>
            </>
          )}
        </p>
        <div className="sheet-foot">
          <button className="btn" onClick={close}>{t("cancel")}</button>
          <button className="btn primary" disabled={busy || !name.trim()} onClick={save}>{t("grants.renameButton")}</button>
        </div>
      </div>
    </>
  );
}
