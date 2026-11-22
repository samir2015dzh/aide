import { ConnectLink } from "./components";
import { shortPath } from "./agentPath";
import { lines } from "./connectGuideLogic";
import { useState, type ReactNode } from "react";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import { AgentSpeed, Workspace } from "@aide/proto/identity";
import { AgentProblem, type AgentStatus } from "@aide/proto/machine";
import type { Machine, Project } from "@aide/proto/local";
import { agentEntry, codingAgentIds, codingBotState, codingAgentTitle, defaultProjectName, projectIdFromFolder, projectTitle, type MachineConnections } from "@aide/core";
import { AgentLogo, AgentTile, Mark } from "@aide/ui";
import type { TerminalCommand } from "../../shared/bridge";
import { useRecheckAgents } from "./agents";
import { autoResumedAt, pauseOf, usageResetAt } from "./agentPause";
import { local } from "./api";
import { connDot, ConnRowView } from "./Connections";
import { Icon } from "./icons";
import { useNow, useStore } from "./store";
import "./agentlist.css";

// A machine's coding agents, as the first run's "Check and Manage Available
// Agents" shows them and settings' machine page too: every coding agent
// Aide supports, what the machine has of each (version, logged in), and
// each installed one opening its page (AgentPage): the model its sessions run
// with (Claude Code and Codex) and the projects it may work in. A project is
// one folder; it lists the agents it is for (none: all of them).

/** The projects agent may work in. */
export const projectsFor = (projects: Project[], agent: string) => projects.filter((p) => p.agents.length === 0 || p.agents.includes(agent));

export const ready = (a: AgentStatus) => a.available && a.loggedIn;

/** Found (or its app is), but it can't run: it says why rather than counting as not installed. */
export const broken = (a: AgentStatus) => !a.available && !a.checking && a.problem !== AgentProblem.UNSPECIFIED && a.problem !== AgentProblem.NOT_FOUND;

/** Why a broken agent can't run, and what to do. */
export function whyBroken(a: AgentStatus, t: TFunction<"firstrun">): string {
  const v = { agent: codingAgentTitle(a.name), binary: shortPath(a.binary), detail: a.detail };
  switch (a.problem) {
    case AgentProblem.APP_WITHOUT_CLI:
      return t("prepare.why.appWithoutCli", { ...v, app: a.detail.replace(/\.app$/, "") });
    case AgentProblem.NO_PROCESSES:
      return t("prepare.why.noProcesses", v);
    case AgentProblem.TIMED_OUT:
      return t("prepare.why.timedOut", v);
    case AgentProblem.MISSING_RUNTIME:
      return t("prepare.why.missingRuntime", { ...v, runtime: a.detail });
    default:
      return t("prepare.why.failed", v);
  }
}

/** A broken agent's state in a few words: its CLI is missing, or it can't run. */
function BrokenState({ a }: { a: AgentStatus }) {
  const { t } = useTranslation("firstrun");
  const cli = a.problem === AgentProblem.APP_WITHOUT_CLI;
  return (
    <span className={cli ? "warn" : "bad"}>
      <i />
      {t(cli ? "prepare.needsCli" : "prepare.cannotRun")}
    </span>
  );
}

/** Claude Code and Codex first, then the order Aide lists them in. */
const order = (name: string) => codingAgentIds.indexOf(name);

/**
 * A machine's agents sorted out: installed (ready first), still being checked
 * (not known yet), not installed; searching while none is known yet, and
 * known once the machine has said which it has.
 */
export function sortAgents(agents: AgentStatus[] | undefined) {
  // The ones the owner hid (SetAgentHidden) are left out of every group, and listed apart to be shown again.
  const shown = (agents ?? []).filter((a) => !a.hidden);
  const hidden = (agents ?? []).filter((a) => a.hidden).sort((a, b) => order(a.name) - order(b.name));
  const installed = shown.filter((a) => a.available).sort((a, b) => Number(ready(b)) - Number(ready(a)) || order(a.name) - order(b.name));
  const troubled = shown.filter(broken).sort((a, b) => order(a.name) - order(b.name));
  const checking = shown.filter((a) => a.checking && !a.available).sort((a, b) => order(a.name) - order(b.name));
  const listed = (id: string) => [...installed, ...troubled, ...checking, ...hidden].some((a) => a.name === id);
  const missing = codingAgentIds.filter((id) => !listed(id));
  return { installed, troubled, checking, missing, hidden, searching: !agents || (installed.length === 0 && checking.length > 0), known: !!agents?.length };
}

