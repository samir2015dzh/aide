import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ConnectGuide, LinePicker, type Place, type Tab } from "../ConnectGuide";
import { useStore } from "../store";

/** What the guide opens goes to the main window: a task, or DeepSeek Harness's own sessions. */
const place: Place = {
  openSession: (id) => window.hermesh?.showInMain({ session: id }),
  openDSH: () => window.hermesh?.showInMain({ dsh: true }),
};

/** Connect: the guide for each coding agent, as the first run has it, and the assistants' connection (Access's Connect Agent…). */
export function Connect() {
  const { t } = useTranslation("connect");
  const ts = useTranslation("settings").t;
  const st = useStore();
  const [tab, setTab] = useState<Tab>("deepseek");
  return (
    <div className="form">
      <p className="cg-intro">{t("lede")}</p>
      <LinePicker tab={tab} set={setTab} />
      {tab === "assistants" ? (
        <div className="cg">
          <p className="cg-intro">{t("assistants.intro")}</p>
          <div className="cg-acts">
            <button className="btn primary" onClick={() => st.set({ sheet: { kind: "newGrant" } })}>
              {ts("grants.connect")}
            </button>
          </div>
        </div>
      ) : (
        <ConnectGuide line={tab} place={place} />
      )}
    </div>
  );
}
