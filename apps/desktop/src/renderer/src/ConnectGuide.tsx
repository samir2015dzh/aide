import { useCallback, useEffect, useState, type ReactNode } from "react";
import { Check } from "./Picker";
import { useTranslation } from "react-i18next";
import type { ACPRegistryEntry, CustomAgent, ListACPRegistryResponse } from "@aide/proto/machine";
import type { Machine } from "@aide/proto/local";
import { codingAgentIds, codingAgentTitle, projectIdFromFolder, projectTitle } from "@aide/core";
import { AgentTile, Mark } from "@aide/ui";
import { projectsFor, whyBroken } from "./AgentList";
import { ConnectionsCard } from "./Connections";
import { local } from "./api";
import { agentOf, detect, fixes, formOf, formProblem, installs, lines, looksSecret, needs, notLoggedIn, terminalCommand, type CustomForm, type Line } from "./connectGuideLogic";
import { Icon } from "./icons";
import { needsUpdate } from "./skew";
import { errorText, useStore } from "./store";
import "./connectGuide.css";

// The Connect guide (the first run's Connect step, and Settings › Connect):
// a separate line for each coding agent the owner connects (DeepSeek
// Harness, Claude, Codex) and one for any other ACP agent, each with what
// must be on the computer first, what to click here, whether the machine
// detects it now (and what to check when it doesn't), a test that runs it,
// and where its tasks show. Assistants (the agents that give tasks) connect
// on their own tab, which the page holding the guide provides.

export type Tab = Line | "assistants";

/** The tabs: each line, then the assistants. */
export function LinePicker({ tab, set }: { tab: Tab; set: (t: Tab) => void }) {
  const { t } = useTranslation("connect");
  return (
    <div className="seg cg-tabs" role="tablist" aria-label={t("title")}>
      {[...lines, "assistants" as const].map((x) => (
        <button key={x} type="button" role="tab" className={x === tab ? "on" : ""} aria-selected={x === tab} onClick={() => set(x)}>
          {/* Every tab with its icon, the same size and baseline (工单 #45): those without a logo get a generic one. */}
          {x === "custom" ? <IconTile name="puzzle" size={18} /> : x === "assistants" ? <IconTile name="persons" size={18} /> : <AgentTile agent={x} size={18} />}
          <span>{t(`lines.${x}`)}</span>
        </button>
      ))}
    </div>
  );
}

/** A generic icon drawn as an agent's logo tile is, for the tabs without a logo of their own. */
function IconTile({ name, size }: { name: string; size: number }) {
  return (
    <span className="cg-itile" style={{ width: size, height: size, borderRadius: size * 0.28 }} aria-hidden="true">
      <Icon name={name} />
    </span>
  );
}

/** Where the guide is: what its buttons that open things in the main window do. */
export type Place = {
  /** Opens a task the guide started. */
  openSession: (id: string) => void;
  /** Opens DeepSeek Harness's own sessions. */
  openDSH: () => void;
};

/** One line's guide, on the computer chosen (this one first). */
export function ConnectGuide({ line, place }: { line: Line; place: Place }) {
  const { t } = useTranslation("connect");
  const st = useStore();
  const online = st.machines.filter((m) => m.online || m.local);
  const [chosen, setChosen] = useState<string>();
  const m = online.find((x) => x.machineId === chosen) ?? online.find((x) => x.local) ?? online[0];
  if (!m) return <p className="cg-note">{t("noMachine")}</p>;
  return (
    <div className="cg" key={`${line}:${m.machineId}`}>
      <p className="cg-intro">{t(`${line}.intro`)}</p>
      {online.length > 1 && (
        <div className="cg-machine">
          <span>{t("machine")}</span>
          <div className="seg" role="group" aria-label={t("machine")}>
            {online.map((x) => (
              <button type="button" key={x.machineId} className={x.machineId === m.machineId ? "on" : ""} aria-pressed={x.machineId === m.machineId} onClick={() => setChosen(x.machineId)}>
                {x.machineName}
              </button>
            ))}
          </div>
          <span className="cg-note">{t("machineNote")}</span>
        </div>
      )}
      {/* What Aide already knows comes first: the form below is for what it doesn't (工单 #136 P09). */}
      {line === "custom" && <KnownAgents m={m} />}
      <Steps title={t("before")} items={needs(line, lineState(m, line)).map((k) => t(k as never, { machine: m.machineName }))} />
      <Steps title={t("steps")} items={[1, 2, 3, 4, 5].map((n) => t(`${line}.step${n}` as never))} ordered />
      {line === "custom" ? <Customs m={m} place={place} /> : <Agent m={m} agent={line} line={line} place={place} />}
    </div>
  );
}

