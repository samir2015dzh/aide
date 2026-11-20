import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-node";
import { LocalService } from "@hermesh/proto/local";
import { app, BrowserWindow, nativeTheme, shell } from "electron";
import { execFile, execFileSync, spawn } from "node:child_process";
import fs from "node:fs";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import { promisify } from "node:util";
import type { LoginItem } from "../../shared/bridge";
import { i18n } from "../../shared/i18n";
import { newer } from "../../shared/update";
import { dataDir, socketPath, testDataDir } from "../paths";
import type { Platform, WindowKind } from "./types";

// Setup.exe (Squirrel.Windows, docs/windows.md §6.1) installs the app for
// this user into %LOCALAPPDATA%\HermeshApp: a folder per version, app-<version>,
// with Hermesh.exe and Update.exe above them, which start the newest one.
// Each version copies hermeshd, hermeshdw and hermesh, and her beside them,
// where they stay, the folder install.ps1 uses, on PATH; hermeshd's logon task runs hermeshdw
// from there (hermeshd service install --owner). Open Hermesh at login is
// the user's Run value, which Startup apps lists and switches. Development
// and test runs use a daemon of their own and touch none of it.
const root = path.resolve(path.dirname(process.execPath), "..");
const updateExe = path.join(root, "Update.exe");
const installed = app.isPackaged && process.platform === "win32" && !process.env.AIDE_DATA_DIR && !process.env.HERMESH_DATA_DIR && !testDataDir && fs.existsSync(updateExe);
/** Hermesh.exe above the versions, which starts the newest: for links, shortcuts and login. */
const stub = path.join(root, "Hermesh.exe");
let devLoginItem = false;
/** Opened at login, the app gets this argument: in the notification area only. */
const atLogin = { path: stub, args: ["--hermesh-at-login"] };
// The app's name to Windows: its Start menu shortcut and its notifications carry it.
const appId = "com.hermesh.app";
if (installed) app.setAppUserModelId(appId);
/** The Start menu's shortcut, at the top of All apps (Squirrel's own would be in a folder). */
const startMenu = path.join(process.env.APPDATA || path.join(os.homedir(), "AppData", "Roaming"), "Microsoft", "Windows", "Start Menu", "Programs");
const shortcut = path.join(startMenu, "Aide.lnk");
/** The shortcut's name before the app was called Aide (RENAME-AIDE.md D9): moved to the new one on update. */
const oldShortcut = path.join(startMenu, "Hermesh.lnk");

// The app's programs, beside Hermesh.exe (package-win.mjs), and where they stay.
const program = (name: string) => path.join(path.dirname(process.execPath), "bin", name);
const binDir = path.join(process.env.LOCALAPPDATA || path.join(os.homedir(), "AppData", "Local"), "Programs", "Hermesh", "bin");
const hermeshd = path.join(binDir, "hermeshd.exe");

/** Whether hermeshd answers on its pipe now. */
function answers(): Promise<boolean> {
  return new Promise((resolve) => {
    const c = net.connect(socketPath());
    const done = (ok: boolean) => {
      c.destroy();
      resolve(ok);
    };
    c.once("connect", () => done(true));
    c.once("error", () => done(false));
    c.setTimeout(2000, () => done(false));
  });
}

/**
 * Copies this version's hermeshd, hermeshdw and hermesh to binDir, the first
 * and last also as aided and aide (the names they have since the app was
 * called Aide; the old ones stay, for the logon task, agents' hooks and
 * scripts: RENAME-AIDE.md B1, B2), and her
 * with what is hers where the app has it (package-win.mjs), unless they are
 * there already. A running program can be renamed, not overwritten: it
 * moves aside, and goes once nothing runs it.
 */
