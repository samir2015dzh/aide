import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useKeepLocalChecked } from "./agents";
import { parseAppLink } from "@aide/core";
import { agentName } from "@aide/ui";
import { local } from "./api";
import { Overview } from "./Overview";
import { HeldMessages } from "./held/HeldMessages";
import { TasksPage } from "./TasksPage";
import { BotPage } from "./bots/Bots";
import { RoomPage } from "./bots/Rooms";
import { SearchPalette } from "./Search";
import { SessionPage } from "./SessionPage";
import { SheetHost } from "./Sheets";
import { Rail } from "./Rail";
import { Sidebar, TaskColumn } from "./Sidebar";
import { useCommands } from "./commands";
import { useLeaving } from "./components";
import { Icon } from "./icons";
import { layoutOf, setSidebar, usePeekEnd } from "./layout";
import { keys, ownKeys, platform } from "./platform";
import { errorText, useStore } from "./store";

export function MainWindow() {
  const st = useStore();
  const { t } = useTranslation("common");
  useCommands();
  useKeepLocalChecked();
  // The window's material shows through the sidebar, macOS's or Windows 11's (the first-run pages stay opaque).
  useEffect(() => {
    if (!window.aide?.material) return;
    document.documentElement.classList.add("vibrant");
    return () => document.documentElement.classList.remove("vibrant");
  }, []);
  const session = st.selection.kind === "session" ? st.sessions.find((s) => s.sessionId === (st.selection as { id: string }).id) : undefined;
  // A selected session that is gone (e.g. its machine forgot it, or it was deleted): the overview.
  useEffect(() => {
    if (st.selection.kind !== "session" || !st.sessionsLoaded || session) return;
    if (st.moreSessions) st.loadOlderSessions();
    else st.set({ selection: { kind: "overview" } });
  }, [st, session]);
  // "Show in Main Window" in the settings window: the search palette, narrowed to a grant or a machine.
  const { set } = st;
  useEffect(
    () =>
      window.aide?.onCommand((c) => {
        if (c.startsWith("searchGrant:")) set({ searching: true, searchFor: { grant: c.slice("searchGrant:".length) } });
        if (c.startsWith("searchMachine:")) set({ searching: true, searchFor: { machine: c.slice("searchMachine:".length) } });
      }),
    [set],
  );
  // An agent's wake link (aide://wake?code=…, docs/agent-names.md §7.2): its setup, at the routine's URL and key.
  const clients = useRef(st.clients);
  clients.current = st.clients;
  const { show } = st;
  useEffect(
    () =>
      window.aide?.onLink((url) => {
        const l = parseAppLink(url);
        if (l?.kind !== "wake") return;
        local.openWakeLink({ code: l.code }).then((o) => {
          const c = clients.current.find((x) => x.clientId === o.clientId);
          set({ sheet: { kind: "wake", clientId: o.clientId, agent: c ? agentName(c.label) : o.clientId, code: l.code, name: o.name } });
        }, (e) => show(errorText(e)));
      }),
    [set, show],
  );
  // A peek ends when it fits again, when another session or page is chosen, and with Esc.
  usePeekEnd(st);
  useEffect(() => {
    set({ peek: null });
  }, [st.selection, set]);
  const peeking = st.peek !== null;
  useEffect(() => {
    if (!peeking) return;
    const esc = (e: KeyboardEvent) => e.key === "Escape" && !e.defaultPrevented && set({ peek: null });
    document.addEventListener("keydown", esc);
    return () => document.removeEventListener("keydown", esc);
  }, [peeking, set]);
  const { sidebar } = layoutOf(st);
  // The page of every session keeps its list beside the session opened from it (工单 #63: no list in the sidebar).
  const [fromList, setFromList] = useState(false);
  useEffect(() => {
    if (st.selection.kind === "sessions") setFromList(true);
    else if (st.selection.kind !== "session") setFromList(false);
  }, [st.selection]);
  const listed = st.selection.kind === "sessions" || (fromList && !!session);
  // The search, keyed by what it was opened for; closed, it fades out as it was.
  const search = useLeaving(st.searching ? JSON.stringify(st.searchFor) : null);
  return (
    <div className={`mainwrap ${sidebar === "docked" ? "" : "railed"} ${sidebar === "over" ? "peek" : ""} ${listed ? "listed" : ""}`}>
      {/* Fluent 40px 窗口标题栏（DESIGN-107 §0.2 / §0.5c #5A）：左侧菜单与侧栏切换，居中搜索框，右侧留窗口按钮区 */}
      <header className="win-titlebar side-top drag">
        <div className="win-titlebar-l">
          <div className="tl-room" aria-hidden="true" />
          <AppMenuButton />
          {sidebar === "docked" && (
            <button className="tb" onClick={() => setSidebar(st, false)} title={`${t("window.hideSidebar")} ${keys(ownKeys.toggleSidebar)}`} aria-label={t("window.hideSidebar")}>
              <Icon name="sidebar" />
            </button>
          )}
          <span className="win-titlebar-brand">Aide</span>
        </div>
        <button className="side-searchbox" onClick={() => st.set({ searching: true })} title={`${t("sidebar.search")} ${keys("CmdOrCtrl+K")}`} aria-label={t("sidebar.search")}>
          <Icon name="search" className="sm" />
          <span className="ph">{t("sidebar.searchHint")}</span>
          <span className="kbd">{keys("CmdOrCtrl+K")}</span>
        </button>
        <div className="win-titlebar-r" aria-hidden="true" />
      </header>
      {/* The sidebar, or its rail (docs/her-team.md §14.9): asked for while there is no room, it shows over the rail. */}
      {sidebar === "docked" ? <Sidebar /> : <Rail />}
      {sidebar === "over" && <Sidebar />}
      {/* Over the conversation, the sidebar goes again with a click beside it. */}
      {sidebar === "over" && <div className="peek-scrim" onClick={() => setSidebar(st, false)} />}
      {listed && <TaskColumn />}
      <section className="content">
        {st.reconnecting && <ReconnectingBanner />}
        {st.selection.kind === "sessions" ? (
          <div className="ctool drag"><div className="ctitle" /></div>
        ) : session ? (
          <SessionPage s={session} />
        ) : st.selection.kind === "held" ? (
          <HeldMessages />
        ) : st.selection.kind === "tasks" && !st.status?.machineOnly ? (
          <TasksPage />
        ) : st.selection.kind === "bot" && !st.status?.machineOnly ? (
          <BotPage key={st.selection.key} botKey={st.selection.key} native={st.selection.native} settings={st.selection.pane === "settings"} />
        ) : st.selection.kind === "room" && !st.status?.machineOnly ? (
          <RoomPage key={st.selection.roomId} machineId={st.selection.machineId} roomId={st.selection.roomId} />
        ) : (
          <Overview />
        )}
      </section>
      {search.shown !== null && <SearchPalette key={search.shown} leaving={search.leaving} />}
      <SheetHost />
      <ToastView />
    </div>
  );
}

