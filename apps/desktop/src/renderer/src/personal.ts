import type { GetStatusResponse } from "@hermesh/proto/local";
import type { PersonalAgentKind } from "@hermesh/ui";
import { i18n } from "../../shared/i18n";

/** A personal agent kind's name as the picker shows it (@hermesh/ui names Other in Chinese, for the console). */
export function kindName(k: PersonalAgentKind): string {
  return k.id === "other" ? i18n.t("common:personal.other") : k.name;
}

/**
 * What the owner pastes into the personal agent's chat; name is what it will
 * be called. It pairs with the connection address of the owner's server.
 */
export function connectMessage(status: GetStatusResponse | null | undefined, name: string): string {
  return i18n.t("common:personal.connectMessage", { address: status?.connectAddress ?? "", name, project: `<${i18n.t("common:personal.projectArg")}>` });
}