function placePrograms() {
  fs.mkdirSync(binDir, { recursive: true });
  for (const f of fs.readdirSync(binDir)) {
    if (f.endsWith(".old")) fs.rm(path.join(binDir, f), { recursive: true, force: true }, () => {});
  }
  const hers = ["her.exe", "agent-desktop.exe", "pdfium.dll"].filter((name) => fs.existsSync(program(name)));
  const names: [string, string][] = ["hermeshd.exe", "hermeshdw.exe", "hermesh.exe", ...hers].map((n) => [n, n]);
  names.push(["hermeshd.exe", "aided.exe"], ["hermesh.exe", "aide.exe"]);
  for (const [name, as] of names) {
    const from = fs.statSync(program(name));
    const to = path.join(binDir, as);
    const there = fs.statSync(to, { throwIfNoEntry: false });
    if (there && there.size === from.size && there.mtimeMs === from.mtimeMs) continue;
    if (there) fs.renameSync(to, `${to}.${Date.now()}.old`);
    fs.copyFileSync(program(name), to);
    fs.utimesSync(to, from.atime, from.mtime);
  }
  // Her's console, a folder hermeshd serves from beside her: replaced whole.
  const from = program("her-console");
  const to = path.join(binDir, "her-console");
  if (fs.existsSync(from) && filesOf(from) !== filesOf(to)) {
    if (fs.existsSync(to)) fs.renameSync(to, `${to}.${Date.now()}.old`);
    fs.cpSync(from, to, { recursive: true, preserveTimestamps: true });
  }
}

/** The files under dir, with their sizes and times: equal for a folder and its copy. */
function filesOf(dir: string): string {
  if (!fs.existsSync(dir)) return "";
  return (fs.readdirSync(dir, { recursive: true }) as string[])
    .sort()
    .map((f) => {
      const s = fs.statSync(path.join(dir, f));
      return s.isFile() ? `${f} ${s.size} ${s.mtimeMs}` : f;
    })
    .join("\n");
}

/** Registers hermeshd's logon task, the owner's and this PC's machine, and starts it. */
function installDaemon() {
  execFile(hermeshd, ["service", "install", "--owner"], { windowsHide: true }, (err) => err && console.warn("hermeshd's logon task:", err));
}

/** binDir on the user's PATH, or not, as install.ps1 writes it; running programs are told. */
function setPath(on: boolean) {
  execFileSync(program("hermeshd.exe"), ["user-path", on ? "add" : "remove", binDir], { windowsHide: true, timeout: 5_000 });
}

/** What Setup's events did, in the data folder's logs (nobody sees the app's output then). */
function setupLog(line: string) {
  try {
    fs.mkdirSync(path.join(dataDir(), "logs"), { recursive: true });
    fs.appendFileSync(path.join(dataDir(), "logs", "setup.log"), `${new Date().toISOString()} ${line}\n`);
  } catch {
    // Not written: no worse than before.
  }
}

/** Runs a step of Setup's, and logs how it went: one failing doesn't stop the others. */
function attempt(what: string, f: () => unknown) {
  const start = Date.now();
  try {
    const r = f();
    setupLog(`${what}: ${r === false ? "not done" : "done"} (${Date.now() - start} ms)`);
  } catch (e) {
    setupLog(`${what}: ${e instanceof Error ? e.message : String(e)} (${Date.now() - start} ms)`);
  }
}

/**
 * Setup.exe and Update.exe start the app with an event (install, updated,
 * uninstall, obsolete) and give it 15 s (10 to uninstall), before anything
 * else; it does what the event asks, and the app quits. The data folder stays on uninstall, as a Mac
 * app's does, and so does the signer in Program Files (only an
 * administrator removes it).
 */