/**
 * The list: one row per installed agent, opening its page (onOpen), the ones
 * being checked, then the ones not installed; until the machine says which
 * it has, unknown instead. Projects none of the installed agents may use
 * (theirs is gone, or not known yet) follow, so they can still be removed.
 */
export function AgentList({ m, agents, unknown, onOpen, connections }: { m: Machine; agents: ReturnType<typeof sortAgents>; unknown?: ReactNode; onOpen: (agent: string) => void; connections?: MachineConnections }) {
  const { t } = useTranslation("firstrun");
  const st = useStore();
  const projects = st.projects.filter((p) => p.machineId === m.machineId);
  const { installed, troubled, checking, missing, hidden, known } = agents;
  const others = projects.filter((p) => !installed.some((a) => projectsFor([p], a.name).length > 0));
  const othersTitle = known ? t("prepare.otherProjects") : t("prepare.allProjects");
  // With the machine's connections (工单 #53, the machine page), each agent is one entry: its one state is the
  // connections' (stale or out of reach: unknown), and its connection rows (Claude Desktop and the command line, DSH's
  // bridge plugin) are details under it, not a second list.
  const entry = (name: string) => (connections ? agentEntry(connections, name) : undefined);
  const recheck = () => void local.checkAgents({ machineId: m.local ? "" : m.machineId }).catch(() => {});
  const details = (name: string, why = false) => {
    const e = entry(name);
    // An agent that can't run says why in its own words above (whyBroken): its one row doesn't say it again. Claude's
    // rows always say which part they are (Claude Desktop, the command line), even with the command line alone.
    const rows = why && e?.rows.length === 1 ? [{ ...e.rows[0], lacks: undefined }] : e?.rows;
    return e && rows ? (
      <div className="ag-conn">
        {rows.map((r) => (
          <ConnRowView key={r.id} r={r} m={m} recheck={recheck} logo={false} bare={rows.length === 1 && name !== "claude"} part />
        ))}
      </div>
    ) : null;
  };
  // Its one state in the words every page uses (工单 #56: the roster, Hand Work To, the counts, the map).
  const stateOf = (name: string) => {
    const b = connections ? codingBotState(m, name, new Date(), connections) : undefined;
    return b ? (
      <span className="conn-state ag-state" data-state={b.state} title={b.line}>
        <i className={`dotc ${connDot(b.state)}`} />
        {b.word}
      </span>
    ) : null;
  };
  return (
    <>
      <div className="ag-list" role="list" aria-label={t("prepare.agents")}>
        {!known && unknown}
        {installed.map((a) => {
          const set = defaultsText(a, t);
          return (
            <div key={a.name} className="ag" role="listitem" aria-label={codingAgentTitle(a.name)} title={a.detail || undefined}>
              <button className="ag-head" onClick={() => onOpen(a.name)}>
                <AgentTile agent={a.name} size={32} />
                <span className="ag-text">
                  <b>{codingAgentTitle(a.name)}</b>
                  <span className="ag-sub">
                    {/* With the connections, the version is in its state's words ("已登录 · 2.1.330"): not said twice. */}
                    {a.version && !entry(a.name) && <span className="mono">{a.version}</span>}
                    {stateOf(a.name) ?? (
                      <span className={a.loggedIn ? "ok" : "warn"}>
                        <i />
                        {a.loggedIn ? t("prepare.signedIn") : t("prepare.notSignedIn")}
                      </span>
                    )}
                    <PausedTag a={a} />
                    <span>{projectsFor(projects, a.name).length > 0 ? t("prepare.projects", { count: projectsFor(projects, a.name).length }) : t("prepare.noProjects")}</span>
                    {set && <span>{set}</span>}
                    {a.name === "claude" && usageText(a, t) && (
                      <span className="mono ag-usage" title={a.usage?.detail || undefined}>
                        {usageText(a, t)}
                      </span>
                    )}
                  </span>
                  {/* Which program was found and answered: what tells an installed agent from one uninstalled elsewhere. */}
                  {a.binary && (
                    <span className="mono ag-path" title={a.binary}>
                      {shortPath(a.binary)}
                    </span>
                  )}
                </span>
                <Icon name="chevR" className="ag-chev" />
              </button>
              {details(a.name)}
            </div>
          );
        })}
        {troubled.map((a) => (
          <div key={a.name} className="ag" role="listitem" aria-label={codingAgentTitle(a.name)}>
            <button className="ag-head" onClick={() => onOpen(a.name)}>
              <AgentTile agent={a.name} size={32} />
              <span className="ag-text">
                <b>{codingAgentTitle(a.name)}</b>
                <span className="ag-sub">
                  {stateOf(a.name) ?? <BrokenState a={a} />}
                  <PausedTag a={a} />
                  {projectsFor(projects, a.name).length > 0 && <span>{t("prepare.projects", { count: projectsFor(projects, a.name).length })}</span>}
                </span>
                {(entry(a.name)?.rows.length ?? 0) <= 1 && <span className="ag-why">{whyBroken(a, t)}</span>}
              </span>
              <Icon name="chevR" className="ag-chev" />
            </button>
            {details(a.name, true)}
          </div>
        ))}
        {checking.map((a) => (
          <div key={a.name} className="ag" role="listitem" aria-label={codingAgentTitle(a.name)}>
            <div className="ag-head">
              <AgentTile agent={a.name} size={32} />
              <span className="ag-text">
                <b>{codingAgentTitle(a.name)}</b>
                <span className="ag-sub">
                  <span className="ag-checking">
                    <Mark size={11} variant="seq" />
                    {t("prepare.checking")}
                  </span>
                </span>
              </span>
            </div>
          </div>
        ))}
      </div>
      {others.length > 0 && (
        <>
          <div className="ag-group">{othersTitle}</div>
          <div className="ag-prjs" role="list" aria-label={othersTitle}>
            {others.map((x) => (
              <ProjectRow key={x.projectId} p={x} />
            ))}
          </div>
        </>
      )}
      {hidden.length > 0 && (
        <p className="ag-note" role="note">
          {t("prepare.hiddenList", { count: hidden.length })}{" "}
          {hidden.map((a, i) => (
            <span key={a.name}>
              {i > 0 && " · "}
              {codingAgentTitle(a.name)}{" "}
              <button className="link" onClick={() => void st.run(() => local.setAgentHidden({ machineId: m.machineId, agent: a.name, hidden: false }))}>
                {t("prepare.show")}
              </button>
            </span>
          ))}
        </p>
      )}
      {known && missing.length > 0 && (
        <>
          <div className="ag-group">{t("prepare.notInstalled")}</div>
          <div className="ag-missing" role="list" aria-label={t("prepare.missing")}>
            {missing.map((name) => (
              <div key={name} role="listitem" aria-label={codingAgentTitle(name)}>
                <AgentLogo agent={name} size={16} />
                {codingAgentTitle(name)}
                {(lines as readonly string[]).includes(name) && <ConnectLink line={name}>{t("prepare.howToConnect")}</ConnectLink>}
              </div>
            ))}
          </div>
        </>
      )}
    </>
  );
}

