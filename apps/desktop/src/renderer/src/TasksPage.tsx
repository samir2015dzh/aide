import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { agoText, codingAgentShort, livenessOf, sessionStateOf, stateLine } from "@aide/core";
import { AgentLogo } from "@aide/ui";
import { useNow, useStore } from "./store";
import { dispatchers, taskRows, type TaskRow } from "./tasks";
import "./dsh/dsh.css";
import "./tasks.css";

/** How many of the work handed out need a look (asks, failed, stalled, lost), for the sidebar. */
export function useTaskLook(): number {
  const st = useStore();
  const now = useNow(30_000);
  return useMemo(
    () =>
      taskRows(st.sessions, st.clients, (s) => sessionStateOf(st.lastKnown(s.sessionId) ?? s, livenessOf(st.machines.find((m) => m.machineId === s.machineId)), st.stateLimits, now), now).filter(
        (r) => r.rank === 0,
      ).length,
    [st, now],
  );
}

/**
 * 任务动态 (work order #27): what the owner's agents (Muse first) handed out
 * through Aide, and how each is going — what it is doing (from the session
 * itself), its state as every screen says it (stalled, lost · last known …),
 * where it runs, when it started and was last heard of. A click opens the
 * session. Needing a look comes first.
 */
export function TasksPage() {
  const { t } = useTranslation("common");
  const st = useStore();
  const now = useNow(5_000);
  const [only, setOnly] = useState("");
  const rows = useMemo(
    () =>
      taskRows(
        st.sessions,
        st.clients,
        (s) => sessionStateOf(st.lastKnown(s.sessionId) ?? s, livenessOf(st.machines.find((m) => m.machineId === s.machineId)), st.stateLimits, now),
        now,
      ),
    [st, now],
  );
  const who = dispatchers(rows);
  const shown = only ? rows.filter((r) => r.dispatcherId === only) : rows;
  const counts = [0, 1, 2, 3].map((k) => shown.filter((r) => r.rank === k).length);
  return (
    <>
      <div className="ctool drag">
        <div className="ctitle" />
      </div>
      <div className="dsh">
        <section className="dsh-view tasks" aria-label={t("tasks.title")}>
          <h1>{t("tasks.title")}</h1>
          <p className="dsh-note">{t("tasks.note")}</p>
          {rows.length > 0 && (
            <p className="dsh-note" aria-label={t("tasks.summary")}>
              {[
                counts[0] ? t("tasks.needLook", { count: counts[0] }) : "",
                counts[1] ? t("tasks.atWork", { count: counts[1] }) : "",
                counts[2] ? t("tasks.doneUnseen", { count: counts[2] }) : "",
                counts[3] ? t("tasks.done", { count: counts[3] }) : "",
              ]
                .filter(Boolean)
                .join(" · ")}
            </p>
          )}
          {who.length > 1 && (
            <div className="tasks-who" role="group" aria-label={t("tasks.dispatchers")}>
              <button className={`btn sm ${only === "" ? "primary" : ""}`} aria-pressed={only === ""} onClick={() => setOnly("")}>
                {t("tasks.everyone", { count: rows.length })}
              </button>
              {who.map((d) => (
                <button key={d.id} className={`btn sm ${only === d.id ? "primary" : ""}`} aria-pressed={only === d.id} onClick={() => setOnly(only === d.id ? "" : d.id)}>
                  {d.name} · {d.count}
                </button>
              ))}
            </div>
          )}
          {rows.length === 0 && <p className="dsh-empty">{t("tasks.none")}</p>}
          <ul className="held-list" aria-label={t("tasks.list")}>
            {shown.map((r) => (
              <TaskItem key={r.session.sessionId} r={r} now={now} />
            ))}
          </ul>
        </section>
      </div>
    </>
  );
}

function TaskItem({ r, now }: { r: TaskRow; now: Date }) {
  const { t } = useTranslation("common");
  const st = useStore();
  const s = r.session;
  const machine = st.machines.find((m) => m.machineId === s.machineId)?.machineName || s.machineId;
  const doing = r.doing.kind === "tool" ? t("tasks.usingTool", { tool: r.doing.text }) : r.doing.text;
  const mark = r.rank === 0 ? "warn" : r.rank === 1 ? "busy" : "";
  return (
    <li className="dsh-row task-row" aria-label={s.title || s.sessionId}>
      <button className="link task-open" onClick={() => st.select(s.sessionId)} title={t("tasks.open")}>
        <AgentLogo agent={s.agent} size={20} />
        <span className="t">
          <b>{s.title || s.projectId}</b>
          <span>
            {[`${r.dispatcher} → ${codingAgentShort(s.agent)}`, `${machine}/${s.projectId}`, r.startedAt ? t("tasks.started", { ago: agoText(r.startedAt, now) }) : "", r.lastActivity ? t("tasks.lastActivity", { ago: agoText(r.lastActivity, now) }) : ""]
              .filter(Boolean)
              .join(" · ")}
          </span>
          <span className="dsh-msg">{doing}</span>
        </span>
        <span className={`task-state ${mark}`}>{stateLine(r.view)}</span>
      </button>
    </li>
  );
}
