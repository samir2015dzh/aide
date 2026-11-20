import { useTranslation } from "react-i18next";
import { Assistants, useConnect } from "../connect";
import "../firstrun/firstrun.css";
import { useStore } from "../store";

/**
 * Connect Agent…: the assistants' line of the Connect guide in a sheet. Pick
 * the assistant, copy what it needs; it logs in with the account and
 * password itself, and shows here once it did (useConnect).
 */
export function NewGrantSheet() {
  const { t } = useTranslation("sheets");
  const tc = useTranslation("connect").t;
  const st = useStore();
  const c = useConnect();
  return (
    <>
      <h3 id="sheet-title">{t("newGrant.title")}</h3>
      {!c.arrived && <p className="sub">{tc("assistants.intro")}</p>}
      <Assistants c={c} size={40} />
      <div className="sheet-foot">
        <button className="btn" onClick={() => st.set({ sheet: null })}>
          {c.arrived ? t("done") : t("cancel")}
        </button>
      </div>
    </>
  );
}
