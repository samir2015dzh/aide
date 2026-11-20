import type { Machine } from "@aide/proto/local";
import { i18n } from "../../../shared/i18n";
import { herDown, needsUpdate } from "../skew";
import { errorText } from "../store";

/**
 * A failed call of her's, for the person: while her doesn't run on the machine (off, starting, failed to
 * start), that, with what to do, not "Aide there is older", which an unanswered method reads as otherwise.
 */
export function herErrorText(m: Machine, e: unknown): string {
  const down = needsUpdate(e) ? herDown(m) : null;
  if (down === null) return errorText(e);
  const machine = m.machineName;
  if (down === "off") return i18n.t("her:down.off", { machine });
  if (down === "starting") return i18n.t("her:down.starting", { machine });
  return i18n.t("her:down.failed", { machine, error: down });
}
