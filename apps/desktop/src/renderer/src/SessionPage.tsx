import { useEffect, useRef, useState } from "react";
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
import { SessionList } from "./session/SessionList";
import "./session/markdown/markdown.css";
import "./session/session.css";

// A session: its conversation, the bottom control area while a turn is
// live, and beside it the sessions to switch to (工单 #90); its settings
// are a click away, in a menu from the toolbar.
export function SessionPage({ s }: { s: Session }) {
  const { t } = useTranslation("session");
  const st = useStore();
  const { events, draft } = useSessionEvents(s.sessionId, s.progress?.lastEventSeq ?? 0n);
  // The card keeps the conversation clear of it when there is room beside it; over the conversation (no room) it leaves it be.
  const { card } = layoutOf(st);
  const info = card !== "hidden";
  const [settings, setSettings] = useState(false);
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
        {/* The session's settings, in a menu (工单 #90): open only when asked for. */}
        <button
          className={`tb ${settings ? "on" : ""}`}
          title={t("page.settings")}
          aria-label={t("page.settings")}
          aria-haspopup="dialog"
          aria-expanded={settings}
          data-settings-toggle
          onClick={() => setSettings(!settings)}
        >
          <Icon name="gear" />
        </button>
        {/* A toolbar button as the sidebar's toggle is, highlighted while the list shows. */}
        <button
          className={`tb ${info ? "on" : ""}`}
          title={t(info ? "page.hideInfo" : "page.showInfo", { keys: keys(ownKeys.toggleInspector) })}
          aria-label={t("page.info")}
          aria-pressed={info}
          onClick={() => setCard(st, !info)}
        >
          <Icon name="sidebar" className="flip" />
        </button>
      </div>
      <div className={`dwrap ${card === "docked" ? "dock" : ""}`}>
        <div className="dmain">
          <Chat key={s.sessionId} s={s} events={events} draft={draft} />
        </div>
        {info && <SessionList s={s} />}
        {settings && <SettingsMenu s={s} onClose={() => setSettings(false)} />}
      </div>
    </>
  );
}

/**
 * The session's settings (工单 #90: every one the info panel had — who started it, which agent runs it, its machine
 * and that machine's settings, its project and branch with what they offer, the model and effort, its review), over
 * the conversation's top right until put away: Esc, a click outside, or the toolbar's button again.
 */
function SettingsMenu({ s, onClose }: { s: Session; onClose: () => void }) {
  const { t } = useTranslation("session");
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const outside = (e: MouseEvent) => {
      const el = e.target as Element;
      // Its own menus (the project's, the branch's) open over the window, outside it.
      if (!ref.current?.contains(el) && !el.closest?.("[data-settings-toggle], [role=menu]")) onClose();
    };
    const esc = (e: KeyboardEvent) => e.key === "Escape" && !e.defaultPrevented && onClose();
    document.addEventListener("mousedown", outside);
    document.addEventListener("keydown", esc);
    return () => {
      document.removeEventListener("mousedown", outside);
      document.removeEventListener("keydown", esc);
    };
  }, [onClose]);
  return (
    <div className="insp-pop" role="dialog" aria-label={t("page.settings")} ref={ref}>
      <Inspector s={s} />
    </div>
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
