import type { TFunction } from "i18next";
import type { AgentStatus } from "@aide/proto/machine";
import { defaultModelChoice, modelChoices } from "@aide/core";
import type { PickerOption } from "./Picker";

/**
 * An agent's model list (工单 #99): each model its specific one ("Opus 5.5", in full below it), the default naming the
 * one it runs; current, a model a conversation runs, listed too when the agent lists it no more. t: the common
 * namespace's.
 */
export function modelOptions(t: TFunction<"common">, a: AgentStatus | undefined, current = ""): PickerOption<string>[] {
  const d = defaultModelChoice(a);
  return [
    { value: "", label: d ? t("composer.defaultModelIs", { model: d.title }) : t("composer.defaultModel"), sub: d?.detail || undefined },
    ...modelChoices(a?.name ?? "", a?.models ?? [], current).map((c) => ({ value: c.value, label: c.title, sub: c.detail || (c.unknown ? t("composer.modelUnknown") : undefined) })),
  ];
}