type T = TFunction<"firstrun">;

const modelName = (a: AgentStatus, id: string) => a.models.find((x) => x.id === id)?.name || id;
const modeName = (a: AgentStatus, id: string) => a.modes.find((x) => x.id === id)?.name || id;
const effortName = (t: T, e: string) => t(`prepare.efforts.${e}`, { defaultValue: e });
const speedName = (t: T, v: AgentSpeed) => (v === AgentSpeed.FAST ? t("prepare.fast") : t("prepare.standard"));

/** What is set for the agent's sessions here, for its row: "" while it follows its own configuration. */
function defaultsText(a: AgentStatus, t: T): string {
  const d = a.defaults;
  if (!d) return "";
  return [d.model && modelName(a, d.model), d.effort && effortName(t, d.effort), d.speed ? speedName(t, d.speed) : "", d.mode && modeName(a, d.mode)].filter(Boolean).join(" · ");
}

/** The Claude plan usage for its row: "Plan 5h 20% · Week 59%", a stale
 * marker when kept after a failed refresh, or a short "unavailable" note.
 * "" when there is no reading at all (no token source wired). */
function usageText(a: AgentStatus, t: T): string {
  const u = a.usage;
  if (!u) return "";
  if (u.unavailable) return t("prepare.usageUnreadable");
  if (u.windows.length === 0) return "";
  const label = (k: string) => (k === "five_hour" ? t("prepare.usage5h") : k === "seven_day" ? t("prepare.usageWeek") : k.replace("seven_day_", "7d "));
  const parts = u.windows.map((w) => `${label(w.key)} ${Math.round(w.utilization)}%`);
  let s = `${t("prepare.usageLabel")} ${parts.join(" · ")}`;
  if (u.stale) s += ` (${t("prepare.usageStale")})`;
  return s;
}

