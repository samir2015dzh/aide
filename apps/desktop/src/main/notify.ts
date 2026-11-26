import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-node";
import {
  applyWatch, codingAgentTitle, displayTitle, emptyWorld, filterNotices, noticeDiff, noticeSnapshots, pendingQuestion, questionAcks, quietSessions, roomNotices, roomSnapshots, t as coreT,
  withKnownState, type NoticeSnapshot, type RoomSnapshot,
} from "@aide/core";
import { LocalService, type Client, type GetStatusResponse, type Machine } from "@aide/proto/local";
import type { Session } from "@aide/proto/session";
import { app, nativeTheme, Notification, powerMonitor } from "electron";
import fs from "node:fs";
import path from "node:path";
import type { Command, Preferences } from "../shared/bridge";
import { i18n, languages, type Language } from "../shared/i18n";
import { healDelay, keepWatching } from "../shared/watchLink";
import { command } from "./ipc";
import { linkBack, linkDown } from "./linklog";
import { platform } from "./platform";
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

/** Updates cached status immediately when an on-demand restart probe confirms aided is back (工单 #120). */
export function noteDaemonRecovered(status: GetStatusResponse) {
  world.status = status;
  linkBack("aided", "app");
  changed();
}

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
    const q = n.kind === "question" ? pendingQuestion(world.sessions.find((s) => s.sessionId === n.sessionId)!) : undefined;
    if (q) told.add(q.questionId);
  }
  seen = noticeSnapshots(world.sessions);
  since ??= look;
  ackQuestions();
}

// The questions this desktop told the owner of (their ids), and what it
// tells their machines of them: that it did, and whether the owner is at
// it (docs/session-states.md §7). A phone holds a question back while a
// desktop has it, and stays silent once the owner was at one.
const told = new Set<string>();

/**
 * The owner is at this desktop: the system has had input within the last
 * minute and is not locked (AIDE_TEST_PRESENCE, "here" or "away", for tests).
 */
export function ownerPresent(): boolean {
  const test = process.env.AIDE_TEST_PRESENCE;
  if (test === "here" || test === "away") return test === "here";
  return powerMonitor.getSystemIdleState(60) === "active";
}

function ackQuestions() {
  const pending = new Set(world.sessions.map((s) => pendingQuestion(s)?.questionId).filter((q) => q !== undefined));
  for (const q of told) if (!pending.has(q)) told.delete(q);
  if (told.size === 0) return;
  const local = createClient(
    LocalService,
    createConnectTransport({ baseUrl: "http://aided", httpVersion: "1.1", useBinaryFormat: true, nodeOptions: { socketPath: socketPath() } }),
  );
  for (const a of questionAcks(world.sessions, told, ownerPresent())) local.markSessionSeen(a).catch((e) => console.warn("telling of a question:", e));
}

// Rooms (工单 #69): the owner's machines put the rooms they host in their status; a room its host told of anew, or with
// more unread, is posted, one notification a room (newer ones replace it), unless its page is in front.
let roomsSeen: Map<string, RoomSnapshot> | undefined;
let roomInFront = "";

/** The room whose page the main window shows (the page says so as it opens and closes). */
export function setRoomOpen(id: string) {
  roomInFront = id;
}

function noticeRooms() {
  const rooms = world.machines.flatMap((m) => m.status?.rooms ?? []);
  const win = mainWindow();
  const open = win && !win.isDestroyed() && win.isFocused() ? roomInFront : "";
  for (const n of roomNotices(roomsSeen, rooms, open)) post(`room-${n.roomId}`, n.title, n.body, `room:${n.hostMachineId}/${n.roomId}`);
  roomsSeen = roomSnapshots(rooms);
}

// Work handed out with no sign of life for two minutes (工单 #70): told once a turn.
const quietTold = new Set<string>();
function noticeQuiet() {
  for (const q of quietSessions(world.sessions, new Date(), quietTold)) {
    quietTold.add(q.key);
    const agent = codingAgentTitle(q.session.agent);
    post(`quiet-${q.session.sessionId}`, coreT("notice.quietTitle", { agent }), coreT("notice.quietBody", { title: displayTitle(q.session) }), `open:${q.session.sessionId}`);
  }
}

// --- Following aided ---

export function watch() {
  // The owner coming back to this desktop while a question waits: its machine hears of it.
  setInterval(ackQuestions, 10_000).unref();
  // Quiet is noticed with time passing, nothing changing.
  setInterval(noticeQuiet, 15_000).unref();
  let w = emptyWorld;
  let gone: ReturnType<typeof setTimeout> | undefined;
  // Kept up, watched again when it fails, ends or hangs (shared/watchLink.ts, 工单 #87); each break kept (main/linklog.ts).
  void keepWatching({
    signal: new AbortController().signal,
    open: (signal) => {
      w = emptyWorld;
      // Where aided is now: a pipe's name changes each time it starts (Windows).
      const local = createClient(
        LocalService,
        createConnectTransport({ baseUrl: "http://aided", httpVersion: "1.1", useBinaryFormat: true, nodeOptions: { socketPath: socketPath() } }),
      );
      // This process's pid: when aided saves power (no window, nothing running), the app's processes give back
      // their idle memory with its own (internal/owner AppHeader, docs/resident.md §1.2).
      return local.watch({}, { signal, headers: { "aide-app-pid": String(process.pid) } });
    },
    message: (m) => {
      const before = w;
      w = applyWatch(w, m);
      world.status = w.status ?? null;
      world.clients = w.clients;
      world.machines = w.machines;
      world.sessions = withKnownState(w.sessions, w.machines);
      // This computer's machine and the relay, once an owner is set up here.
      if (world.status?.initialized) {
        if (world.status.relayConnected) linkBack("relay", "app");
        else linkDown("relay", "app");
      }
      if (w.sessions !== before.sessions || w.machines !== before.machines) noticeSessions();
      if (w.machines !== before.machines && !world.status?.machineOnly) noticeRooms();
      if (w !== before) changed();
    },
    down: (why, detail) => {
      console.warn("aided watch:", why, detail);
      linkDown("aided", "app", why, detail);
      if (world.status !== null) {
        world.status = null;
        changed();
      }
      // Not back within half a minute: aided is started again, as the app does when it opens (platform.ensureDaemon),
      // and again while it stays down (healDelay).
      clearTimeout(gone);
      const heal = (attempt: number) => {
        gone = setTimeout(() => {
          platform.ensureDaemon(attempt);
          heal(attempt + 1);
        }, healDelay(attempt));
      };
      heal(0);
    },
    up: () => {
      clearTimeout(gone);
      linkBack("aided", "app");
    },
  });
}
