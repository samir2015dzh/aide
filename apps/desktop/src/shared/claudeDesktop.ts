// Going on with a Claude Code session of Aide's in the Claude desktop app (工单 #131). The app imports a session Claude
// Code ran without it through its own link, claude://resume?session=<Claude Code's session id>: it writes the session's
// record itself (local_<id>, the key it de-duplicates on) and opens it, so the session is in its list from then on.
// Aide also writes that record as a session starts (internal/provider/claude/desktop.go), but the app reads its index
// only as it starts, and its Microsoft Store build is said to pass over records written from outside: the link is what
// the owner can count on. Only a session Aide is done with: the app runs a process of its own on it (anthropics/
// claude-code#111489: one still running is imported and run twice). No imports, so `node --test` loads this file.

const sessionID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** The link that opens Claude Code's session ref in the Claude desktop app, or "" for a ref that is not a session id. */
export function claudeResumeURL(ref: string): string {
  return sessionID.test(ref) ? `claude://resume?session=${ref.toLowerCase()}` : "";
}

/** The links the pages may open outside: a web page, the Claude desktop app itself (工单 #118), or one of its sessions. */
export function openableOutside(url: unknown): url is string {
  if (typeof url !== "string") return false;
  if (url.startsWith("https://") || url === "claude://") return true;
  const m = /^claude:\/\/resume\?session=(.*)$/.exec(url);
  return !!m && claudeResumeURL(m[1]) === url;
}