/** How a line's agent stands on m now (the custom line has none of its own). */
const lineState = (m: Machine, line: Line) =>
  line === "custom" ? "checking" : detect(m.status?.agents.find((x) => x.name === line), m.online || m.local, !!m.status?.agents.length);

function Steps({ title, items, ordered }: { title: string; items: ReactNode[]; ordered?: boolean }) {
  const List = ordered ? "ol" : "ul";
  return (
    <section className="cg-sec" aria-label={title}>
      <div className="fhead">{title}</div>
      <List className="cg-list">
        {items.map((x, i) => (
          <li key={i}>{x}</li>
        ))}
      </List>
    </section>
  );
}

/** Checks the machine's agents again: this computer's in the background, another's end to end. */
const recheck = (m: Machine) => local.checkAgents(m.local ? { onPurpose: true } : { machineId: m.machineId });

/** An agent's status, what to try, the test, and where its tasks show. */
function Agent({ m, agent, line, place }: { m: Machine; agent: string; line: Line; place: Place }) {
  const { t } = useTranslation("connect");
  const tf = useTranslation("firstrun").t;
  const a = m.status?.agents.find((x) => x.name === agent);
  const d = detect(a, m.online || m.local, !!m.status?.agents.length);
  const [checking, setChecking] = useState(false);
  const check = useCallback(async () => {
    setChecking(true);
    await recheck(m).catch(() => {}); // the next status shows what came of it
    setChecking(false);
  }, [m.machineId]); // eslint-disable-line react-hooks/exhaustive-deps
  // Opening the line checks the machine's agents: what it says is now.
  useEffect(() => void check(), [check]);
  const v = { machine: m.machineName, agent: codingAgentTitle(agent), app: a?.detail.replace(/\.app$/, "") ?? "", version: a?.version ?? "" };
  const windows = (m.status?.platform ?? "").startsWith("windows");
  // Claude Code signs in from Aide (工单 #51: Connection details below); Codex and Antigravity in a terminal.
  const login = agent === "codex" ? "login" : undefined;
  const antigravityLoginCmd = agent === "antigravity" ? "aide agent login antigravity" : undefined;
  const state = d === "ready" && a?.version ? t("state.readyVersion", v) : d === "signIn" && agent === "claude" ? t("state.signInClaude", v) : t(`state.${d}`, v);
  return (
    <>
      <section className="cg-sec" aria-label={t("status")}>
        <div className="fhead">{t("status")}</div>
        <div className={`cg-status ${d}`} role="status">
          {d === "checking" ? <Mark size={12} variant="seq" /> : <i />}
          <span>{state}</span>
          {d !== "offline" && (
            <button className="btn sm" disabled={checking || d === "checking"} onClick={check}>
              {checking ? t("checking") : t("checkAgain")}
            </button>
          )}
        </div>
        {installs[line] && (d === "missing" || d === "appOnly" || d === "broken") && <Install m={m} line={line} registryId={installs[line]} />}
        {fixes(line, d).length > 0 && (
          <div className="cg-fix">
            <span>{t("tryThis")}</span>
            <ul className="cg-list">
              {fixes(line, d).map((k) => (
                <li key={k}>
                  {t(k as never, { ...v, reason: a ? whyBroken(a, tf) : "" })}
                  {d === "signIn" && login !== undefined && a?.binary && k.endsWith("fixSignIn") && <CopyLine text={terminalCommand(a.binary, login, windows)} />}
                  {d === "signIn" && antigravityLoginCmd && k.endsWith("fixSignIn") && <CopyLine text={antigravityLoginCmd} />}
                </li>
              ))}
            </ul>
          </div>
        )}
      </section>
      {/* The same rows as the machine's page: Claude Desktop and Claude Code apart, DSH and its plugin (工单 #44, #46, #49, #111). */}
      {(agent === "claude" || agent === "deepseek" || agent === "antigravity") && d !== "offline" && (
        <section className="cg-sec" aria-label={t("details")}>
          <div className="fhead">{t("details")}</div>
          <ConnectionsCard m={m} only={(r) => r.agent === agent} />
        </section>
      )}
      {/* A trial run is what confirms an ACP agent's sign-in (工单 #112): offered while it is unconfirmed too. */}
      <Verify m={m} agent={agent} line={line} ready={d === "ready" || d === "unconfirmed"} place={place} />
      <Steps
        title={t("where")}
        items={[
          ...(line === "custom" ? [] : [t(`${line}.where1`)]),
          ...(line === "deepseek" ? [t("deepseek.where2"), t("deepseek.where3")] : []),
          ...(line === "claude" ? [t("claude.where2"), t("claude.where3")] : []),
        ]}
      />
      {line === "deepseek" && (
        <div className="cg-acts">
          <button className="btn" onClick={place.openDSH}>
            <Icon name="terminal" />
            {t("deepseek.openSessions")}
          </button>
        </div>
      )}
      {line === "claude" && <p className="cg-note">{t("claude.assistant")}</p>}
    </>
  );
}

