import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Assistants, useConnect } from "../connect";
import { ConnectGuide, LinePicker, type Place, type Tab } from "../ConnectGuide";

/** What the guide opens goes to the main window: a task, or DeepSeek Harness's own sessions. */
const place: Place = {
  openSession: (id) => window.hermesh?.showInMain({ session: id }),
  openDSH: () => window.hermesh?.showInMain({ dsh: true }),
};

/** Connect: the guide for each coding agent, as the first run has it, and the assistants' (line: the one to open on). */
export function Connect({ line }: { line?: Tab }) {
  const { t } = useTranslation("connect");
  const [tab, setTab] = useState<Tab>(line ?? "deepseek");
  const c = useConnect();
  return (
    <div className="form">
      <p className="cg-intro">{t("lede")}</p>
      <LinePicker tab={tab} set={setTab} />
      {tab === "assistants" ? (
        <div className="cg">
          {!c.arrived && <p className="cg-intro">{t("assistants.intro")}</p>}
          <Assistants c={c} size={40} />
        </div>
      ) : (
        <ConnectGuide line={tab} place={place} />
      )}
    </div>
  );
}
