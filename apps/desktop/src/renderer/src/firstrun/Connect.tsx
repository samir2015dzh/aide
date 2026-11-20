import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ConnectGuide, LinePicker, type Place, type Tab } from "../ConnectGuide";
import { Assistants, useConnect } from "../connect";
import { Icon } from "../icons";
import { SheetHost } from "../Sheets";
import { useStore } from "../store";
import { Frame, StepIcon } from "./Frame";
import { setProgress } from "./progress";

// The first run's Connect step: a separate guide for each coding agent
// (DeepSeek Harness, Claude Desktop, Codex, any other ACP agent:
// ConnectGuide), and the assistants that give them tasks on their own tab,
// each logging in with the account and password itself (useConnect). The
// first task follows once an assistant connected.

export function Connect() {
  const { t } = useTranslation("firstrun");
  const tc = useTranslation("connect").t;
  const st = useStore();
  const c = useConnect();
  const [tab, setTab] = useState<Tab>("deepseek");
  const assistants = tab === "assistants";
  const first = c.arrived?.label;
  // A task the guide started, or DSH's own sessions: the first run ends there.
  const place: Place = {
    openSession: (id) => (setProgress(null), st.select(id)),
    openDSH: () => (setProgress(null), st.set({ selection: { kind: "dsh" } })),
  };
  const next = () => setProgress(first ? { step: "first", agent: first } : null);

  return (
    <Frame
      sheet={st.sheet?.kind === "addProject" || st.sheet?.kind === "scope" ? <SheetHost /> : undefined}
      foot={
        <>
          <p className="promise">
            <Icon name="shield" />
            {assistants ? t("connect.promise") : t("prepare.promise")}
          </p>
          {st.status?.localMachineEnabled && (
            <button className="btn" onClick={() => setProgress({ step: "prepare" })}>
              {t("back")}
            </button>
          )}
          {assistants && !first ? (
            <button className="link" onClick={next}>
              {t("connect.later")}
            </button>
          ) : (
            <button className={first ? "btn primary" : "btn"} onClick={next}>
              {t("firstTask.enter")}
            </button>
          )}
        </>
      }
    >
      <StepIcon name="link" />
      <h2 className="ob-h">{tc("title")}</h2>
      <p className="ob-p">{tc("lede")}</p>
      <LinePicker tab={tab} set={setTab} />
      {assistants ? (
        <>
          {!first && <p className="ob-p">{tc("assistants.intro")}</p>}
          <Assistants c={c} />
        </>
      ) : (
        <ConnectGuide line={tab} place={place} />
      )}
    </Frame>
  );
}
