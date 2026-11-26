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
  const goSection = (id: string, fallback?: () => void) => {
    const el = document.getElementById(id);
    if (el) {
      el.scrollIntoView({ behavior: matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth", block: "start" });
      return;
    }
    fallback?.();
  };
  return (
    <div className="ov-tiles" role="group" aria-label={t("tiles.label")}>
      <button type="button" className={`ov-tile need ${waits > 0 ? "on" : ""}`} onClick={() => goSection("ov-attention", () => goSection("ov-working"))}>
        <b>{waits}</b>
        <span>{t("tiles.needYou")}</span>
      </button>
      <button type="button" className="ov-tile done" onClick={() => goSection("ov-finished", () => st.set({ selection: { kind: "tasks" } }))}>
        <b>{team.finished.length}</b>
        <span>{t("tiles.done")}</span>
      </button>
      <button type="button" className="ov-tile run" onClick={() => goSection("ov-working", () => st.set({ selection: { kind: "tasks" } }))}>
        <b>{team.running.length}</b>
        <span>{t("tiles.running")}</span>
      </button>
      <button type="button" className="ov-tile mach" onClick={() => window.aide?.openSettings("machines")}>
        <b>
          {online}
          <small>/{st.machines.length}</small>
        </b>
        {/* One number and one label, as the three beside it (工单 #140 P19): the offline ones said in the label. */}
        <span>{offline > 0 ? t("tiles.machinesOffline", { count: offline }) : t("tiles.machines")}</span>
      </button>
    </div>
  );
}
