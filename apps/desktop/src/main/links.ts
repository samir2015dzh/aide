import { ConnectError } from "@connectrpc/connect";
import { parseAppLink } from "@aide/core";
import { app } from "electron";
import { mainWindow, showMain } from "./windows";

// aide:// links for the page, kept until it takes them (it may not be
// loaded yet when the browser starts the app with one). There is no browser
// sign-in: every way in starts with the owner's own server, typed (owner's
// ruling of 2026-11-19), so a link code (aide://link) is never ours.
const links: string[] = [];

/** An aide:// link the app was opened with: a wake link (docs/agent-names.md §7.2). */
export function receiveLink(url: string) {
  const l = parseAppLink(url);
  // Any other aide: link (aide://open) brings the app forward, as opening it would.
  // So does a sign-in's link code or an account's join link: those come from a hosted center, not ours.
  if (l?.kind !== "wake") return void (app.isReady() && showMain());
  links.push(url);
  if (!app.isReady()) return; // the window opens once ready, and takes it
  const win = mainWindow();
  showMain();
  win?.webContents.send("link");
}

export function takeLinks(): string[] {
  return links.splice(0);
}

/** An error's message for the person (a daemon error without its code). */
export function errorMessage(e: unknown): string {
  if (e instanceof ConnectError) return e.rawMessage;
  return e instanceof Error ? e.message : String(e);
}