const logins: Record<string, { key: TerminalCommand; text: string }> = {
  codex: { key: "codexLogin", text: "codex login" },
  claude: { key: "claudeLogin", text: "claude" },
  antigravity: { key: "antigravityLogin", text: "aide agent login antigravity" },
};

/**
 * An agent's page on a machine, opened from its row: signing in when it
 * isn't, the model, reasoning effort, (Codex) speed and (ACP agents) mode its
 * sessions run with when the agent starting one names no model, and the
 * projects it may work in.
 */
export function AgentPage({ m, agent, bare = false }: { m: Machine; agent: string; bare?: boolean }) {
  const { t } = useTranslation("firstrun");
  const st = useStore();
  const a = m.status?.agents.find((x) => x.name === agent);
  const [busy, setBusy] = useState(false);
  const [ran, setRan] = useState(false);
  useRecheckAgents(ran && !a?.loggedIn);
  const title = codingAgentTitle(agent);
  const projects = st.projects.filter((p) => p.machineId === m.machineId);
  const mine = projectsFor(projects, agent);
  // Its state in the same words as its entry on the machine's list (工单 #53): one account, never two.
  const now = useNow(30_000);
  const e = codingBotState(m, agent, now);
  // Logging in runs in this Mac's Terminal: another machine's agent is logged in there.
  const login = m.local ? logins[agent] : undefined;

  // On this Mac the folder is picked: one already a project gets this agent
  // too; a new one is for this agent only. Another machine's is typed in.
  const add = async () => {
    if (!m.local) return st.set({ sheet: { kind: "addProject", machineId: m.machineId, agent } });
    const path = await window.aide?.pickDirectory();
    if (!path) return;
    const same = projects.find((x) => x.path === path);
    setBusy(true);
    if (same) {
      if (same.agents.length > 0 && !same.agents.includes(agent)) await st.run(() => save(same, [...same.agents, agent]), t("prepare.alsoFor", { project: same.projectId, agent: title }));
    } else {
      const projectId = projectIdFromFolder(path.split(/[\\/]/).filter(Boolean).pop() ?? "", new Set(projects.map((x) => x.projectId)));
      await st.run(() => local.addProject({ machineId: m.machineId, projectId, path, agents: [agent] }));
    }
    setBusy(false);
  };

  return (
    <div className="ag-page" role={bare ? undefined : "region"} aria-label={bare ? undefined : title}>
      {/* bare: on its bot's page (工单 #57), whose head already says who it is and how it stands. */}
      {!bare && <div className="ag-hero">
        <AgentTile agent={agent} size={44} />
        <span className="ag-text">
          <b>{title}</b>
          <span className="ag-sub">
            <span>{m.machineName}</span>
            {a?.version && <span className="mono">{a.version}</span>}
            {a && e ? (
              <span className="conn-state ag-state" data-state={e.state} title={e.line}>
                <i className={`dotc ${connDot(e.state)}`} />
                {e.word}
              </span>
            ) : a && (broken(a) ? <BrokenState a={a} /> : (
              <span className={a.loggedIn ? "ok" : "warn"}>
                <i />
                {a.loggedIn ? t("prepare.signedIn") : t("prepare.notSignedIn")}
              </span>
            ))}
          </span>
        </span>
      </div>}
      {a && broken(a) && <p className="ag-why">{whyBroken(a, t)}</p>}
      {a && a.available && !a.loggedIn && (
        <div className="ag-login">
          {ran ? (
            <>
              <Mark size={12} variant="seq" />
              {t("prepare.waitingLogin")}
            </>
          ) : (
            <>
              <span>{t("prepare.loginFirst", { agent: title })}</span>
              {login && (
                <>
                  <span className="chip mono">{login.text}</span>
                  <button className="btn sm" onClick={() => (window.aide?.runInTerminal(login.key), setRan(true))}>
                    <Icon name="terminal" />
                    {t("prepare.runInTerminal")}
                  </button>
                </>
              )}
            </>
          )}
        </div>
      )}
      {a && (a.models.length > 0 || a.modes.length > 0) && <Defaults a={a} m={m} />}
      {a && a.modelCatalogs.length > 0 && <ModelCatalogs a={a} />}
      <section aria-label={t("prepare.projectsHead")}>
        <div className="ag-group">{t("prepare.projectsHead")}</div>
        {mine.length > 0 && (
          <div className="ag-prjs">
            {mine.map((x) => (
              <ProjectRow key={x.projectId} p={x} agent={agent} />
            ))}
          </div>
        )}
        <button className="ag-add" onClick={add} disabled={busy || (!m.online && !m.local)}>
          <Icon name="plus" />
          {busy ? t("prepare.adding") : t("prepare.addProject")}
        </button>
      </section>
      {a && <PauseSection a={a} m={m} />}
      {/* The owner's say over a listing they don't trust (SetAgentHidden): hiding only, as nothing here can be edited. */}
      {/* The Claude app's own sessions are read in the main window's Claude page; DSH's (those opened in DSH Desktop) in its DeepSeek page. */}
      {(agent === "claude" || agent === "deepseek") && (
        <section className="ag-dsh" aria-label={t(agent === "claude" ? "prepare.claudeSessions" : "prepare.dshSessions")}>
          <div className="ag-group">{t(agent === "claude" ? "prepare.claudeSessions" : "prepare.dshSessions")}</div>
          <div className="fgroup">
            <button className="frow act" onClick={() => window.aide?.showInMain(agent === "claude" ? { claude: true } : { dsh: true })}>
              {t("prepare.dshSessionsOpen")}
              <Icon name="chevR" className="sm chev" />
            </button>
          </div>
        </section>
      )}
      {/* The owner's say over a listing they don't trust (SetAgentHidden): hiding only, as nothing here can be edited. */}
      {a && !a.hidden && (
        <section className="ag-hide" aria-label={t("prepare.hide")}>
          <div className="fgroup">
            <button className="frow act danger" onClick={() => void st.run(() => local.setAgentHidden({ machineId: m.machineId, agent, hidden: true }))}>
              {t("prepare.hide")}
            </button>
          </div>
          <p className="ag-note">{t("prepare.hideNote")}</p>
        </section>
      )}
    </div>
  );
}

