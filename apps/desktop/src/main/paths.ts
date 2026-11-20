import { app } from "electron";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { envPrefix, kept } from "./legacy";

// Test builds only (AIDE_TEST_DATA_DIR when building): a data directory
// baked in, so an app that Squirrel relaunches after an update still talks
// to the test's daemon, without touching the session's environment.
declare const __AIDE_TEST_DATA_DIR__: string;
export const testDataDir = __AIDE_TEST_DATA_DIR__;

// Mirrors DataDir and LocalAPI in internal/paths.
export function dataDir(): string {
  if (testDataDir) return testDataDir;
  // AIDE_DATA_DIR, or the name it had before the rename (legacy.ts).
  const set = process.env.AIDE_DATA_DIR || process.env[envPrefix + "DATA_DIR"];
  if (set) return set;
  const home = os.homedir();
  if (process.platform === "darwin") return kept(path.join(home, "Library", "Application Support", "Aide"));
  if (process.platform === "win32") return kept(path.join(process.env.LOCALAPPDATA || path.join(home, "AppData", "Local"), "Aide"));
  if (process.env.XDG_DATA_HOME) return kept(path.join(process.env.XDG_DATA_HOME, "aide"));
  return kept(path.join(home, ".local", "share", "aide"));
}

export function socketPath(): string {
  if (process.platform === "win32") return pipeName(process.env.AIDE_SOCKET && !app.isPackaged ? process.env.AIDE_SOCKET : path.join(dataDir(), "run", "aided.pipe"));
  if (process.env.AIDE_SOCKET && !app.isPackaged) return process.env.AIDE_SOCKET;
  const p = path.join(dataDir(), "run", "aided.sock");
  if (Buffer.byteLength(p) > 100) return path.join(os.tmpdir(), `aide-${process.getuid?.() ?? 0}`, "aided.sock");
  return p;
}

/**
 * On Windows aided serves a pipe with a random name, written in a file
 * only this user can read (internal/localipc): read again for each
 * connection, since it changes whenever aided starts. Before it has
 * started there is no name, and connecting to the file's path fails as
 * a missing socket does.
 */
function pipeName(file: string): string {
  if (file.startsWith("\\\\.\\pipe\\")) return file;
  try {
    return fs.readFileSync(file, "utf8").trim() || file;
  } catch {
    return file;
  }
}