function installerEvent(): boolean {
  const event = installed ? process.argv[1] : undefined;
  if (event?.startsWith("--squirrel-") && event !== "--squirrel-firstrun") setupLog(`${event} ${app.getVersion()}`);
  switch (event) {
    case "--squirrel-install":
      attempt("the Start menu", () => shell.writeShortcutLink(shortcut, { target: stub, appUserModelId: appId, description: "Aide" }));
      attempt("PATH", () => setPath(true));
      break;
    case "--squirrel-updated":
      // Hermesh.lnk becomes Aide.lnk, with the same app id: pinned and grouped as before. One the owner removed stays removed.
      if (fs.existsSync(oldShortcut)) {
        attempt("the Start menu", () => {
          shell.writeShortcutLink(shortcut, { target: stub, appUserModelId: appId, description: "Aide" });
          fs.rmSync(oldShortcut, { force: true });
        });
      }
      attempt("PATH", () => setPath(true));
      break;
    case "--squirrel-uninstall":
      // The app running (Squirrel ends nothing), its files could not go. Its
      // helpers go with it: one already gone is no failure.
      attempt("the app", () => {
        const list = execFileSync("tasklist.exe", ["/FO", "CSV", "/NH", "/FI", "IMAGENAME eq Hermesh.exe"], { encoding: "utf8", windowsHide: true, timeout: 10_000 });
        for (const [, pid] of list.matchAll(/^"Hermesh\.exe","(\d+)"/gim)) {
          if (Number(pid) === process.pid) continue;
          try {
            process.kill(Number(pid));
          } catch {
            // Gone already.
          }
        }
      });
      attempt("hermeshd's logon task", () => execFileSync(hermeshd, ["service", "uninstall"], { windowsHide: true, timeout: 5_000 }));
      attempt("links", () => app.removeAsDefaultProtocolClient("hermesh", stub, []));
      attempt("open at login", () => app.setLoginItemSettings({ ...atLogin, openAtLogin: false }));
      attempt("the Start menu", () => [shortcut, oldShortcut].forEach((f) => fs.rmSync(f, { force: true })));
      attempt("PATH", () => setPath(false));
      // Last: hermeshd, just stopped, lets go of its files within moments.
      attempt("the programs", () => fs.rmSync(binDir, { recursive: true, force: true, maxRetries: 8 }));
      break;
    case "--squirrel-obsolete":
      break;
    default:
      return false;
  }
  return true;
}

/** Open at login: on, and whether the owner turned it off in Startup apps. */
function loginItem(): LoginItem {
  const s = app.getLoginItemSettings(atLogin);
  return { on: s.openAtLogin, needsApproval: s.openAtLogin && !s.executableWillLaunchAtLogin };
}

const powershell = (script: string, timeout = 20_000) =>
  promisify(execFile)("powershell.exe", ["-NoProfile", "-NonInteractive", "-Command", script], { windowsHide: true, timeout });

const symbols = () => (nativeTheme.shouldUseDarkColors ? "#ffffff" : "#1a1a1a");
/** Mica, behind the sidebar as macOS's vibrancy is: Windows 11 22H2 (build 22621) and later. */
const mica = Number(os.release().split(".")[2] ?? 0) >= 22621;
/** The caption buttons as high as the top band: the first run's (48 px) or the toolbar's (44 px). */
const buttonsHeight = (kind: WindowKind) => (kind === "onboarding" ? 48 : 44);

