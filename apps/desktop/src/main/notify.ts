import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-node";
import { applyWatch, emptyWorld, filterNotices, herNotices, herSnapshots, noticeDiff, noticeSnapshots, withKnownState, type HerSnapshot, type NoticeSnapshot } from "@aide/core";
import { LocalService, type Client, type GetStatusResponse, type Machine } from "@aide/proto/local";
import type { Session } from "@aide/proto/session";
import { app, nativeTheme, Notification } from "electron";
import fs from "node:fs";
import path from "node:path";
import type { Command, Preferences } from "../shared/bridge";
import { i18n, languages, type Language } from "../shared/i18n";
import { command } from "./ipc";
import { socketPath } from "./paths";
import { mainWindow } from "./windows";

// What reaches the owner while no window may be open: the main process
// follows aided itself (LocalService.Watch: everything once, then each
// change, no polling), posts notifications for what changed, and keeps what
// it saw for the menu bar.

export type World = {
  /** null: aided cannot be reached. */
  status: GetStatusResponse | null;
  sessions: Session[];
  clients: Client[];
  machines: Machine[];
};
export const world: World = { status: null, sessions: [], clients: [], machines: [] };
export const worldListeners = new Set<() => void>();
const changed = () => worldListeners.forEach((f) => f());

// --- Preferences (the settings window sets them) ---

const defaults: Preferences = { appearance: "system", notifyStarted: true, notifyQuestion: true, notifyFailed: true, notifyTurnDone: false, menuBadge: true, language: "system" };
let prefs: Preferences | undefined;
const prefsFile = () => path.join(app.getPath("userData"), "preferences.json");

export function preferences(): Preferences {
  if (!prefs) {
    try {
      prefs = { ...defaults, ...JSON.parse(fs.readFileSync(prefsFile(), "utf8")) };
    } catch {
      prefs = { ...defaults };
    }
  }
  return prefs!;
}

/** Every window (and the native menus) in the chosen appearance: CSS follows it through prefers-color-scheme. */
export function applyAppearance() {
  nativeTheme.themeSource = preferences().appearance;
}

export function setPreferences(p: Partial<Preferences>): Preferences {
  const next = { ...preferences() };
  for (const k of ["notifyStarted", "notifyQuestion", "notifyFailed", "notifyTurnDone", "menuBadge"] as const) if (typeof p[k] === "boolean") next[k] = p[k];
  if (p.language === "system" || languages.includes(p.language as Language)) next.language = p.language!;
  if (p.appearance === "system" || p.appearance === "light" || p.appearance === "dark") next.appearance = p.appearance;
  prefs = next;
  applyAppearance();
  fs.mkdirSync(path.dirname(prefsFile()), { recursive: true });
  fs.writeFileSync(prefsFile(), JSON.stringify(next));
  changed();
  return next;
}

// --- Notifications ---

export type Posted = { id: string; title: string; body: string; click(): void };
/** Notifications that can still be clicked, newest last (tests read them). */
export const posted: Posted[] = [];
const shown = new Map<string, Notification>();

function post(id: string, title: string, body: string, open: Command, silent = false) {
  withdraw(id);
  const n = new Notification({ id, title, body, silent });
  const p: Posted = { id, title, body, click: () => command(open) };
  n.on("click", p.click);
  // Kept, so their clicks still arrive; the oldest go.
  shown.set(id, n);
  posted.push(p);
  if (posted.length > 50) withdraw(posted[0].id);
  n.show();
}

function withdraw(id: string) {
  shown.get(id)?.close();
  shown.delete(id);
  const i = posted.findIndex((x) => x.id === id);
  if (i >= 0) posted.splice(i, 1);
}

// Sessions as they were at the last look; none before the first. A session
// seen for the first time only tells about its question, so a question
// still waiting when the app starts is notified again (macOS took the old
// notification away when the app quit); failures and finished turns from
// before are no news. A session not seen before is new when it was created
// after the first look (since).
let seen = new Map<string, NoticeSnapshot>();
let since: Date | undefined;

function noticeSessions() {
  // A machine only does not know the owner's agents by name.
  const unnamed = i18n.t(world.status?.machineOnly ? "main:anAgent" : "main:disconnectedGrant");
  const grant = (s: Session) => (s.createdBy === "owner" ? i18n.t("main:you") : world.clients.find((c) => c.clientId === s.createdBy)?.label ?? unnamed);
  const look = new Date();
  for (const n of filterNotices(noticeDiff(seen, world.sessions, grant, since), preferences())) {
    post(`session-${n.kind}-${n.sessionId}`, n.title, n.body, `open:${n.sessionId}`, n.kind === "turnDone" || n.kind === "started");
  }
  seen = noticeSnapshots(world.sessions);
  since ??= look;
}

// What Her's assistants send (docs/her-server.md §9.3), from each machine's
// Her as Watch brings it: notified while the main window is not in front,
// unless the assistant is muted, and clicked, opening the Her entry at it.
// An environment whose messages were all read, here or on another device,
// takes its notifications back. Nothing before the first look is news.
let herSeen: Map<string, HerSnapshot> | undefined;

function noticeHer() {
  if (herSeen) {
    const looking = mainWindow()?.isFocused() ?? false;
    for (const n of herNotices(herSeen, world.machines)) {
      if (!looking) post(`her-${n.machineId}-${n.botId}`, n.title, n.body, `her:${n.machineId}`);
    }
    for (const m of world.machines) {
      if (m.her?.enabled && m.her.unread === 0) for (const p of posted.filter((x) => x.id.startsWith(`her-${m.machineId}-`))) withdraw(p.id);
    }
  }
  herSeen = herSnapshots(world.machines);
}

// --- Following aided ---

export function watch() {
  (async () => {
    for (;;) {
      let w = emptyWorld;
      // Where aided is now: a pipe's name changes each time it starts (Windows).
      const local = createClient(
        LocalService,
        createConnectTransport({ baseUrl: "http://aided", httpVersion: "1.1", useBinaryFormat: true, nodeOptions: { socketPath: socketPath() } }),
      );
      try {
        // This process's pid: when aided saves power (no window, nothing running), the app's processes give back
        // their idle memory with its own (internal/owner AppHeader, docs/resident.md §1.2).
        for await (const m of local.watch({}, { headers: { "aide-app-pid": String(process.pid) } })) {
          const before = w;
          w = applyWatch(w, m);
          world.status = w.status ?? null;
          world.clients = w.clients;
          world.machines = w.machines;
          world.sessions = withKnownState(w.sessions, w.machines);
          if (w.sessions !== before.sessions || w.machines !== before.machines) noticeSessions();
          if (w.machines !== before.machines) noticeHer();
          if (w !== before) changed();
        }
      } catch (e) {
        console.warn("aided watch:", e);
      }
      // aided restarted, or this watch fell behind: watched again.
      if (world.status !== null) {
        world.status = null;
        changed();
      }
      await new Promise((r) => setTimeout(r, 1000));
    }
  })();
}
