import type { Session } from "@aide/proto/session";
import { displayTitle } from "@aide/core";
import { i18n } from "../../../shared/i18n";
import { local } from "../api";
import type { Store } from "../store";

// What the session page does, also for the app menu (Session → Interrupt This Turn, Answer Question, Copy Session ID).

/** Stops the turn in progress; the session stays and goes on with the next message. */
export function interrupt(st: Store, s: Session) {
  return st.run(() => local.interruptSession({ sessionId: s.sessionId }), i18n.t("session:actions.interrupted", { title: displayTitle(s) }));
}

/**
 * Pushes the session's branch to its repository's remote with the machine's own git login: Aide never pushes
 * unasked. With pr, then opens GitHub's page to open its pull request.
 */
export async function push(st: Store, s: Session, pr = false) {
  let url = "";
  const ok = await st.run(async () => {
    url = (await local.pushSession({ sessionId: s.sessionId })).pullRequestUrl;
  }, i18n.t("session:actions.pushed", { branch: s.branch }));
  if (ok && pr && url !== "") window.aide?.openExternal(url);
}

/**
 * Rolls the session back to the end of turn `to` (0: its base), once confirmed: every message and change after it is
 * discarded, for the agent too.
 */
export function confirmRollback(st: Store, sessionId: string, title: string, to: number, last: number) {
  st.set({
    sheet: {
      kind: "confirm",
      title: i18n.t("session:actions.rollbackTitle", { turn: to + 1 }),
      message: i18n.t("session:actions.rollbackMessage", { from: to + 1, to: last }),
      button: i18n.t("session:actions.rollbackButton"),
      run: () => local.rollbackSession({ sessionId, turn: to }),
      done: i18n.t("session:actions.rolledBack", { title, turn: to }),
    },
  });
}

export function copySessionId(st: Store, s: Session) {
  navigator.clipboard.writeText(s.sessionId).then(() => st.show(i18n.t("session:actions.copiedId")));
}

/** Puts the cursor in the pending question's answer field, if one is shown. */
export function focusAnswer() {
  document.getElementById("qtext")?.focus();
}
