import { app } from "electron";
import fs from "node:fs";
import path from "node:path";
import type { LinkEntry } from "../shared/watchLink";

// The record of the app's links breaking and coming back (工单 #87): this computer's Aide background service (aided)
// and this computer's machine with the relay, each break and its end, kept in the app's profile for the settings'
// records page to show, so a "it showed offline" can be traced afterwards. The newest 200 entries.

const keep = 200;
let entries: LinkEntry[] | undefined;
const file = () => path.join(app.getPath("userData"), "link-log.json");

export function linkLog(): LinkEntry[] {
  if (!entries) {
    try {
      const v = JSON.parse(fs.readFileSync(file(), "utf8"));
      entries = Array.isArray(v) ? v : [];
    } catch {
      entries = [];
    }
  }
  return entries;
}

export function addLinkEntry(e: LinkEntry) {
  const all = [...linkLog(), e].slice(-keep);
  entries = all;
  fs.promises.writeFile(file(), JSON.stringify(all)).catch((err) => console.warn("link log:", err));
}

/** When each link went down (for an entry's downFor when it comes back), by link and who noticed. */
const downAt = new Map<string, number>();

/** A link went down: kept once until it is back. */
export function linkDown(link: LinkEntry["link"], from: LinkEntry["from"], why?: LinkEntry["why"], detail?: string) {
  const k = `${link}/${from}`;
  if (downAt.has(k)) return;
  const at = Date.now();
  downAt.set(k, at);
  addLinkEntry({ at, link, kind: "down", why, detail: detail || undefined, from });
}

/** A link is back: kept with how long it was down, if it was. */
export function linkBack(link: LinkEntry["link"], from: LinkEntry["from"]) {
  const k = `${link}/${from}`;
  const was = downAt.get(k);
  if (was === undefined) return;
  downAt.delete(k);
  const at = Date.now();
  addLinkEntry({ at, link, kind: "back", downFor: at - was, from });
}
