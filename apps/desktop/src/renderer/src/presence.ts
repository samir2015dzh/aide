import type { Presence } from "@aide/core";

// A state dot's colour for each presence (工单 #135 P25 / 审计 B): one colour per meaning, so the same label is the
// same dot on the sidebar, a bot's page, the connections card and the machine page. The presence comes from
// @aide/core (rowPresence, BotState.presence), never from the label's words.
const dotClass: Record<Presence, string> = {
  online: "on", // green: a personal agent connected now
  signedIn: "ready", // Fluent blue: signed in / ready
  unconfirmed: "warn", // amber: there, its sign-in not confirmed
  signedOut: "bad", // red: does not work until fixed
  checking: "checking", // hollow blue ring
  unknown: "", // neutral grey: not known now, away, paused
};

/** The `.dotc` class for a presence. */
export function presenceDot(p: Presence): string {
  return dotClass[p];
}
