import { ConnectLink } from "../components";
import { useEffect, useState, type FormEvent } from "react";
import { Icon } from "../icons";
import { useTranslation } from "react-i18next";
import { codingAgentTitle, displayTitle } from "@aide/core";
import { AgentLogo } from "@aide/ui";
import { projectsFor } from "../AgentList";
import { local } from "../api";
import { useStore } from "../store";
import { ErrorLine, useAction } from "./common";

/**
 * New Session (docs/technical-design.md §3.4): the owner starts a coding agent on one of the owner's machines, in
 * one of its projects for that agent or in a GitHub repository, with the model, effort and mode the agent offers
 * there (unset: the machine's defaults, else the agent's own), and the first message. The machine checks it all;
 * what it refuses stays here as it said it. Started, the session opens once the list has it.
 */
export function NewSessionSheet({ machineId, agent: agentAsked }: { machineId?: string; agent?: string }) {
  const { t } = useTranslation("sheets");
  const tf = useTranslation("firstrun").t;
  const st = useStore();
  const online = st.machines.filter((m) => m.online || m.local);
  const [chosen, setMid] = useState(machineId);
  const machine = online.find((m) => m.machineId === chosen) ?? online.find((m) => m.local) ?? online[0];
  const mid = machine?.machineId ?? "";
  const agents = (machine?.status?.agents ?? []).filter((a) => a.available);
  const [agentChosen, setAgent] = useState(agentAsked);
  // A paused agent is listed, but no new session starts with it (SetAgentPaused).
  const usable = agents.filter((x) => !x.pause);
  const a = usable.find((x) => x.name === agentChosen) ?? usable[0];
  const agent = a?.name ?? "";
  const projects = projectsFor(st.projects.filter((p) => p.machineId === mid), agent);
  const [fromRepo, setFromRepo] = useState(false);
  const [projectChosen, setProject] = useState("");
  const project = (projects.find((p) => p.projectId === projectChosen) ?? projects[0])?.projectId ?? "";
  const [repo, setRepo] = useState("");
  const [branch, setBranch] = useState("");
  const [model, setModel] = useState("");
  const [effort, setEffort] = useState("");
  const [mode, setMode] = useState("");
  const [prompt, setPrompt] = useState("");
  // One key for this sheet: a second click, or a retry after the answer was lost, starts no second session.
  const [key] = useState(() => crypto.randomUUID());
  const [started, setStarted] = useState<string>();
  const act = useAction();
  const title = codingAgentTitle(agent);

  // Options are the agent's on this machine: picking another one leaves none set.
  const pickMachine = (id: string) => (setMid(id), setAgent(undefined), setOptions());
  const pickAgent = (name: string) => (setAgent(name), setOptions());
  const setOptions = (m = "", e = "", md = "") => (setModel(m), setEffort(e), setMode(md));
  const own = a?.defaults?.model || a?.own?.model || "";
  const efforts = a ? (a.models.find((x) => x.id === (model || own))?.efforts ?? [...new Set(a.models.flatMap((x) => x.efforts))]) : [];

  // The new session opens once the list has it (the watch brings it a moment after the start returns).
  const { set } = st;
  const has = started !== undefined && st.sessions.some((s) => s.sessionId === started);
  useEffect(() => {
    if (!started) return;
    const open = () => set({ sheet: null, selection: { kind: "session", id: started } });
    if (has) return open();
    const late = setTimeout(open, 10_000);
    return () => clearTimeout(late);
  }, [started, has, set]);

  const ready = !!mid && !!agent && prompt.trim() !== "" && (fromRepo ? repo.trim() !== "" : project !== "");
  const start = async (e: FormEvent) => {
    e.preventDefault();
    if (!ready || act.busy) return;
    await act.go(async () => {
      const res = await local.startSession({
        machineId: mid,
        session: {
          idempotencyKey: key,
          agent,
          prompt,
          ...(fromRepo ? { repo: repo.trim(), baseRef: branch.trim() } : { projectId: project }),
          model,
          effort,
          mode,
        },
      });
      const s = res.session!;
      st.show(t("newSession.started", { title: displayTitle(s) }));
      setStarted(s.sessionId);
    });
  };
  const busy = act.busy || !!started;

  return (
    <form onSubmit={start}>
      <h3 id="sheet-title">{t("newSession.title")}</h3>
      <p className="sub">{t("newSession.sub")}</p>
      <div className="fgroup">
        <div className="frow">
          <span className="fl">{t("newSession.machine")}</span>
          <span className="fv">
            {online.length ? (
              <div className="seg" role="group" aria-label={t("newSession.machine")}>
                {online.map((m) => (
                  <button type="button" key={m.machineId} className={m.machineId === mid ? "on" : ""} aria-pressed={m.machineId === mid} onClick={() => pickMachine(m.machineId)}>
                    <Icon name={m.local ? "laptop" : (m.status?.platform ?? "").startsWith("darwin") ? "mini" : "server"} />
                    {m.machineName}
                  </button>
                ))}
              </div>
            ) : (
              <>
                {t("newSession.noMachines")} <ConnectLink>{t("newSession.howToConnect")}</ConnectLink>
              </>
            )}
          </span>
        </div>
        {machine && (
          <div className="frow">
            <span className="fl">{t("newSession.agent")}</span>
            <span className="fv">
              {agents.length ? (
                <div className="seg" role="group" aria-label={t("newSession.agent")}>
                  {agents.map((x) => (
                    <button type="button" key={x.name} className={x.name === agent ? "on" : ""} aria-pressed={x.name === agent} disabled={!!x.pause} title={x.pause ? tf("prepare.paused") : undefined} onClick={() => pickAgent(x.name)}>
                      <AgentLogo agent={x.name} size={14} />
                      {codingAgentTitle(x.name)}
                      {x.pause && <> · {tf("prepare.paused")}</>}
                    </button>
                  ))}
                  {usable.length === 0 && <span className="sub">{tf("prepare.allPaused")}</span>}
                </div>
              ) : (
                <>
                  {t("newSession.noAgents", { machine: machine.machineName })} <ConnectLink>{t("newSession.howToConnect")}</ConnectLink>
                </>
              )}
            </span>
          </div>
        )}
        {a && (
          <>
            <div className="frow">
              <span className="fl">{t("newSession.source")}</span>
              <span className="fv">
                <div className="seg" role="group" aria-label={t("newSession.source")}>
                  <button type="button" className={fromRepo ? "" : "on"} aria-pressed={!fromRepo} onClick={() => setFromRepo(false)}>{t("newSession.project")}</button>
                  <button type="button" className={fromRepo ? "on" : ""} aria-pressed={fromRepo} onClick={() => setFromRepo(true)}>{t("newSession.repo")}</button>
                </div>
              </span>
            </div>
            {!fromRepo ? (
              <div className="frow">
                <label className="fl" htmlFor="ns-project">{t("newSession.project")}</label>
                <span className="fv">
                  {projects.length ? (
                    <select id="ns-project" className="psel" style={{ width: 260 }} value={project} onChange={(e) => setProject(e.target.value)}>
                      {projects.map((p) => (
                        <option key={p.projectId} value={p.projectId}>{p.projectId}</option>
                      ))}
                    </select>
                  ) : (
                    <>
                      {t("newSession.noProjects", { machine: machine!.machineName, agent: title })}
                      <button type="button" className="link" onClick={() => st.set({ sheet: { kind: "addProject", machineId: mid, agent } })}>{t("newSession.addProject")}</button>
                    </>
                  )}
                </span>
              </div>
            ) : (
              <>
                <div className="frow">
                  <label className="fl" htmlFor="ns-repo">{t("newSession.repoLabel")}</label>
                  <span className="fv">
                    <input id="ns-repo" className="field mono" style={{ width: 260 }} placeholder={t("newSession.repoPlaceholder")} value={repo} onChange={(e) => setRepo(e.target.value)} />
                  </span>
                </div>
                <div className="frow">
                  <label className="fl" htmlFor="ns-branch">{t("newSession.branch")}</label>
                  <span className="fv">
                    <input id="ns-branch" className="field mono" style={{ width: 260 }} placeholder={t("newSession.branchPlaceholder")} value={branch} onChange={(e) => setBranch(e.target.value)} />
                  </span>
                </div>
              </>
            )}
            {a.models.length > 0 && (
              <div className="frow">
                <label className="fl" htmlFor="ns-model">{t("newSession.model")}</label>
                <span className="fv">
                  <select id="ns-model" className="psel" style={{ width: 260 }} value={model} onChange={(e) => setOptions(e.target.value, "", mode)}>
                    <option value="">{t("newSession.default")}</option>
                    {a.models.map((x) => (
                      <option key={x.id} value={x.id}>{x.name || x.id}</option>
                    ))}
                  </select>
                </span>
              </div>
            )}
            {efforts.length > 0 && (
              <div className="frow">
                <label className="fl" htmlFor="ns-effort">{t("newSession.effort")}</label>
                <span className="fv">
                  <select id="ns-effort" className="psel" style={{ width: 260 }} value={effort} onChange={(e) => setEffort(e.target.value)}>
                    <option value="">{t("newSession.default")}</option>
                    {efforts.map((x) => (
                      <option key={x} value={x}>{tf(`prepare.efforts.${x}`, { defaultValue: x })}</option>
                    ))}
                  </select>
                </span>
              </div>
            )}
            {a.modes.length > 0 && (
              <div className="frow">
                <label className="fl" htmlFor="ns-mode">{t("newSession.mode")}</label>
                <span className="fv">
                  <select id="ns-mode" className="psel" style={{ width: 260 }} value={mode} onChange={(e) => setMode(e.target.value)}>
                    <option value="">{t("newSession.default")}</option>
                    {a.modes.map((x) => (
                      <option key={x.id} value={x.id}>{x.name || x.id}</option>
                    ))}
                  </select>
                </span>
              </div>
            )}
          </>
        )}
      </div>
      {a && a.models.length === 0 && a.modes.length === 0 && <p className="sheet-note muted">{t("newSession.noOffers", { agent: title })}</p>}
      {a && fromRepo && <p className="sheet-note muted">{t("newSession.repoHint", { machine: machine!.machineName })}</p>}
      {a && (
        <>
          <label className="wk-lab" htmlFor="ns-prompt">{t("newSession.prompt")}</label>
          <textarea
            id="ns-prompt"
            className="field ns-prompt"
            rows={5}
            placeholder={t("newSession.promptPlaceholder", { agent: title })}
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            // ⌘↩ starts it, as Send would; ↩ alone breaks the line.
            onKeyDown={(e) => e.key === "Enter" && (e.metaKey || e.ctrlKey) && !e.nativeEvent.isComposing && start(e)}
          />
        </>
      )}
      <ErrorLine text={act.error} />
      <div className="sheet-foot">
        <button type="button" className="btn" onClick={() => st.set({ sheet: null })}>{t("cancel")}</button>
        <button type="submit" className="btn primary" disabled={busy || !ready}>
          {busy ? t("newSession.starting") : t("newSession.start")}
        </button>
      </div>
    </form>
  );
}
