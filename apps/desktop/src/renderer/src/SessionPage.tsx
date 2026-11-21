import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { Session } from "@aide/proto/session";
import { displayTitle, openedNews } from "@aide/core";
import { local } from "./api";
import { Icon } from "./icons";
import { MachineTag } from "./MachineTag";
import { layoutOf, setCard } from "./layout";
import { keys, ownKeys } from "./platform";
import { useStore } from "./store";
import { Chat } from "./session/Chat";
import { useSessionEvents } from "./session/events";
import { Inspector } from "./session/Inspector";
import "./session/markdown/markdown.css";
import "./session/session.css";

// A session: its conversation, the bottom control area while a turn is
// live, and the info panel.
export function SessionPage({ s }: { s: Session }) {
  const { t } = useTranslation("session");
  const st = useStore();
  const { events, draft } = useSessionEvents(s.sessionId, s.progress?.lastEventSeq ?? 0n);
  // The card keeps the conversation clear of it when there is room beside it; over the conversation (no room) it leaves it be.
  const { card } = layoutOf(st);
  const info = card !== "hidden";
  useMarkSeen(s);
  return (
    <>
      <div className="ctool drag">
        {/* The title line is the session's name alone; its machine (several machines only) sits right after it, in the bar. */}
        <div className="ctitle-wrap">
          <div className="ctitle">
            <h2>{displayTitle(s)}</h2>
          </div>
          <MachineTag machineId={s.machineId} />
        </div>
        {/* A toolbar button as the sidebar's toggle is, highlighted while the info shows. */}
        <button
          className={`tb ${info ? "on" : ""}`}
          title={t(info ? "page.hideInfo" : "page.showInfo", { keys: keys(ownKeys.toggleInspector) })}
          aria-label={t("page.info")}
          aria-pressed={info}
          onClick={() => setCard(st, !info)}
        >
          <Icon name="infoCard" />
        </button>
      </div>
      <div className={`dwrap ${card === "docked" ? "dock" : ""}`}>
        <div className="dmain">
          <Chat key={s.sessionId} s={s} events={events} draft={draft} />
        </div>
        {info && <Inspector s={s} />}
      </div>
    </>
  );
}

/** Whether the window is in front of the owner: shown, and focused. */
function useInFront(): boolean {
  const now = () => document.visibilityState === "visible" && document.hasFocus();
  const [front, setFront] = useState(now);
  useEffect(() => {
    const look = () => setFront(now());
    window.addEventListener("focus", look);
    window.addEventListener("blur", look);
    document.addEventListener("visibilitychange", look);
    return () => {
      window.removeEventListener("focus", look);
      window.removeEventListener("blur", look);
      document.removeEventListener("visibilitychange", look);
    };
  }, []);
  return front;
}

/**
 * The session's page in front of the owner tells its machine what the owner has now seen (docs/session-states.md
 * §7): a finished turn leaves "new results", a question is the owner's own, on every device.
 */
function useMarkSeen(s: Session) {
  const front = useInFront();
  const news = openedNews(s);
  useEffect(() => {
    if (front && news !== "") local.markSessionSeen({ sessionId: s.sessionId, opened: true }).catch(() => {});
  }, [s.sessionId, news, front]);
}
