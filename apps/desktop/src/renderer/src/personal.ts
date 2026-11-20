import type { GetStatusResponse } from "@hermesh/proto/local";
import type { PersonalAgentKind } from "@hermesh/ui";
import { i18n } from "../../shared/i18n";

/** A personal agent kind's name as the picker shows it (@hermesh/ui names Other in Chinese, for the console). */
export function kindName(k: PersonalAgentKind): string {
  return k.id === "other" ? i18n.t("common:personal.other") : k.name;
}

/**
 * What the owner sends the assistant, to log in with the CLI as name: the
 * server and the account; the password is a placeholder the owner fills in.
 */
export function connectMessage(status: GetStatusResponse | null | undefined, name: string): string {
  return i18n.t("common:personal.connectMessage", {
    server: status?.relayUrl ?? "",
    account: status?.account ?? "",
    name,
    project: `<${i18n.t("common:personal.projectArg")}>`,
    password: `<${i18n.t("common:personal.passwordArg")}>`,
  });
}
