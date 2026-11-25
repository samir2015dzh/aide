import { app, net, protocol, session } from "electron";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { useWindowsWords } from "../shared/i18n";
import { forward, forwardedCalls, openUpstream } from "./gateway";
import { registerIPC } from "./ipc";
import { chooseLanguage } from "./language";
import { setAppMenu } from "./menu";
import { applyAppearance, posted, watch, world } from "./notify";
import { dataDir, socketPath } from "./paths";
import { setDaemonRestarterForTest } from "./daemonRestart";
import { platform } from "./platform";
import { receiveLink } from "./links";
import { showTray, trayState } from "./tray";
import { startUpdater } from "./updater";
import { devURL, mainWindow, showMain } from "./windows";

const rendererDir = path.join(import.meta.dirname, "../renderer");
// pdf.js (a PDF over the window) decodes some images with WebAssembly and draws some through blob: URLs.
const csp = "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'";

// The window profile lives in a folder of its own inside Aide's data
// directory (Electron's default would be that directory itself, aided's).
// Tests run each instance with its own profile (and so its own single-instance lock).
app.setPath("userData", !app.isPackaged && process.env.AIDE_USER_DATA ? process.env.AIDE_USER_DATA : path.join(dataDir(), "desktop"));
chooseLanguage();
if (platform.token === "win") useWindowsWords();

protocol.registerSchemesAsPrivileged([
  // codeCache: V8 keeps the renderer's compiled script between launches.
  { scheme: "app", privileges: { standard: true, secure: true, supportFetchAPI: true, corsEnabled: true, stream: true, codeCache: true } },
]);

// Before the single-instance lock: an update's event comes while the app runs.
const installerEvent = platform.installerEvent();
if (installerEvent || !app.requestSingleInstanceLock()) app.quit();

function serve() {
  // In development the page comes from the Vite server, a different origin.
  const cors: Record<string, string> = devURL
    ? { "access-control-allow-origin": new URL(devURL).origin, "access-control-allow-headers": "*", "access-control-allow-methods": "POST", "access-control-expose-headers": "*" }
    : {};
  protocol.handle("app", async (req) => {
    const url = new URL(req.url);
    if (url.host !== "aide") return new Response("not found", { status: 404 });
    if (url.pathname.startsWith("/api/")) return forward(req, url.pathname.slice(4), socketPath(), cors);
    if (devURL) return new Response("not found", { status: 404 });
    // Static files of the renderer, never outside its directory.
    const file = path.normalize(path.join(rendererDir, decodeURIComponent(url.pathname === "/" ? "/index.html" : url.pathname)));
    if (!file.startsWith(rendererDir + path.sep)) return new Response("not found", { status: 404 });
    const res = await net.fetch(pathToFileURL(file).toString());
    const headers = new Headers(res.headers);
    if (file.endsWith(".html")) headers.set("content-security-policy", csp);
    return new Response(res.body, { status: res.status, headers });
  });
}

// aide:// links (wake links; any other brings the app forward): macOS hands them to the running app;
// Windows and Linux start a second instance with the link as an argument.
// Only the installed app takes the scheme, not development and test runs.
const linkArg = (argv: string[]) => argv.find((a) => a.startsWith("aide://"));
let started = false;
const pendingLinks: string[] = [];
function openLink(url: string) {
  if (started) receiveLink(url);
  else pendingLinks.push(url);
}
app.on("open-url", (e, url) => {
  e.preventDefault();
  openLink(url);
});
app.on("second-instance", (_e, argv) => {
  const url = linkArg(argv);
  if (url) openLink(url);
  else if (started) showMain();
});

app.whenReady().then(async () => {
  // Quitting still reaches ready: nothing of the app's runs for the installer.
  if (installerEvent) return;
  applyAppearance();
  if (!(await platform.settle())) return;
  platform.registerLinks();
  serve();
  registerIPC();
  platform.registerDaemon();
  setAppMenu();
  watch();
  // The page keeps its layout in localStorage, which Chromium writes to disk a moment later: what
  // changed just before quitting (the info card hidden, say) is written now, not lost.
  app.on("will-quit", () => session.defaultSession.flushStorageData());
  started = true;
  const url = linkArg(process.argv);
  if (url) receiveLink(url);
  pendingLinks.splice(0).forEach(receiveLink);
  if (!platform.openedAtLogin()) showMain();
  showTray();
  startUpdater();
  app.on("activate", () => showMain());
});

// Aide stays in the menu bar or the notification area: notifications come from here.
app.on("window-all-closed", () => {});

// For end-to-end tests (evaluated in the main process by Playwright).
Object.assign(globalThis, { aideTest: { openUpstream, forwardedCalls, mainWindow, notifications: () => posted, tray: trayState, world: () => world, setDaemonRestarter: setDaemonRestarterForTest } });