export const win: Platform = {
  token: "win",
  installed,
  material: mica,

  // Setup.exe installs the app where it runs.
  settle() {
    return Promise.resolve(true);
  },
  installerEvent,
  registerLinks() {
    if (installed) app.setAsDefaultProtocolClient("hermesh", stub, []);
  },

  registerDaemon() {
    if (!installed) return;
    try {
      placePrograms();
    } catch (e) {
      console.warn("placing hermeshd:", e);
    }
    // Its logon task starts it; not running (the first launch, or a daemon of install.ps1's,
    // a machine only), the app registers the task again, which starts it.
    void answers().then((ok) => ok || installDaemon());
    // The caption buttons follow the theme.
    nativeTheme.on("updated", () => {
      for (const w of BrowserWindow.getAllWindows()) {
        try {
          w.setTitleBarOverlay({ symbolColor: symbols() });
        } catch {
          // A window without the overlay (the notification area's panel).
        }
      }
    });
  },

  restartDaemon(why) {
    if (!installed) return;
    console.warn(`restarting hermeshd: ${why}`);
    // Its logon task, ended and run again: the supervisor, and the daemon with it, on the programs in binDir.
    execFile(hermeshd, ["service", "restart"], { windowsHide: true }, (err) => {
      if (err) {
        console.warn("restarting hermeshd:", err);
        installDaemon();
      }
    });
  },

  daemonNeedsApproval() {
    return false; // the logon task has no switch in Settings
  },

  loginItem() {
    return installed ? loginItem() : { on: devLoginItem, needsApproval: false };
  },

  setLoginItem(on) {
    if (!installed) {
      devLoginItem = on;
      return { on, needsApproval: false };
    }
    app.setLoginItemSettings({ ...atLogin, openAtLogin: on });
    return loginItem();
  },

  openedAtLogin() {
    return installed && process.argv.includes("--hermesh-at-login");
  },

  openLoginItems() {
    shell.openExternal("ms-settings:startupapps");
  },

  runInTerminal(_c, command) {
    // A tab of Windows Terminal, else a PowerShell window; either stays when the command ends.
    const powershell = ["powershell.exe", "-NoExit", "-Command", command];
    const wt = spawn("wt.exe", powershell, { detached: true, stdio: "ignore" });
    wt.on("error", () => spawn("cmd.exe", ["/c", "start", "", ...powershell], { detached: true, stdio: "ignore", windowsHide: true }).unref());
    wt.unref();
  },

  openFolder(dir, to) {
    if (!fs.statSync(dir, { throwIfNoEntry: false })?.isDirectory()) return;
    if (to === "files") return void spawn("explorer.exe", [dir], { detached: true, stdio: "ignore" }).unref();
    // A tab of Windows Terminal there, else a PowerShell window.
    const wt = spawn("wt.exe", ["-d", dir], { detached: true, stdio: "ignore" });
    wt.on("error", () => spawn("cmd.exe", ["/c", "start", "", "powershell.exe", "-NoExit"], { cwd: dir, detached: true, stdio: "ignore", windowsHide: true }).unref());
    wt.unref();
  },

  openProcessMonitor() {
    spawn("taskmgr.exe", [], { detached: true, stdio: "ignore" }).unref();
  },

  async computerName() {
    return os.hostname();
  },

  frame(kind: WindowKind) {
    // The page's own background from the first frame (--win), for no white flash in the dark.
    const backgroundColor = nativeTheme.shouldUseDarkColors ? "#1e1e21" : "#ffffff";
    if (kind === "panel") return { backgroundColor };
    // Windows draws the caption buttons over the page's top band, whose toolbar rows are the drag area;
    // Mica shows where the page is clear (the sidebar), as on the Mac.
    const material = mica && kind !== "auxiliary" ? { backgroundMaterial: "mica" as const, backgroundColor: "#00000000" } : { backgroundColor };
    return { titleBarStyle: "hidden", titleBarOverlay: { color: "#00000000", symbolColor: symbols(), height: buttonsHeight(kind) }, ...material };
  },

  placeButtons(win, kind) {
    win.setTitleBarOverlay({ height: buttonsHeight(kind) });
  },

  grow(win, to) {
    // Windows doesn't animate setBounds: a few frames of it, easing out, as macOS does.
    const from = win.getBounds();
    const start = Date.now();
    const step = () => {
      if (win.isDestroyed()) return;
      const t = Math.min(1, (Date.now() - start) / 200);
      const e = 1 - (1 - t) ** 3;
      const at = (a: number, b: number) => Math.round(a + (b - a) * e);
      win.setBounds({ x: at(from.x, to.x), y: at(from.y, to.y), width: at(from.width, to.width), height: at(from.height, to.height) });
      if (t < 1) setTimeout(step, 16);
    };
    step();
  },
};
