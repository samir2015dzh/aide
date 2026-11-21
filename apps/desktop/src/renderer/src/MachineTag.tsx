import { useTranslation } from "react-i18next";
import { useStore } from "./store";
import { machineTag, manyMachines } from "./machineScope";
import "./machinetag.css";

/**
 * A session's machine where its title is (the owner's rule, 2026-11-21): with more than one machine, which
 * one it is on, this computer said as such; with one, nothing (there is nothing to tell apart).
 */
export function MachineTag({ machineId }: { machineId: string }) {
  const { t } = useTranslation("common");
  const st = useStore();
  if (!manyMachines(st.machines)) return null;
  const m = machineTag(st.machines, machineId);
  return (
    <span className={`mtag ${m.local ? "here" : ""}`} title={t("machineTag.on", { name: m.name })}>
      {m.local ? t("machineTag.here", { name: m.name }) : m.name}
    </span>
  );
}

/** Where what is written goes, by the way to write it (take over, message), while there are several machines. */
export function WritesTo({ machineId }: { machineId: string }) {
  const { t } = useTranslation("common");
  const st = useStore();
  if (!manyMachines(st.machines)) return null;
  const m = machineTag(st.machines, machineId);
  return (
    <p className="mto" role="note">
      {t("machineTag.to", { name: m.local ? t("machineTag.here", { name: m.name }) : m.name })}
    </p>
  );
}

/** A group's header in a list of sessions by machine. */
export function MachineGroupHead({ name, local, count }: { name: string; local: boolean; count: number }) {
  const { t } = useTranslation("common");
  return (
    <div className="mgrp" role="heading" aria-level={3}>
      <span className={`mtag ${local ? "here" : ""}`}>{local ? t("machineTag.here", { name }) : name}</span>
      <span className="mgrp-n">{count}</span>
    </div>
  );
}
