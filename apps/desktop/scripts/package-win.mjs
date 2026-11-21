#!/usr/bin/env node
// Builds the Windows app (docs/windows.md §6.1) into dist/:
//   dist/Aide-windows-<arch>/                 the app's files, as Setup.exe installs them
//   dist/Aide-windows-<arch>-setup.exe        on Windows: the installer (Squirrel.Windows), per user, unsigned
//   dist/Aide-windows-<arch>.nupkg            on Windows: the update, Squirrel's package
//   dist/RELEASES-win32-<arch>.json           on Windows: the feed naming it, and its hash (main/updater.ts),
//   dist/RELEASES-win32-<arch>.json.sig         signed with the release key when given one
//   dist/Aide.ico                             on Windows: the icon Installed apps shows, a release file
//
// The app:
//   Aide.exe, resources\app.asar    Electron: main, preload and the page
//   bin\aided.exe                   console, every command
//   bin\aidedw.exe                  windowless; the daemon's logon task
//   bin\aide.exe                    the agents' CLI
// The app copies bin's three where they stay across updates, on PATH (main/platform/win.ts).
//
// Needs Go and Node, on any system; the installer needs Windows, so elsewhere only the app's files are made.
//
// Environment:
//   VERSION                     version (default: package.json's); above 0.0.0, as 0.0.1-local for one's own build (windows-version.mjs)
//   AIDE_ARCH                x64 (default) or arm64
//   AIDE_DIST                where to put it all (default: dist/)
//   AIDE_RELEASE_URL         where the release's files are downloaded from, for the feed
//   RELEASE_SIGNING_KEY_FILE    the release key (ECDSA P-256, PEM), to sign the feed
//   AIDE_UPDATE_FEED         test builds only: where the app looks for updates (default: the latest GitHub release)
//   AIDE_RELEASE_PUBKEY_FILE test builds only: the key the app checks the feed with (default: scripts/release-pubkey.pem)
import { packager } from "@electron/packager";
import { flipFuses, FuseV1Options, FuseVersion } from "@electron/fuses";
import { execFileSync } from "node:child_process";
import { createHash, sign } from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { createWindowsInstaller } from "electron-winstaller";
import { releaseCatalog } from "../src/shared/update.ts";
import enNotes from "../src/locales/en/releases.json" with { type: "json" };
import zhNotes from "../src/locales/zh-CN/releases.json" with { type: "json" };
import { windowsIcons } from "./windows-icons.mjs";
import { installSplash } from "./windows-splash.mjs";
import { windowsVersionProblem } from "./windows-version.mjs";

const desktop = path.resolve(import.meta.dirname, "..");
const root = path.resolve(desktop, "../..");
const dist = path.resolve(process.env.AIDE_DIST || path.join(root, "dist"));
const tmp = path.join(dist, "tmp-win");
const pkg = JSON.parse(fs.readFileSync(path.join(desktop, "package.json"), "utf8"));
const version = process.env.VERSION || pkg.version;
// Before anything is built: an app at a version Squirrel's launcher can't start (windows-version.mjs).
const versionProblem = windowsVersionProblem(version);
if (versionProblem) throw new Error(`VERSION: ${versionProblem}`);
const electronVersion = JSON.parse(fs.readFileSync(path.join(desktop, "node_modules/electron/package.json"), "utf8")).version;
const arch = process.env.AIDE_ARCH || "x64";
if (!["x64", "arm64"].includes(arch)) throw new Error(`AIDE_ARCH: ${arch}`);
const onWindows = process.platform === "win32";

const sh = (cmd, args, opts = {}) => execFileSync(cmd, args, { stdio: "inherit", ...opts });
const step = (s) => console.log(`› ${s}`);

fs.rmSync(tmp, { recursive: true, force: true });
fs.mkdirSync(tmp, { recursive: true });

step("building the page and the main process");
const staged = path.join(tmp, "app");
sh("pnpm", ["--dir", desktop, "build"], { env: { ...process.env, VERSION: version, AIDE_DESKTOP_OUT: staged }, shell: onWindows });
fs.writeFileSync(
  path.join(staged, "package.json"),
  JSON.stringify({ name: "aide", productName: "Aide", version, main: "main/index.js", type: "module" }, null, 2),
);

step(`building aided, aidedw and aide (${arch})`);
const bin = path.join(tmp, "bin");
// No build names a server: logging in asks for it (docs/accounts.md).
const ldflags = `-s -w -X github.com/samir2015dzh/aide/internal/machine.Version=${version}`;
const goEnv = { ...process.env, GOOS: "windows", GOARCH: arch === "x64" ? "amd64" : "arm64", CGO_ENABLED: "0" };
for (const [name, cmd, flags] of [
  ["aided.exe", "./cmd/aided", ldflags],
  ["aidedw.exe", "./cmd/aided", `${ldflags} -H windowsgui`],
  ["aide.exe", "./cmd/aide", ldflags],
]) {
  sh("go", ["build", "-trimpath", "-ldflags", flags, "-o", path.join(bin, name), cmd], { cwd: root, env: goEnv });
}