/** "Paused" on an agent's row, with when it resumes by itself if it does (SetAgentPaused). */
function PausedTag({ a }: { a: AgentStatus }) {
  const { t } = useTranslation("firstrun");
  const p = pauseOf(a);
  if (!p) return null;
  return <span className="warn ag-paused">{p.until ? t("prepare.pausedUntil", { when: p.until.toLocaleString() }) : t("prepare.paused")}</span>;
}

/**
 * Pause or resume the agent on this machine (SetAgentPaused), beside hiding it and not in its place: paused, no new
 * session starts with it there, those under way run on, and nothing is removed. Where the machine read when its plan's
 * usage resets (Claude Code), it can be paused until then, to resume by itself.
 */
function PauseSection({ a, m }: { a: AgentStatus; m: Machine }) {
  const { t } = useTranslation("firstrun");
  const st = useStore();
  const now = useNow(60_000);
  const p = pauseOf(a);
  const reset = usageResetAt(a, now);
  const resumed = autoResumedAt(a);
  const set = (paused: boolean, until?: Date) =>
    void st.run(() =>
      local.setAgentPaused({
        machineId: m.machineId,
        agent: a.name,
        paused,
        resumeAt: until ? { seconds: BigInt(Math.floor(until.getTime() / 1000)), nanos: 0 } : undefined,
      }),
    );
  return (
    <section className="ag-hide" aria-label={t("prepare.pauseHead")}>
      <div className="ag-group">{t("prepare.pauseHead")}</div>
      {p ? (
        <>
          <div className="fgroup">
            <button className="frow act" onClick={() => set(false)}>{t("prepare.resume")}</button>
          </div>
          <p className="ag-note">{p.until ? t("prepare.pausedUntilNote", { when: p.until.toLocaleString() }) : t("prepare.pausedNote")}</p>
        </>
      ) : (
        <>
          <div className="fgroup">
            <button className="frow act" onClick={() => set(true)}>{t("prepare.pause")}</button>
            {reset && <button className="frow act" onClick={() => set(true, reset)}>{t("prepare.pauseUntilReset", { when: reset.toLocaleString() })}</button>}
          </div>
          <p className="ag-note">{t("prepare.pauseNote")}</p>
          {resumed && <p className="ag-note">{t("prepare.autoResumed", { when: resumed.toLocaleString() })}</p>}
        </>
      )}
    </section>
  );
}

