import fs from "node:fs";
import path from "node:path";

// The product's name before it was called Aide (internal/legacy mirrors it,
// docs/aide-rename.md): what an installation from before the rename made on
// this computer, looked for so that it keeps working. Nothing else in the app
// says the old name.

/** The data directory's name before the rename: on macOS and Windows, and on Linux. */
export const dataDirTitle = "Hermesh";
export const dataDirName = "hermesh";

/** The environment variables' prefix before the rename (HERMESH_X). */
export const envPrefix = "HERMESH_";

/**
 * What in a data directory says it holds an identity (the owner's device, the machine): one with
 * none of these is not in use, whatever else it holds. Mirrors dataIdentity in internal/paths.
 */
const identity = [path.join("owner", "current"), path.join("owner", "owner.db"), path.join("machine", "keys", "machine.key")];

/** Whether dir has any of the files that say it is in use. */
function holds(dir: string): boolean {
  return identity.some((f) => fs.existsSync(path.join(dir, f)));
}

/**
 * dir, or the data directory an installation from before the rename made in
 * its place (…/hermesh for …/aide) while dir holds no identity and that one
 * does: its sessions and services point into it by absolute path, so it is
 * used where it is. Whether dir exists does not decide: something can make
 * it (the service's definition, the CLI's home inside it) before anything is
 * kept there. An old directory without an identity is still used while dir
 * does not exist, as before. Mirrors kept in internal/paths.
 */
export function kept(dir: string): string {
  const base = path.basename(dir);
  const old = base === "Aide" ? dataDirTitle : base === "aide" ? dataDirName : "";
  if (!old || holds(dir)) return dir;
  const was = path.join(path.dirname(dir), old);
  return holds(was) || (!fs.existsSync(dir) && fs.existsSync(was)) ? was : dir;
}
