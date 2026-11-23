import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import type { Session } from "@aide/proto/session";
import { displayTitle, listTime, projectTitle, sessionsByProject, updatedDate } from "@aide/core";
import { StatusDot } from "../components";
import { Icon } from "../icons";
import { Picker } from "../Picker";
import { useNow, useStore } from "../store";

/**
 * The session page's right side (工单 #90, after DSH's sidebar): the sessions to switch to — this coding agent's on
 * this machine, by project, each with its state, title and when it last moved; a click opens one. At the top, a new
 * session, and the one other thing kept here: which project folder's sessions to show.
 */
export function SessionList({ s }: { s: Session }) {
  const { t } = useTranslation("session");
  const st = useStore();
  const now = useNow(30_000);
  const groups = useMemo(() => sessionsByProject(s, st.sessions), [s, st.sessions]);
  const [folder, setFolder] = useState("");
  const title = (projectId: string) => {
    const p = st.projects.find((x) => x.machineId === s.machineId && x.projectId === projectId);
    return p ? projectTitle(p) : projectId;
  };
  const shown = groups.filter((g) => folder === "" || g.projectId === folder);
  return (
    <aside className="insp-col sesslist" aria-label={t("list.label")}>
      <div className="sesslist-top">
        <button className="btn sm sesslist-new" onClick={() => st.set({ sheet: { kind: "newSession", machineId: s.machineId, agent: s.agent } })}>
          <Icon name="plus" className="sm" />
          {t("list.new")}
        </button>
        <Picker
          chip
          size="sm"
          label={t("list.folder")}
          value={folder}
          onChange={setFolder}
          options={[{ value: "", label: t("list.allFolders") }, ...groups.map((g) => ({ value: g.projectId, label: title(g.projectId), icon: <Icon name="folder" className="sm" /> }))]}
        />
      </div>
      <div className="sesslist-body">
        {shown.map((g) => (
          <div key={g.projectId} role="group" aria-label={title(g.projectId)} className="sesslist-g">
            <div className="sesslist-h">
              <Icon name="folder" className="sm" />
              <span>{title(g.projectId)}</span>
            </div>
            {g.sessions.map((x) => {
              const cur = x.sessionId === s.sessionId;
              return (
                <button key={x.sessionId} className={`sesslist-row ${cur ? "cur" : ""}`} aria-current={cur || undefined} onClick={() => !cur && st.select(x.sessionId)}>
                  <StatusDot s={x} />
                  <span className="sesslist-t">{displayTitle(x)}</span>
                  <span className="sesslist-time">{listTime(updatedDate(x), now)}</span>
                </button>
              );
            })}
          </div>
        ))}
      </div>
    </aside>
  );
}
