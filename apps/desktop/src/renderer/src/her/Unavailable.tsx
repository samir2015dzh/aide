import { useTranslation } from "react-i18next";
import type { Machine } from "@aide/proto/local";
import type { HerHas } from "../skew";

/**
 * A page's body while her on the machine can't give it (herHas): Her is not turned on there, her is
 * not running there (starting, or failed to), or the machine's Aide is older than this app. Each says
 * what it is and what to do; only the last says to update. null when her can.
 */
export function HerUnavailable({ machine, has }: { machine: Pick<Machine, "machineName" | "her">; has: HerHas }) {
  const { t: tc } = useTranslation("common");
  const name = machine.machineName;
  if (has === "older")
    return (
      <div className="rtn-empty" data-testid="needs-update">
        <b>{tc("needsUpdate.title")}</b>
        {tc("needsUpdate.body", { machine: name })}
      </div>
    );
  if (has === "off")
    return (
      <div className="rtn-empty" data-testid="her-off">
        <b>{tc("herUnavailable.offTitle", { machine: name })}</b>
        {tc("herUnavailable.offBody", { machine: name })}
      </div>
    );
  if (has === "notRunning") {
    const error = machine.her?.error ?? "";
    return (
      <div className="rtn-empty" data-testid="her-not-running">
        <b>{tc("herUnavailable.notRunningTitle", { machine: name })}</b>
        {error ? tc("herUnavailable.failedBody", { machine: name, error }) : tc("herUnavailable.notRunningBody", { machine: name })}
      </div>
    );
  }
  return null;
}