/**
 * Installs the agent's program on m from the official ACP registry, where the agent finds it (工单 #133): one button, its
 * download checked against the registry's SHA-256, done only once the machine finds the agent there; else why not.
 */
function Install({ m, line, registryId }: { m: Machine; line: Line; registryId: string }) {
  const { t } = useTranslation("connect");
  const [state, setState] = useState<{ busy: boolean; error?: string }>({ busy: false });
  const install = async () => {
    setState({ busy: true });
    try {
      await local.installMachineACPRegistryAgent({ machineId: m.machineId, registryId });
      await recheck(m).catch(() => {}); // the next status says it is detected
      setState({ busy: false });
    } catch (e) {
      setState({ busy: false, error: errorText(e) });
    }
  };
  return (
    <div className="cg-install" role="group" aria-label={t(`${line}.install` as never)}>
      <p>{t(`${line}.installWhat` as never, { machine: m.machineName })}</p>
      <button className="btn primary" disabled={state.busy} onClick={install}>
        {state.busy ? <Mark size={12} variant="seq" /> : <Icon name="arrowDown" />}
        {state.busy ? t(`${line}.installing` as never) : t(`${line}.install` as never)}
      </button>
      {state.error && <p className="cg-err" role="alert">{t("installFailed", { reason: state.error })}</p>}
    </div>
  );
}

/** A command to type, and a button that copies it. */
function CopyLine({ text }: { text: string }) {
  const { t } = useTranslation("firstrun");
  const [copied, setCopied] = useState(false);
  return (
    <span className="cg-copy">
      <code className="am-chip sel-text">{text}</code>
      <button className="btn sm" onClick={() => navigator.clipboard.writeText(text).then(() => setCopied(true))}>
        <Icon name={copied ? "check" : "copy"} />
        {copied ? t("copied") : t("copy")}
      </button>
    </span>
  );
}

/**
 * The test: the agent answers one short question (TestMachineAgent), then a
 * real task in one of its projects, opened like any task. Without a project
 * the owner adds one first.
 */