/** Windows: the app menu, under its button (there is no menu bar; main/menu.ts). */
export function AppMenuButton() {
  const { t } = useTranslation("common");
  if (platform !== "win") return null;
  return (
    <button
      className="tb"
      aria-label={t("window.menu")}
      title={t("window.menu")}
      onClick={(e) => {
        const r = e.currentTarget.getBoundingClientRect();
        window.aide?.showAppMenu(r.left, r.bottom + 4);
      }}
    >
      <Icon name="menu" />
    </button>
  );
}

/**
 * What an action says (st.show, st.run's failures). It shows where the action was (工单 #61, the owner's rule): in
 * the sheet open over the window when there is one, under its buttons (inSheet), else at the window's foot.
 */
export function ToastView({ inSheet = false }: { inSheet?: boolean }) {
  const st = useStore();
  const { t } = useTranslation("common");
  const { shown: toast, leaving } = useLeaving(st.toast);
  if (!toast || (st.sheet !== null) !== inSheet) return null;
  return (
    <div className={`toast ${inSheet ? "in-sheet" : ""} ${leaving ? "leaving" : ""}`} inert={leaving} role="status">
      <Icon name="check" />
      <span>{toast.text}</span>
      {toast.undo && (
        <button className="btn sm" onClick={() => (st.set({ toast: null }), toast.undo?.())}>{t("window.undo")}</button>
      )}
    </div>
  );
}

/** In-place background service restart control inside the reconnecting banner (工单 #120). */
function ReconnectingBanner() {
  const { t } = useTranslation("common");
  const [restarting, setRestarting] = useState(false);
  const [err, setErr] = useState<{ reason: string; needsApproval?: boolean; canOpenLogs: boolean } | null>(null);
  const restart = async () => {
    if (restarting) return;
    setRestarting(true);
    setErr(null);
    try {
      const res = await window.aide?.restartDaemon();
      if (res && !res.ok) setErr({ reason: res.reason, needsApproval: res.needsApproval, canOpenLogs: res.canOpenLogs });
    } catch (e) {
      setErr({ reason: e instanceof Error ? e.message : String(e), canOpenLogs: true });
    } finally {
      setRestarting(false);
    }
  };
  return (
    <div className="link-banner" role="status">
      <i aria-hidden="true" />
      <span>{t("link.reconnecting")}</span>
      <button className="link-banner-btn" disabled={restarting} onClick={() => void restart()}>
        {restarting ? t("daemon.restarting") : t("daemon.restart")}
      </button>
      {err && (
        <>
          <span className="link-banner-err" role="alert" title={err.reason}>{err.reason}</span>
          {err.needsApproval && (
            <button className="link-banner-link" onClick={() => window.aide?.openLoginItems()}>{t("daemon.openLoginItems")}</button>
          )}
          {err.canOpenLogs && (
            <button className="link-banner-link" onClick={() => window.aide?.openDaemonLogs()}>{t("daemon.openLogs")}</button>
          )}
        </>
      )}
    </div>
  );
}
