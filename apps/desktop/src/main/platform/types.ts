import type { BrowserWindow, BrowserWindowConstructorOptions, Rectangle } from "electron";
import type { LoginItem, TerminalCommand } from "../../shared/bridge";

/** The kinds of window, for their frames. */
export type WindowKind = "main" | "onboarding" | "settings" | "auxiliary" | "panel";

/**
 * What differs between the systems the app runs on (docs/windows.md §5.1):
 * one implementation each, chosen once at start (platform/index.ts).
 */
export interface Platform {
  /** The pages' data-platform token. */
  token: "mac" | "win";
  /** The installed app runs aided itself; development and test runs use a daemon of their own. */
  installed: boolean;
  /** The window's material shows through the sidebar: macOS's vibrancy, Windows 11's Mica. */
  material: boolean;
  /** True when the app may start here; otherwise it has moved, or is quitting. */
  settle(): Promise<boolean>;
  /** The installer started this run to set the app up or remove it (Windows): it does that and quits. */
  installerEvent(): boolean;
  /** Takes aide:// links (sign-in, join and wake). */
  registerLinks(): void;
  /** Makes aided start by itself, and start now if it does not run. */
  registerDaemon(): void;
  /** aided stopped answering (工单 #87): started again if it does not run, as when the app opens; nothing if it runs. */
  ensureDaemon(): void;
  /** Restarts aided (after an update brought another version). */
  restartDaemon(why: string): void;
  /** Restarts aided on demand (工单 #120) and rejects with the real reason if starting/restarting it fails. */
  restartDaemonNow(why: string): Promise<void>;
  /** The owner turned aided's start off in the system's settings. */
  daemonNeedsApproval(): boolean;
  /** Opening the app at login. */
  loginItem(): LoginItem | Promise<LoginItem>;
  setLoginItem(on: boolean): LoginItem | Promise<LoginItem>;
  /** This run was started at login: the app lives in the menu bar or notification area only. */
  openedAtLogin(): boolean;
  /** The system's settings for what starts at login. */
  openLoginItems(): void;
  /** Runs one of a fixed set of commands in a terminal window of the system's. */
  runInTerminal(c: TerminalCommand, command: string): void;
  /** Opens a folder in the system's file browser (Finder, File Explorer) or a terminal there; anything but a folder, not at all. */
  openFolder(dir: string, to: "files" | "terminal"): void;
  /** Opens the system's list of processes (Activity Monitor, Task Manager). */
  openProcessMonitor(): void;
  /** This computer's name, as the person sees it. */
  computerName(): Promise<string>;
  /** A window's frame: its title bar and material. */
  frame(kind: WindowKind): BrowserWindowConstructorOptions;
  /** Places the window's own buttons for its mode (macOS's traffic lights). */
  placeButtons(win: BrowserWindow, kind: WindowKind): void;
  /** Moves the window to bounds, animated. */
  grow(win: BrowserWindow, bounds: Rectangle): void;
}