/** Read-only contextual observations, separate from machine-wide defaults. */
function ModelCatalogs({ a }: { a: AgentStatus }) {
  const { t } = useTranslation("firstrun");
  const now = useNow(1000);
  return (
    <section aria-label={t("prepare.catalogsTitle")}>
      <div className="ag-group">{t("prepare.catalogsTitle")}</div>
      <p className="ag-note">{t("prepare.catalogsNote")}</p>
      {a.modelCatalogs.map((c) => (
        <div className="ag-prjs ag-catalog" role="group" aria-label={c.scope} key={c.scope}>
          <p className="ag-note">{c.scope} · {t(`prepare.catalogState.${c.state}`, { defaultValue: t("prepare.catalogState.unknown") })}</p>
          <p className="ag-note">{t("prepare.catalogSource", { source: c.source || "—" })} · {c.observedAtUnixMs ? new Date(Number(c.observedAtUnixMs)).toLocaleString() : t("prepare.catalogNever")}</p>
          {c.expiresAtUnixMs > 0n && Number(c.expiresAtUnixMs) <= now.getTime() && <p className="ag-note">{t("prepare.catalogExpired")}</p>}
          {c.models.map((m) => <p className="ag-note mono" key={m.id}>{m.id}</p>)}
          {c.state === "known" && c.models.length === 0 && <p className="ag-note">{t("prepare.catalogEmpty")}</p>}
          {c.defaultModel && <p className="ag-note">{t("prepare.catalogInitial", { model: c.defaultModel })}</p>}
          {c.currentModel && <p className="ag-note">{t("prepare.catalogCurrent", { model: c.currentModel })}</p>}
        </div>
      ))}
    </section>
  );
}

/**
 * The model, reasoning effort, (Codex) speed and (ACP agents, once a session
 * of theirs said what they offer) mode an agent's sessions run with when
 * the agent starting one names no model, one row each: the agent's own
 * configuration unless set here; set here, only Aide's sessions use them.
 * Each row's button shows the value and opens a menu of the choices.
 */
