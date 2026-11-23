import { useTranslation } from "react-i18next";
import { SessionStatus, type Session } from "@aide/proto/session";
import { codingAgentTitle, isArchivable, isInterruptible, modelTitle } from "@aide/core";
import { i18n } from "../../../shared/i18n";
import type { SlashCommand } from "../bots/Composer";
import { runCommand } from "../commands";
import { useStore, type Store } from "../store";

// What a session's composer offers (工单 #92): the commands typing / lists, and the choices it runs with under it.

/**
 * The commands typed after / in a session's composer: the work-order macros the machine expands (!plan, !implement:
 * internal/machine/expand.go), and the app's own session commands, as its menus run them (commands.ts), while they apply.
 */
export function sessionCommands(st: Store, s: Session, setText: (v: string) => void): SlashCommand[] {
  const t = (k: "plan" | "implement" | "newSession" | "interrupt" | "archive" | "unarchive" | "copyId") => i18n.t(`common:composer.cmd.${k}`);
  return [
    { id: "plan", name: "plan", label: t("plan"), run: () => setText("!plan ") },
    { id: "implement", name: "implement", label: t("implement"), run: () => setText("!implement ") },
    { id: "new", name: "new", label: t("newSession"), run: () => st.set({ sheet: { kind: "newSession", machineId: s.machineId, agent: s.agent } }) },
    ...(isInterruptible(s) ? [{ id: "interrupt", name: "stop", label: t("interrupt"), run: () => runCommand(st, "interrupt") }] : []),
    ...(isArchivable(s) ? [{ id: "archive", name: "archive", label: t("archive"), run: () => runCommand(st, "archive") }] : []),
    ...(s.status === SessionStatus.ARCHIVED ? [{ id: "unarchive", name: "unarchive", label: t("unarchive"), run: () => runCommand(st, "unarchive") }] : []),
    { id: "copyId", name: "id", label: t("copyId"), run: () => runCommand(st, "copySessionId") },
  ];
}

/**
 * The model and effort the session runs with, under its composer on the right (工单 #92): a session keeps them, so
 * they are told. What the agent does not tell follows its own setting. withModel false: its model is picked beside
 * it (another one starts a new conversation, 工单 #98 补充), only its effort is told.
 */
export function SessionParams({ s, withModel = true }: { s: Session; withModel?: boolean }) {
  const { t } = useTranslation(["session", "firstrun"]);
  const st = useStore();
  const models = st.machines.find((m) => m.machineId === s.machineId)?.status?.agents.find((a) => a.name === s.agent)?.models;
  const same = t("inspector.sameAs", { agent: codingAgentTitle(s.agent) });
  const model = s.model === "" ? same : modelTitle(s.model, models);
  const effort = s.effort === "" ? "" : t(`firstrun:prepare.efforts.${s.effort}`, { defaultValue: s.effort });
  return (
    <span className="composer-param" title={t("inspector.context")}>
      {[withModel ? model : "", effort].filter(Boolean).join(" · ")}
    </span>
  );
}
