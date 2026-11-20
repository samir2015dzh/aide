import fs from "node:fs";
import path from "node:path";

// The product's name before it was called Aide (internal/legacy mirrors it,
// docs/aide-rename.md): what an installation from before the rename made on
// this computer, looked for so that it keeps working. Nothing else in the app
// says the old name.

/** The data directory's name before the rename: on macOS and Windows, and on Linux. */
const dataDirTitle = "Hermesh";
const dataDirName = "hermesh";

/** The environment variables' prefix before the rename (HERMESH_X). */
export const envPrefix = "HERMESH_";

/**
 * dir, or the data directory an installation from before the rename made in
 * its place (…/hermesh for …/aide) while dir does not exist and that one
 * does: its sessions and services point into it by absolute path, so it is
 * used where it is. Mirrors kept in internal/paths.
 */
export function kept(dir: string): string {
  if (fs.existsSync(dir)) return dir;
  const base = path.basename(dir);
  const old = base === "Aide" ? dataDirTitle : base === "aide" ? dataDirName : "";
  if (!old) return dir;
  const was = path.join(path.dirname(dir), old);
  return fs.existsSync(was) ? was : dir;
}