function Defaults({ a, m }: { a: AgentStatus; m: Machine }) {
  const { t } = useTranslation("firstrun");
  const st = useStore();
  const title = codingAgentTitle(a.name);
  const d = { model: a.defaults?.model ?? "", effort: a.defaults?.effort ?? "", speed: a.defaults?.speed ?? AgentSpeed.UNSPECIFIED, mode: a.defaults?.mode ?? "" };
  const own = a.own;
  const same = (v: string) => (v ? t("prepare.sameAsValue", { agent: title, value: v }) : t("prepare.sameAs", { agent: title }));
  const effortsOf = (id: string) => a.models.find((x) => x.id === id)?.efforts;
  const model = d.model || own?.model || "";

  const save = (next: typeof d) => st.run(() => local.setAgentDefaults({ machineId: m.machineId, defaults: { agent: a.name, ...next } }));
  const pick = async (items: { id: string; label: string; checked: boolean }[], set: (v: string) => void) => {
    const id = await window.aide?.popupMenu(items);
    if (id == null) return;
    set(id);
  };
  const rows = [
    {
      key: "model",
      label: t("prepare.model"),
      value: d.model ? modelName(a, d.model) : same(own?.model ? modelName(a, own.model) : ""),
      open: () =>
        pick(
          [{ id: "", label: same(own?.model ? modelName(a, own.model) : ""), checked: !d.model }, ...a.models.map((x) => ({ id: x.id, label: x.name || x.id, checked: d.model === x.id }))],
          (v) => {
            // An effort the newly picked model lacks goes back to the agent's own.
            const ok = effortsOf(v || own?.model || "");
            save({ ...d, model: v, effort: d.effort && ok && !ok.includes(d.effort) ? "" : d.effort });
          },
        ),
    },
    {
      key: "effort",
      label: t("prepare.effort"),
      value: d.effort ? effortName(t, d.effort) : same(own?.effort ? effortName(t, own.effort) : ""),
      open: () =>
        pick(
          [
            { id: "", label: same(own?.effort ? effortName(t, own.effort) : ""), checked: !d.effort },
            ...(effortsOf(model) ?? a.models[0].efforts).map((e) => ({ id: e, label: effortName(t, e), checked: d.effort === e })),
          ],
          (v) => save({ ...d, effort: v }),
        ),
    },
    ...(a.name === "codex"
      ? [
          {
            key: "speed",
            label: t("prepare.speed"),
            value: d.speed ? speedName(t, d.speed) : same(own?.speed ? speedName(t, own.speed) : ""),
            open: () =>
              pick(
                [
                  { id: "0", label: same(own?.speed ? speedName(t, own.speed) : ""), checked: !d.speed },
                  { id: "1", label: t("prepare.standard"), checked: d.speed === AgentSpeed.STANDARD },
                  { id: "2", label: t("prepare.fast"), checked: d.speed === AgentSpeed.FAST },
                ],
                (v) => save({ ...d, speed: Number(v) }),
              ),
          },
        ]
      : []),
    ...(a.modes.length > 0
      ? [
          {
            key: "mode",
            label: t("prepare.mode"),
            value: d.mode ? modeName(a, d.mode) : same(own?.mode ? modeName(a, own.mode) : ""),
            open: () =>
              pick(
                [{ id: "", label: same(own?.mode ? modeName(a, own.mode) : ""), checked: !d.mode }, ...a.modes.map((x) => ({ id: x.id, label: x.name || x.id, checked: d.mode === x.id }))],
                (v) => save({ ...d, mode: v }),
              ),
          },
        ]
      : []),
  ];
  // An ACP agent may offer modes and no model, and tells the efforts of the
  // models its sessions ran only.
  const shown = rows.filter((r) => (r.key !== "model" || a.models.length > 0) && (r.key !== "effort" || a.models.some((x) => x.efforts.length > 0)));
  return (
    <section aria-label={t("prepare.defaultsHead")}>
      <div className="ag-group">{t("prepare.defaultsHead")}</div>
      <div className="ag-prjs">
        {shown.map((r) => (
          <div key={r.key} className="ag-set" role="group" aria-label={r.label}>
            <span>{r.label}</span>
            <button className="ag-pop" onClick={r.open} disabled={!m.online && !m.local}>
              {r.value}
              <Icon name="chevD" />
            </button>
          </div>
        ))}
      </div>
      <p className="ag-note">{t("prepare.defaultsNote", { agent: title })}</p>
    </section>
  );
}

