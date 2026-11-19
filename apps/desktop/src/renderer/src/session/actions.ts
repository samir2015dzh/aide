import type { Session } from "@hermesh/proto/session";
import { displayTitle } from "@hermesh/core";
import { i18n } from "../../../shared/i18n";
import { local } from "../api";
import type { Store } from "../store";

// What the session page does, also for the app menu (Session → Interrupt This Turn, Answer Question, Copy Session ID).

/** Stops the turn in progress; the session stays and goes on with the next message. */
export function interrupt(st: Store, s: Session) {
  return st.run(() => local.interruptSession({ sessionId: s.sessionId }), i18n.t("session:actions.interrupted", { title: displayTitle(s) }));
}

/**
 * Pushes the session's branch to its repository's remote with the machine's own git login: Hermesh never pushes
 * unasked. With pr, then opens GitHub's page to open its pull request.
 */
export async function push(st: Store, s: Session, pr = false) {
  let url = "";
  const ok = await st.run(async () => {
    url = (await local.pushSession({ sessionId: s.sessionId })).pullRequestUrl;
  }, i18n.t("session:actions.pushed", { branch: s.branch }));
  if (ok && pr && url !== "") window.hermesh?.openExternal(url);
}

export function copySessionId(st: Store, s: Session) {
  navigator.clipboard.writeText(s.sessionId).then(() => st.show(i18n.t("session:actions.copiedId")));
}

/** Puts the cursor in the pending question's answer field, if one is shown. */
export function focusAnswer() {
  document.getElementById("qtext")?.focus();
}
