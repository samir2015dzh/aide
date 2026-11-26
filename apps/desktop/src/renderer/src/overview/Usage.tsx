import { memo, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { QueryWorkResponse } from "@aide/proto/local";
import { codingAgentShort, dayKey, language, tokensText, usageGroups, usageParts, usageRequest, usageYear, type UsageCell, type UsageGroup, type UsagePart } from "@aide/core";
import { AgentLogo } from "@aide/ui";
import { i18n } from "../../../shared/i18n";
import { local } from "../api";
import { machineLabel } from "../health";
import { Icon } from "../icons";
import { useStore } from "../store";

/** A day as the overview's header and the usage tip show it: "Mon, Sep 29". */
export function dayText(d: Date): string {
  const date = new Intl.DateTimeFormat(language(), { month: "short", day: "numeric" }).format(d);
  const weekday = new Intl.DateTimeFormat(language(), { weekday: "short" }).format(d);
  return i18n.t("overview:day", { date, weekday });
}

/** A year of work by day and agent (QueryWork, answered from what aided holds), asked again when its counts move; null until the first answer. */
export function useUsage(now: Date): QueryWorkResponse | null {
  const st = useStore();
  const [res, setRes] = useState<QueryWorkResponse | null>(null);
  const today = dayKey(now);
  useEffect(() => {
    let stop = false;
    local
      .queryWork(usageRequest(now))
      .then((r) => !stop && setRes(r))
      .catch(() => {}); // kept as it was
    return () => {
      stop = true;
    };
    // Asked again each day, and as the counts move.
  }, [today, st.workVersion]);
  return res;
}

/** How the year's tokens were last split (工单 #142): kept while the app runs, as the overview comes and goes; by agent to start with, as upstream's. */
let lastGroup: UsageGroup = usageGroups[0];

/** "Usage": a year of tokens by day, as GitHub's contributions, and the year's tokens by coding agent or by machine. */
export const Usage = memo(function Usage({ now, res }: { now: Date; res: QueryWorkResponse | null }) {
  const { t } = useTranslation("overview");
  const y = useMemo(() => usageYear(res, now), [res, now]);
  // The same answer split one way or the other: the parts add up to the year's total and a day's to the day's.
  const [group, setGroup] = useState<UsageGroup>(lastGroup);
  const parts = useMemo(() => usageParts(res, now, group), [res, now, group]);
  const card = useRef<HTMLDivElement>(null);
  const [tip, setTip] = useState<{ c: UsageCell; x: number; y: number } | null>(null);
  const hover = (c: UsageCell, el: HTMLElement) => {
    const r = el.getBoundingClientRect();
    const b = card.current?.getBoundingClientRect();
    if (!b) return;
    setTip({ c, x: Math.min(Math.max(r.left - b.left + r.width / 2, 70), b.width - 70), y: r.top - b.top });
  };
  const month = new Intl.DateTimeFormat(language(), { month: "short" });
  // A weekday's initial, 0 for Sunday (4 January 2026 was one).
  const weekday = (day: number) => new Intl.DateTimeFormat(language(), { weekday: "narrow" }).format(new Date(2026, 0, 4 + day));
  let lastMonth = -1;
  const months = Array.from({ length: y.weeks }, (_, w) => {
    const c = y.cells[w * 7];
    const m = c ? c.date.getMonth() : lastMonth;
    const label = c && m !== lastMonth ? month.format(c.date) : "";
    lastMonth = m;
    return label;
  });
  // Machines counted only up to their last sync, or still taking their history.
  const st = useStore();
  const name = (id: string) => st.machines.find((m) => m.machineId === id)?.machineName ?? id;
  // A part by its name, never its id (工单 #139): this Mac as the text names it elsewhere, a machine since removed says so.
  const machineOf = (id: string) => st.machines.find((m) => m.machineId === id);
  const partName = (p: UsagePart) =>
    p.other ? t("usage.other") : group === "agent" ? codingAgentShort(p.id) : machineOf(p.id) ? machineLabel(machineOf(p.id)!) : t("usage.goneMachine");
  const partIcon = (p: UsagePart) => (p.other ? <i className="u-dot" /> : group === "agent" ? <AgentLogo agent={p.id} size={14} /> : <Icon name="laptop" className="sm" />);
  const syncing = (res?.sources ?? []).filter((s) => s.syncing).map((s) => name(s.machineId));
  const behind = (res?.sources ?? []).filter((s) => !s.live && !s.syncing && s.syncedAt);
  const oldest = behind.length ? new Date(Math.min(...behind.map((s) => Number(s.syncedAt!.seconds) * 1000))) : null;
  const todayCell = y.cells.findLastIndex((c) => c !== null);
  return (
    <section aria-label={t("usage.title")}>
      <div className="sec-h">{t("usage.title")}</div>
      <div className="card usage" ref={card}>
        <div className="u-top">
          <div className="u-big">
            <small>{t("usage.year")}</small>
            <b>{tokensText(y.total)}</b>
          </div>
          {y.delta !== null && <span className="u-delta">{y.delta >= 0 ? "↑" : "↓"} {Math.abs(y.delta)}%<span>{t("usage.last30")}</span></span>}
          <div className="u-kpis">
            <div className="u-kpi"><small>{t("usage.today")}</small><b>{tokensText(y.today)}</b></div>
            <div className="u-kpi"><small>{t("usage.activeDays")}</small><b>{t("usage.days", { count: y.active })}</b></div>
            <div className="u-kpi"><small>{t("usage.streak")}</small><b>{t("usage.days", { count: y.streak })}</b></div>
          </div>
        </div>
        <div className="u-heat">
          <div className="u-in" style={{ "--weeks": y.weeks } as React.CSSProperties}>
            <div className="u-months" aria-hidden="true">
              {months.map((m, i) => <span key={i}>{m}</span>)}
            </div>
            <div className="u-wd" aria-hidden="true">
              <span /><span>{weekday(1)}</span><span /><span>{weekday(3)}</span><span /><span>{weekday(5)}</span><span />
            </div>
            <div className="u-cells" role="img" aria-label={t("usage.cells", { tokens: tokensText(y.total), active: y.active })} onMouseLeave={() => setTip(null)}>
              {y.cells.map((c, i) =>
                c ? (
                  <div key={i} className={`l${c.level} ${i === todayCell ? "today" : ""}`} onMouseEnter={(e) => hover(c, e.currentTarget)} />
                ) : (
                  <div key={i} className="none" />
                ),
              )}
            </div>
          </div>
        </div>
        <div className="u-foot">
          {y.total > 0 ? (
            <div className="u-mix">
              {/* What the year's tokens are split by (工单 #142): its two ways shown at once, not a filter. */}
              <div className="seg" role="radiogroup" aria-label={t("usage.group")}>
                {usageGroups.map((g) => (
                  <button
                    key={g}
                    type="button"
                    role="radio"
                    aria-checked={g === group}
                    className={g === group ? "on" : ""}
                    title={g === "machine" ? t("usage.byMachineHint") : undefined}
                    onClick={() => setGroup((lastGroup = g))}
                  >
                    {t(g === "agent" ? "usage.byAgent" : "usage.byMachine")}
                  </button>
                ))}
              </div>
              {parts.map((p) => (
                <span key={`${p.other}:${p.id}`} className="u-part">
                  {partIcon(p)}
                  {partName(p)}
                  <b>{tokensText(p.tokens)}</b>
                  <small>{Math.round(p.share * 100)}%</small>
                </span>
              ))}
            </div>
          ) : (
            <span className="u-empty">{res ? t("usage.empty") : ""}</span>
          )}
          <div className="u-scale" aria-hidden="true">{t("usage.less")}{[0, 1, 2, 3, 4].map((l) => <div key={l} className={`l${l}`} />)}{t("usage.more")}</div>
        </div>
        {/* Where the numbers come from, under the legend in small print, not in its row (工单 #140 P21). */}
        {syncing.length > 0 ? (
          <p className="u-source">{t("usage.syncing", { machines: syncing.join(t("listSeparator")) })}</p>
        ) : (
          oldest && <p className="u-source">{t("usage.behind", { machines: behind.map((s) => name(s.machineId)).join(t("listSeparator")), time: new Intl.DateTimeFormat(language(), { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" }).format(oldest) })}</p>
        )}
        {tip && (
          <div className="u-tip" style={{ left: tip.x, top: tip.y }}>
            <b>{tip.c.tokens ? `${tokensText(tip.c.tokens)} tokens` : t("usage.noUsage")}</b>
            {tip.c.sessions > 0 && t("usage.sessions", { count: tip.c.sessions })}
            <br />
            {dayText(tip.c.date)}
            {/* The day in the parts the footer names: they add up to the day's tokens. */}
            {tip.c.tokens > 0 && (
              <span className="u-parts">
                {parts.filter((p) => p.days.get(tip.c.key)).map((p) => (
                  <span key={`${p.other}:${p.id}`}>
                    <span>{partName(p)}</span>
                    <em>{tokensText(p.days.get(tip.c.key)!)}</em>
                  </span>
                ))}
              </span>
            )}
          </div>
        )}
      </div>
    </section>
  );
});