step("drawing the icons");
const layout = path.join(dist, `Aide-windows-${arch}`);
fs.rmSync(layout, { recursive: true, force: true });
const ico = path.join(tmp, "Aide.ico");
windowsIcons(ico);

step("packaging Aide.exe");
const [built] = await packager({
  dir: staged,
  out: tmp,
  name: "Aide",
  executableName: "Aide",
  electronVersion,
  platform: "win32",
  arch,
  appVersion: version,
  icon: ico,
  // What Task Manager and Installed apps show: Aide; Aide.exe is what links, shortcuts and login start.
  win32metadata: { CompanyName: "Aide", FileDescription: "Aide", ProductName: "Aide", InternalName: "Aide", OriginalFilename: "Aide.exe" },
  asar: true,
  prune: false,
  overwrite: true,
});
fs.renameSync(built, layout);
// Chromium's own words: only the app's languages stay.
for (const f of fs.readdirSync(path.join(layout, "locales"))) if (!["en-US.pak", "zh-CN.pak"].includes(f)) fs.rmSync(path.join(layout, "locales", f));

step("flipping Electron fuses");
// As on the Mac (packager embeds the asar's digest in Aide.exe).
await flipFuses(path.join(layout, "Aide.exe"), {
  version: FuseVersion.V1,
  [FuseV1Options.RunAsNode]: false,
  [FuseV1Options.EnableCookieEncryption]: true,
  [FuseV1Options.EnableNodeOptionsEnvironmentVariable]: false,
  [FuseV1Options.EnableNodeCliInspectArguments]: false,
  [FuseV1Options.EnableEmbeddedAsarIntegrityValidation]: true,
  [FuseV1Options.OnlyLoadAppFromAsar]: true,
  [FuseV1Options.GrantFileProtocolExtraPrivileges]: false,
});

step("adding the programs");
fs.cpSync(bin, path.join(layout, "bin"), { recursive: true });
console.log(`✓ ${layout}`);

if (onWindows) {
  step("making the installer");
  // pnpm runs no install scripts of dependencies: electron-winstaller's picks this computer's 7-Zip.
  const vendor = path.join(desktop, "node_modules/electron-winstaller/vendor");
  for (const ext of ["exe", "dll"]) fs.copyFileSync(path.join(vendor, `7z-${process.arch}.${ext}`), path.join(vendor, `7z.${ext}`));
  const out = path.join(dist, `squirrel-${arch}`);
  fs.rmSync(out, { recursive: true, force: true });
  const splash = path.join(tmp, "installing.gif");
  installSplash(splash);
  await createWindowsInstaller({
    appDirectory: layout,
    outputDirectory: out,
    // Squirrel installs into %LOCALAPPDATA%\<name> and removes that folder on uninstall:
    // not Aide, the data folder's name. Squirrel updates the package of this id; an app
    // installed before the rename (its package had the old name) does not update to it, and is reinstalled
    // once (docs/aide-rename.md).
    name: "AideApp",
    title: "Aide",
    authors: "Aide",
    description: "Aide",
    setupExe: "AideSetup.exe",
    exe: "Aide.exe",
    version,
    setupIcon: ico,
    // What Setup.exe shows while it installs (Squirrel's own is an old editor's).
    loadingGif: splash,
    // Apps › Installed apps shows it; Squirrel downloads it while installing (the release carries it).
    iconUrl: "https://github.com/samir2015dzh/aide/releases/latest/download/Aide.ico",
    // The template takes Electron's files only.
    additionalFiles: [{ src: "bin\\**", target: "lib\\net45\\bin" }],
    noMsi: true,
    noDelta: true,
  });
  const setup = path.join(dist, `Aide-windows-${arch}-setup.exe`);
  fs.renameSync(path.join(out, "AideSetup.exe"), setup);
  fs.copyFileSync(ico, path.join(dist, "Aide.ico"));
  console.log(`✓ ${setup}`);

  step("the update and its feed");
  // RELEASES: one line, "<SHA-1> <package> <size>", which the app writes beside the package it downloads.
  const releases = fs.readFileSync(path.join(out, "RELEASES"), "utf8").replace(/^\uFEFF/, "").trim();
  const nupkg = path.join(dist, `Aide-windows-${arch}.nupkg`);
  fs.copyFileSync(path.join(out, releases.split(/\s+/)[1]), nupkg);
  const releaseURL = process.env.AIDE_RELEASE_URL || `https://github.com/samir2015dzh/aide/releases/download/v${version}`;
  const feed = path.join(dist, `RELEASES-win32-${arch}.json`);
  fs.writeFileSync(feed, JSON.stringify({
    currentRelease: version,
    aideNotes: releaseCatalog(enNotes, zhNotes, version),
    package: { url: `${releaseURL}/${path.basename(nupkg)}`, sha256: createHash("sha256").update(fs.readFileSync(nupkg)).digest("hex"), releases },
  }, null, 2));
  if (process.env.RELEASE_SIGNING_KEY_FILE) fs.writeFileSync(`${feed}.sig`, sign("sha256", fs.readFileSync(feed), fs.readFileSync(process.env.RELEASE_SIGNING_KEY_FILE, "utf8")));
  console.log(`✓ ${nupkg}\n✓ ${feed}`);
}