function Verify({ m, agent, line, ready, place }: { m: Machine; agent: string; line: Line; ready: boolean; place: Place }) {
  const { t } = useTranslation("connect");
  const st = useStore();
  const title = codingAgentTitle(agent);
  const projects = projectsFor(
    st.projects.filter((p) => p.machineId === m.machineId),
    agent,
  );
  const [test, setTest] = useState<{ busy?: boolean; ok?: boolean; text?: string }>({});
  const [started, setStarted] = useState<string>();
  const [starting, setStarting] = useState(false);
  const [adding, setAdding] = useState(false);
  const [key] = useState(() => crypto.randomUUID());
  const run = async () => {
    setTest({ busy: true });
    try {
      const r = await local.testMachineAgent({ machineId: m.machineId, agent });
      // Its own "Not logged in · Please run /login" is said in words, never as it printed it (工单 #44).
      if (agent === "claude" && notLoggedIn(r.reply)) setTest({ ok: false, text: t("test.notLoggedIn") });
      else setTest({ ok: r.ok, text: r.ok ? t("test.ok", { agent: title, reply: r.reply, seconds: (r.millis / 1000).toFixed(1) }) : t("test.failed", { reason: r.reply }) });
    } catch (e) {
      setTest({ ok: false, text: t("test.failed", { reason: errorText(e) }) });
    }
  };
  const start = async () => {
    if (!projects.length) return;
    setStarting(true);
    try {
      const r = await local.startSession({ machineId: m.machineId, session: { idempotencyKey: key, agent, projectId: projects[0].projectId, prompt: t("test.prompt") } });
      setStarted(r.session!.sessionId);
      st.show(t("test.started"));
    } catch (e) {
      st.show(errorText(e));
    } finally {
      setStarting(false);
    }
  };
  // On this computer the folder is picked; another computer's is typed in.
  const add = async () => {
    if (!m.local) return st.set({ sheet: { kind: "addProject", machineId: m.machineId, agent } });
    const path = await window.aide?.pickDirectory();
    if (!path) return;
    setAdding(true);
    const projectId = projectIdFromFolder(path.split(/[\\/]/).filter(Boolean).pop() ?? "", new Set(st.projects.filter((p) => p.machineId === m.machineId).map((x) => x.projectId)));
    await st.run(() => local.addProject({ machineId: m.machineId, projectId, path, agents: [agent] }));
    setAdding(false);
  };
  const names = projects.map(projectTitle).join(t("listSeparator")); // 、 in Chinese (工单 #136 P32)
  const addButton = (
    <div className="cg-acts">
      <button className="btn" onClick={add} disabled={adding}>
        <Icon name="plus" />
        {adding ? t("project.adding") : t("project.add")}
      </button>
    </div>
  );
  // Not usable yet: one line saying where to go (the status above), no buttons that can't be pressed (工单 #136 P16).
  if (!ready)
    return (
      <section className="cg-sec" aria-label={t("verify")}>
        <div className="fhead">{t("verify")}</div>
        <div className="cg-box">
          <p className="cg-note">{t("test.notReady", { agent: title, machine: m.machineName })}</p>
        </div>
      </section>
    );
  return (
    <section className="cg-sec" aria-label={t("verify")}>
      <div className="fhead">{t("verify")}</div>
      <div className="cg-box">
        {projects.length > 0 && (
          <>
            <p>{t("project.some", { count: projects.length, agent: title, machine: m.machineName, names })}</p>
            {addButton}
          </>
        )}
        <p className="cg-note">{t("test.runNote")}</p>
        <div className="cg-acts">
          <button className="btn primary" onClick={run} disabled={test.busy}>
            {test.busy ? t("test.running", { agent: title }) : t("test.run")}
          </button>
        </div>
        {test.text && (
          <p className={`cg-result ${test.ok ? "ok" : "bad"}`} role="status">
            {test.text}
            {!test.ok && line !== "custom" && <span className="cg-note"> {t(`${line}.testFailedHint`)}</span>}
          </p>
        )}
        {projects.length ? (
          <>
            <p className="cg-note">{t("test.startNote", { project: projects[0].projectId })}</p>
            <div className="cg-acts">
              <button className="btn" onClick={start} disabled={starting || !!started}>
                {starting ? t("test.starting") : t("test.start")}
              </button>
              {started && (
                <button className="btn primary" onClick={() => place.openSession(started)}>
                  {t("test.open")}
                </button>
              )}
            </div>
          </>
        ) : (
          <>
            {/* No project: the test task's one step, not a button that can't be pressed. */}
            <p className="cg-note">{t("project.none", { agent: title, machine: m.machineName })}</p>
            {addButton}
          </>
        )}
      </div>
    </section>
  );
}

const builtIn = codingAgentIds;

/** Other names an agent goes by, as its owner may know it (WorkBuddy is Tencent's app around CodeBuddy Code). */
const alsoKnownAs: Record<string, string> = { codebuddy: "WorkBuddy" };

