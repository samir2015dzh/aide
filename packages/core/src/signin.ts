// Links and logging in (docs/accounts.md): the pure parts the app's main
// process and its first-run pages share. Every way in starts with the
// owner's own server, typed, and its account and password.

/** An aide:// link the app was opened with. */
export type AppLink = { kind: "wake"; code: string };

/** aide://wake?code=… (an agent's wake link, docs/agent-names.md §7.2). */
export function parseAppLink(s: string): AppLink | null {
  let u: URL;
  try {
    u = new URL(s);
  } catch {
    return null;
  }
  if (u.protocol !== "aide:") return null;
  if (u.host === "wake") {
    const code = u.searchParams.get("code");
    return code ? { kind: "wake", code } : null;
  }
  return null;
}

/** A device's platform, by Node's name for the OS. */
export function rootPlatform(nodePlatform: string): string {
  return nodePlatform === "darwin" ? "macos" : nodePlatform === "win32" ? "windows" : "linux";
}

/** A project id for a folder the person picked: its name, made safe and unique. */
export function projectIdFromFolder(name: string, taken: ReadonlySet<string>): string {
  let s = [...name.toLowerCase()].map((c) => (/^[a-z0-9._-]$/.test(c) ? c : "-")).join("");
  s = s.replace(/^[-._]+/, "");
  if (s === "") s = "project";
  s = s.slice(0, 60);
  let id = s;
  for (let n = 2; taken.has(id); n++) id = `${s}-${n}`;
  return id;
}
