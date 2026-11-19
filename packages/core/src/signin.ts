// Joining and links (docs/trusted-devices.md 4.1–4.2): the pure parts the
// app's main process and its first-run pages share. There is no browser
// sign-in to a hosted center: every way in starts with the owner's own
// server, typed (owner's ruling of 2026-11-19).

/** A hermesh:// link the app was opened with. */
export type AppLink = { kind: "link"; code: string } | { kind: "join"; owner: string } | { kind: "wake"; code: string };

/**
 * hermesh://link?code=… (the browser sign-in came back with a link code),
 * hermesh://join?owner=… (the account already has an owner: join it)
 * or hermesh://wake?code=… (an agent's wake link, docs/agent-names.md §7.2).
 */
export function parseAppLink(s: string): AppLink | null {
  let u: URL;
  try {
    u = new URL(s);
  } catch {
    return null;
  }
  if (u.protocol !== "hermesh:") return null;
  if (u.host === "link") {
    const code = u.searchParams.get("code");
    return code ? { kind: "link", code } : null;
  }
  if (u.host === "join") {
    const owner = u.searchParams.get("owner");
    return owner ? { kind: "join", owner } : null;
  }
  if (u.host === "wake") {
    const code = u.searchParams.get("code");
    return code ? { kind: "wake", code } : null;
  }
  return null;
}

/** A root key's platform, by Node's name for the OS. */
export function rootPlatform(nodePlatform: string): string {
  return nodePlatform === "darwin" ? "macos" : nodePlatform === "win32" ? "windows" : "linux";
}

/**
 * A connection address, "relay.example.com/o_…": where devices connect and
 * the owner, as internal/connaddr parses it.
 */
export function parseConnectAddress(s: string): { relay: string; owner: string } | null {
  let t = s.trim();
  if (t === "") return null;
  if (!t.includes("://")) t = "https://" + t;
  let u: URL;
  try {
    u = new URL(t);
  } catch {
    return null;
  }
  if (u.protocol !== "https:" && u.protocol !== "http:") return null;
  const path = u.pathname.replace(/^\/+|\/+$/g, "");
  const i = path.lastIndexOf("/");
  const owner = path.slice(i + 1);
  if (!owner.startsWith("o_") || owner.length !== 26) return null;
  const prefix = i > 0 ? "/" + path.slice(0, i) : "";
  return { relay: `${u.protocol}//${u.host}${prefix}`, owner };
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