/**
 * Every coding agent Aide knows besides the lines above, each with its own row and how it stands on m now (工单 #136
 * P09: WorkBuddy had no entry here, only a mention): detected (and its version), found but not signed in, broken (why),
 * or not there; with one check again for them all.
 */
function KnownAgents({ m }: { m: Machine }) {
  const { t } = useTranslation("connect");
  const tf = useTranslation("firstrun").t;
  const [checking, setChecking] = useState(false);
  const check = async () => {
    setChecking(true);
    await recheck(m).catch(() => {});
    setChecking(false);
  };
  const ids = builtIn.filter((x) => !(lines as readonly string[]).includes(x));
  return (
    <section className="cg-sec" aria-label={t("known.title")}>
      <div className="fhead">{t("known.title")}</div>
      <p className="cg-note">{t("known.note", { machine: m.machineName })}</p>
      <div className="fgroup">
        {ids.map((id) => {
          const a = m.status?.agents.find((x) => x.name === id);
          const d = detect(a, m.online || m.local, !!m.status?.agents.length);
          const name = alsoKnownAs[id] ? `${codingAgentTitle(id)}（${alsoKnownAs[id]}）` : codingAgentTitle(id);
          const v = { machine: m.machineName, agent: name, version: a?.version ?? "", app: a?.detail.replace(/\.app$/, "") ?? "" };
          const state = d === "ready" && a?.version ? t("state.readyVersion", v) : t(`state.${d}`, v);
          const hint = d === "missing" ? t("known.installHint", v) : d === "broken" && a ? whyBroken(a, tf) : d === "signIn" ? t("known.signInHint", v) : "";
          return (
            <div key={id} className="frow cg-known" role="group" aria-label={name} data-state={d}>
              <AgentTile agent={id} size={20} />
              <span className="ft">
                <b>{name}</b>
                <span className="fdesc">{state}</span>
                {hint && <span className="fdesc">{hint}</span>}
              </span>
            </div>
          );
        })}
      </div>
      <div className="cg-acts">
        <button className="btn sm" disabled={checking} onClick={check}>{checking ? t("checking") : t("checkAgain")}</button>
      </div>
    </section>
  );
}

