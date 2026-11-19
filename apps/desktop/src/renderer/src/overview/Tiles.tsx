import { useTranslation } from "react-i18next";
import { overviewMarks } from "../Sidebar";
import { useStore } from "../store";
import { useTeam } from "../team";

/**
 * The overview's four numbers at the top (design/aide in the handover): what needs the owner, what is done, what
 * runs, and how many machines are online. They count what the sidebar and the sections below show; they decide nothing.
 */
export function Tiles({ now }: { now: Date }) {
  const { t } = useTranslation("overview");
  const st = useStore();
  const team = useTeam();
  const { waits } = overviewMarks(st, now);
  const online = st.machines.filter((m) => m.online).length;
  const offline = st.machines.length - online;
  return (
    <div className="ov-tiles" role="group" aria-label={t("tiles.label")}>
      <div className={`ov-tile need ${waits > 0 ? "on" : ""}`}>
        <b>{waits}</b>
        <span>{t("tiles.needYou")}</span>
      </div>
      <div className="ov-tile done">
        <b>{team.finished.length}</b>
        <span>{t("tiles.done")}</span>
      </div>
      <div className="ov-tile run">
        <b>{team.running.length}</b>
        <span>{t("tiles.running")}</span>
      </div>
      <div className="ov-tile mach">
        <b>
          {online}
          <small>/{st.machines.length}</small>
        </b>
        <span>{t("tiles.machines")}</span>
        {offline > 0 && <em>{t("tiles.offline", { count: offline })}</em>}
      </div>
    </div>
  );
}