/** Adds the project again, as it is, for agents (adding again changes it). */
const save = (p: Project, agents: string[], workspace = p.workspace) =>
  local.addProject({ machineId: p.machineId, projectId: p.projectId, path: p.path, workspace, defaultBaseRef: workspace === Workspace.IN_PLACE ? "" : p.defaultBaseRef, description: p.description, agents });

/**
 * A project of one agent: its name and way of working, its folder, and a
 * ⋯ menu to change the way of working or take it from this agent. Of no
 * installed agent (agent unset): the agents it is for, and removing it.
 */
function ProjectRow({ p, agent }: { p: Project; agent?: string }) {
  const { t } = useTranslation("firstrun");
  const st = useStore();
  // Its name as the owner knows it (工单 #50): changed in place; empty goes back to its id.
  const [naming, setNaming] = useState<string>();
  const rename = async (name: string) => {
    setNaming(undefined);
    if (name.trim() === (p.name ?? "")) return;
    await st.run(() => local.renameProject({ machineId: p.machineId, projectId: p.projectId, name: name.trim() }), t("prepare.renamed"));
  };
  const worktree = p.workspace !== Workspace.IN_PLACE;
  const how = worktree ? t("prepare.worktree") : t("prepare.inPlace");
  const more = async () => {
    const id = await window.aide?.popupMenu([
      { id: "rename", label: t("prepare.renameItem") },
      { separator: true },
      { id: "worktree", label: t("prepare.worktreeItem"), checked: worktree },
      { id: "inPlace", label: t("prepare.inPlaceItem"), checked: !worktree },
      { separator: true },
      { id: "remove", label: agent ? t("prepare.removeItem", { agent: codingAgentTitle(agent) }) : t("machineOnly.removeProject") },
    ]);
    if (id === "rename") return setNaming(projectTitle(p));
    if (id === "worktree" && !worktree) await st.run(() => save(p, p.agents, Workspace.WORKTREE));
    if (id === "inPlace" && worktree) await st.run(() => save(p, p.agents, Workspace.IN_PLACE));
    if (id === "remove") {
      // Taken from the last agent it is for, it goes; one for all stays for the others.
      const rest = agent ? (p.agents.length > 0 ? p.agents : codingAgentIds).filter((x) => x !== agent) : [];
      await st.run(() => (rest.length > 0 ? save(p, rest) : local.removeProject({ machineId: p.machineId, projectId: p.projectId })));
    }
  };
  return (
    <div className="ag-prj" role="group" aria-label={projectTitle(p)}>
      <Icon name="folder" />
      <div className="ag-prj-t">
        <span>
          {naming !== undefined ? (
            <input
              className="field sm"
              autoFocus
              maxLength={64}
              aria-label={t("prepare.renameItem")}
              value={naming}
              onChange={(e) => setNaming(e.target.value)}
              onBlur={() => void rename(naming)}
              onKeyDown={(e) => (e.key === "Enter" ? void rename(naming) : e.key === "Escape" && setNaming(undefined))}
            />
          ) : (
            <>
              <b title={p.name ? p.projectId : undefined}>{projectTitle(p)}</b>
              {/* Renaming is a pencil (工单 #65): what it does is its tip, never a sentence standing in the name's place. */}
              <button className="tb ag-prj-edit" title={t("prepare.renameTip", { name: projectTitle(p) })} aria-label={t("prepare.renameTip", { name: projectTitle(p) })} onClick={() => setNaming(projectTitle(p))}>
                <Icon name="edit" />
              </button>
            </>
          )}
          <em>{agent ? how : [how, ...p.agents.map(codingAgentTitle)].join(" · ")}</em>
        </span>
        <span className="mono" title={p.path}>
          {p.path}
        </span>
      </div>
      <button className="tb" aria-label={t("prepare.more")} onClick={more}>
        <Icon name="more" />
      </button>
    </div>
  );
}