/** The agents the owner added on a machine: each one's status and test, and the form to add or change one. */
function Customs({ m, place }: { m: Machine; place: Place }) {
  const { t } = useTranslation("connect");
  const st = useStore();
  const [agents, setAgents] = useState<CustomAgent[]>();
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<CustomAgent | "new" | null>(null);
  const [picked, setPicked] = useState<string>();
  const [removing, setRemoving] = useState<string>();
  const load = useCallback(() => {
    local.listMachineCustomAgents({ machineId: m.machineId }).then(
      (r) => (setAgents(r.agents), setError("")),
      (e) => setError(needsUpdate(e) ? t("custom.update", { machine: m.machineName }) : errorText(e)),
    );
  }, [m.machineId]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(load, [load]);
  const shown = agents?.find((a) => a.name === picked) ?? agents?.[0];
  const remove = async (name: string) => {
    const ok = await st.run(() => local.removeMachineCustomAgent({ machineId: m.machineId, name }), t("custom.removed", { name }));
    setRemoving(undefined);
    if (ok) load();
  };
  return (
    <>
      <section className="cg-sec" aria-label={t("custom.saved", { machine: m.machineName })}>
        <div className="fhead">{t("custom.saved", { machine: m.machineName })}</div>
        {error && <p className="sheet-err" role="alert">{error}</p>}
        {agents && agents.length === 0 && !editing && <p className="cg-note">{t("custom.none")}</p>}
        {agents && agents.length > 0 && (
          <div className="fgroup">
            {agents.map((a) => (
              <div key={a.name} className={`frow cg-agent ${a.name === shown?.name ? "on" : ""}`} role="group" aria-label={a.name}>
                <button className="cg-pick" onClick={() => setPicked(a.name)}>
                  <AgentTile agent={a.name} size={24} />
                  <span className="ft">
                    <b>
                      {a.name}
                      <span className="cg-badge">
                        {a.source === "registry"
                          ? t("custom.sourceRegistry", { version: a.registryVersion ? `v${a.registryVersion}` : "" }).trim()
                          : t("custom.sourceLocal")}
                      </span>
                    </b>
                    <span className="fdesc mono">{[a.command, ...a.args].join(" ")}</span>
                  </span>
                </button>
                {removing === a.name ? (
                  <>
                    <button className="btn sm" onClick={() => setRemoving(undefined)}>{t("custom.cancel")}</button>
                    <button className="btn sm danger" onClick={() => remove(a.name)}>{t("custom.removeConfirm")}</button>
                  </>
                ) : (
                  <>
                    <button className="btn sm" onClick={() => setEditing(a)}>{t("custom.edit")}</button>
                    <button className="btn sm" title={t("custom.removeMessage", { machine: m.machineName })} onClick={() => setRemoving(a.name)}>
                      {t("custom.remove")}
                    </button>
                  </>
                )}
              </div>
            ))}
          </div>
        )}
        {removing && <p className="cg-note">{t("custom.removeMessage", { machine: m.machineName })}</p>}
        {!editing && !error && (
          <div className="cg-acts">
            <button className="btn" onClick={() => setEditing("new")}>
              <Icon name="plus" />
              {t("custom.add")}
            </button>
          </div>
        )}
      </section>
      {editing && (
        <CustomFormView
          m={m}
          agent={editing === "new" ? undefined : editing}
          close={(saved) => {
            setEditing(null);
            if (saved) {
              setPicked(saved);
              load();
            }
          }}
        />
      )}
      {!editing && <ACPRegistrySection m={m} onInstalled={(name) => { setPicked(name); load(); }} />}
      {!editing && shown && <Agent key={shown.name} m={m} agent={shown.name} line="custom" place={place} />}
    </>
  );
}

/** Official ACP Registry browser + one-click verified install (工单 #110). */
function ACPRegistrySection({ m, onInstalled }: { m: Machine; onInstalled: (name: string) => void }) {
  const { t } = useTranslation("connect");
  const st = useStore();
  const [data, setData] = useState<ListACPRegistryResponse>();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");
  const [installing, setInstalling] = useState<string>();

  const fetchList = useCallback(
    (refresh: boolean) => {
      setLoading(true);
      local
        .listMachineACPRegistry({ machineId: m.machineId, refresh })
        .then(
          (res) => {
            setData(res);
            setError("");
          },
          (e) => setError(needsUpdate(e) ? t("custom.update", { machine: m.machineName }) : errorText(e)),
        )
        .finally(() => setLoading(false));
    },
    [m.machineId, m.machineName, t],
  );

  useEffect(() => {
    fetchList(false);
  }, [fetchList]);

  const install = async (entry: ACPRegistryEntry) => {
    setInstalling(entry.id);
    setError("");
    try {
      const res = await local.installMachineACPRegistryAgent({ machineId: m.machineId, registryId: entry.id });
      const name = res.agent?.name || entry.id;
      st.show(t("custom.registryInstalledToast", { name, machine: m.machineName }));
      onInstalled(name);
      fetchList(false);
    } catch (e) {
      setError(needsUpdate(e) ? t("custom.update", { machine: m.machineName }) : errorText(e));
    } finally {
      setInstalling(undefined);
    }
  };

  const q = query.trim().toLowerCase();
  const entries = (data?.entries ?? []).filter(
    (e) => !q || e.id.toLowerCase().includes(q) || e.name.toLowerCase().includes(q) || e.description.toLowerCase().includes(q),
  );

  return (
    <section className="cg-sec" aria-label={t("custom.registryTitle")}>
      <div className="fhead">{t("custom.registryTitle")}</div>
      <div className="cg-box">
        <p className="cg-note">{t("custom.registryNote")}</p>
        <div className="cg-reg-bar">
          <input
            className="field"
            type="search"
            value={query}
            placeholder={t("custom.registrySearch")}
            onChange={(e) => setQuery(e.target.value)}
          />
          <button className="btn sm" disabled={loading} onClick={() => fetchList(true)}>
            {loading ? t("custom.registryRefreshing") : t("custom.registryRefresh")}
          </button>
        </div>
        {error && (
          <p className="sheet-err" role="alert">
            {error}
          </p>
        )}
        {data?.lastError && (
          <p className="cg-note warn">
            {t("custom.registryFallback", { source: data.source || "embedded", reason: data.lastError })}
          </p>
        )}
        {entries.length > 0 && (
          <div className="fgroup cg-reg-list">
            {entries.map((e) => (
              <div key={e.id} className="frow cg-reg-row">
                <div className="ft">
                  <b>
                    {e.name}
                    {e.version && <span className="cg-badge">v{e.version}</span>}
                    {e.distribution && (
                      <span className="cg-badge">
                        {e.distribution}
                        {e.sha256Verified ? " · SHA-256" : ""}
                      </span>
                    )}
                  </b>
                  <span className="fdesc">{e.description || e.id}</span>
                  {!e.supported && e.unsupportedReason && <span className="cg-note warn">{e.unsupportedReason}</span>}
                </div>
                {e.installed ? (
                  <span className="cg-badge ok">{t("custom.registryInstalled", { name: e.installedAs || e.id })}</span>
                ) : (
                  <button
                    className="btn sm"
                    disabled={!e.supported || installing !== undefined}
                    onClick={() => void install(e)}
                  >
                    {installing === e.id ? t("custom.registryInstalling") : t("custom.registryInstall")}
                  </button>
                )}
              </div>
            ))}
          </div>
        )}
        {!loading && data && entries.length === 0 && <p className="cg-note">{t("custom.registryEmpty")}</p>}
      </div>
    </section>
  );
}

/** One field of the form: its label, the control, what goes there, an example. */
function Field({ id, label, help, example, children }: { id: string; label: string; help: string; example: string; children: ReactNode }) {
  const { t } = useTranslation("connect");
  return (
    <div className="cg-field">
      <label htmlFor={id}>{label}</label>
      {children}
      <p className="cg-note">
        {help} <span className="cg-ex">{t("custom.example", { value: example })}</span>
      </p>
    </div>
  );
}

/**
 * Adding or changing a custom agent: its name (fixed once saved), command,
 * arguments, variables (secret ones kept on the machine, shown as saved) and
 * working folder; Test Connection starts it once there as given; Save keeps
 * it, in use at once.
 */
function CustomFormView({ m, agent, close }: { m: Machine; agent?: CustomAgent; close: (saved?: string) => void }) {
  const { t } = useTranslation("connect");
  const st = useStore();
  const [f, setF] = useState<CustomForm>(() => formOf(agent));
  const [test, setTest] = useState<{ busy?: boolean; ok?: boolean; text?: string }>({});
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const problem = formProblem(f, builtIn);
  const set = (p: Partial<CustomForm>) => (setF({ ...f, ...p }), setTest({}));
  const setEnv = (i: number, p: Partial<CustomForm["env"][number]>) => set({ env: f.env.map((e, j) => (j === i ? { ...e, ...p } : e)) });
  const def = agentOf(f);
  const runTest = async () => {
    setTest({ busy: true });
    try {
      const r = await local.testMachineCustomAgent({ machineId: m.machineId, agent: def });
      const protoSuffix = r.protocolVersion ? ` · ACP v${r.protocolVersion}` : "";
      const baseOk = r.agentTitle
        ? t("custom.testOkAs", { title: r.agentTitle, version: r.agentVersion })
        : t("custom.testOk", { agent: def.name });
      setTest({
        ok: r.ok,
        text: r.ok
          ? `${baseOk}${protoSuffix}`
          : [t("custom.testFailed", { reason: r.reason }), r.authMethods.length ? t("custom.testAuth", { methods: r.authMethods.join(t("listSeparator")) }) : ""].filter(Boolean).join(" "),
      });
    } catch (e) {
      setTest({ ok: false, text: t("custom.testFailed", { reason: needsUpdate(e) ? t("custom.update", { machine: m.machineName }) : errorText(e) }) });
    }
  };
  const save = async () => {
    setSaving(true);
    setError("");
    try {
      await local.saveMachineCustomAgent({ machineId: m.machineId, agent: def });
      st.show(t("custom.savedToast", { name: def.name, machine: m.machineName }));
      close(def.name);
    } catch (e) {
      setError(needsUpdate(e) ? t("custom.update", { machine: m.machineName }) : errorText(e));
      setSaving(false);
    }
  };
  const pickDir = async () => {
    const p = await window.aide?.pickDirectory();
    if (p) set({ dir: p });
  };
  return (
    <section className="cg-sec cg-form" aria-label={agent ? t("custom.editTitle", { name: agent.name }) : t("custom.newTitle")}>
      <div className="fhead">{agent ? t("custom.editTitle", { name: agent.name }) : t("custom.newTitle")}</div>
      <div className="cg-box">
        <Field id="cg-name" label={t("custom.name")} help={t("custom.nameHelp")} example={t("custom.exampleName")}>
          <input id="cg-name" className="field mono" value={f.name} disabled={!!agent} placeholder={t("custom.exampleName")} onChange={(e) => set({ name: e.target.value })} />
        </Field>
        <Field id="cg-command" label={t("custom.command")} help={t("custom.commandHelp")} example={t("custom.exampleCommand")}>
          <input id="cg-command" className="field mono" value={f.command} placeholder={t("custom.exampleCommand")} onChange={(e) => set({ command: e.target.value })} />
        </Field>
        <Field id="cg-args" label={t("custom.args")} help={t("custom.argsHelp")} example={t("custom.exampleArgs")}>
          <input id="cg-args" className="field mono" value={f.args} placeholder={t("custom.exampleArgs")} onChange={(e) => set({ args: e.target.value })} />
        </Field>
        <Field id="cg-env-0" label={t("custom.env")} help={t("custom.envHelp")} example={t("custom.exampleEnv")}>
          {f.env.map((e, i) => (
            <div className="cg-env" key={i}>
              <input
                id={`cg-env-${i}`}
                className="field mono"
                aria-label={t("custom.envName")}
                placeholder={t("custom.envName")}
                value={e.name}
                disabled={e.saved}
                onChange={(x) => setEnv(i, { name: x.target.value })}
              />
              <input
                className="field mono"
                aria-label={t("custom.envValue")}
                type={e.secret || looksSecret(e.name) ? "password" : "text"}
                placeholder={e.saved ? t("custom.envSaved") : t("custom.envValue")}
                value={e.value}
                onChange={(x) => setEnv(i, { value: x.target.value })}
              />
              <span className="cg-secret">
                <Check on={e.secret || looksSecret(e.name)} disabled={e.saved} label={t("custom.envSecret")} set={(on) => setEnv(i, { secret: on })} />
                {t("custom.envSecret")}
              </span>
              <button className="tb" aria-label={t("custom.envRemove")} onClick={() => set({ env: f.env.filter((_, j) => j !== i) })}>
                <Icon name="x" />
              </button>
            </div>
          ))}
          <div>
            <button className="btn sm" onClick={() => set({ env: [...f.env, { name: "", value: "", secret: false }] })}>
              <Icon name="plus" />
              {t("custom.envAdd")}
            </button>
          </div>
        </Field>
        <Field id="cg-dir" label={t("custom.dir")} help={t("custom.dirHelp")} example={t("custom.exampleDir")}>
          <div className="cg-env">
            <input id="cg-dir" className="field mono" value={f.dir} onChange={(e) => set({ dir: e.target.value })} />
            {m.local && (
              <button className="btn sm" onClick={pickDir}>
                {t("custom.choose")}
              </button>
            )}
          </div>
        </Field>
        {def.command && <p className="cg-note mono sel-text">{t("custom.runs", { command: [def.command, ...def.args].join(" ") })}</p>}
        {problem && (f.name || f.command) && <p className="cg-note warn">{t(`custom.problem.${problem}` as never)}</p>}
        {test.text && (
          <p className={`cg-result ${test.ok ? "ok" : "bad"}`} role="status">
            {test.text}
          </p>
        )}
        {error && <p className="sheet-err" role="alert">{error}</p>}
        <div className="cg-acts">
          <button className="btn" onClick={() => close()}>{t("custom.cancel")}</button>
          <button className="btn" onClick={runTest} disabled={!!problem || test.busy}>
            {test.busy ? t("custom.testing") : t("custom.testConnection")}
          </button>
          <button className="btn primary" onClick={save} disabled={!!problem || saving}>
            {saving ? t("custom.saving") : t("custom.save")}
          </button>
        </div>
      </div>
    </section>
  );
}
