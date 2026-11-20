import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { AboutWindow, UpdatesWindow, WhatsNewWindow } from "./update";
import { FirstRun, useFirstRunPending } from "./FirstRun";
import { Mark } from "@aide/ui";
import { Upgrade } from "./firstrun/SignIn";
import { MainWindow } from "./MainWindow";
import { SettingsWindow } from "./Settings";
import { AideProvider, useStore } from "./store";
import { TrayPanel } from "./TrayPanel";

export function App() {
  // The menu bar panel gets what it shows from the main process, not from aided.
  if (location.hash === "#tray") return <TrayPanel />;
  if (location.hash === "#updates") return <UpdatesWindow />;
  if (location.hash === "#about") return <AboutWindow />;
  if (location.hash === "#whats-new") return <WhatsNewWindow />;
  const settings = location.hash.startsWith("#settings");
  return <AideProvider>{settings ? <SettingsWindow /> : <Root />}</AideProvider>;
}

/** First run until this device belongs to an owner and its first-run pages are done, then the main window. */
function Root() {
  const st = useStore();
  const firstRun = useFirstRunPending();
  // A machine only has the main window too; becoming one of the owner's devices too, logging in again.
  const member = st.status?.initialized === true || st.status?.machineOnly === true;
  const upgrading = st.upgrading && st.status?.machineOnly === true;
  const ready = member && !st.status?.removed && !firstRun && !upgrading;
  // The window takes its size once aided answers, and keeps it if it stops answering later.
  useEffect(() => {
    if (st.status) window.aide?.setWindowMode(ready ? "main" : "onboarding");
  }, [ready, st.status]);
  if (st.status === undefined) return <div className="empty drag"><Mark size={40} variant="seq" /></div>;
  if (st.status === null) return <DaemonUnavailable />;
  if (upgrading)
    return <Upgrade relay={st.status.relayUrl} cancel={() => st.set({ upgrading: false })} />;
  return ready ? <MainWindow /> : <FirstRun />;
}

function DaemonUnavailable() {
  const { t } = useTranslation("common");
  // The installed app's background service waits until the person allows it in System Settings › Login Items.
  const [approval, setApproval] = useState(false);
  // It usually answers within moments of starting; the explanation only if it doesn't.
  const [late, setLate] = useState(false);
  useEffect(() => {
    window.aide?.daemonNeedsApproval().then(setApproval);
    const t = setTimeout(() => setLate(true), 8000);
    return () => clearTimeout(t);
  }, []);
  return (
    <div className="empty drag" style={{ height: "100%" }}>
      <Mark size={40} variant="wait" />
      <b>{t("daemon.connecting")}</b>
      {approval ? (
        <>
          <span>{t("daemon.needsApproval")}</span>
          <button className="btn" onClick={() => window.aide?.openLoginItems()}>{t("daemon.openLoginItems")}</button>
        </>
      ) : (
        late && <span>{t("daemon.noResponse")}</span>
      )}
    </div>
  );
}
