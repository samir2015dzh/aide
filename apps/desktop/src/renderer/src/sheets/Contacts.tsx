import { useCallback, useEffect, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { codingAgentTitle, displayTitle, projectTitle } from "@aide/core";
import { AgentTile } from "@aide/ui";
import type { Persona } from "@aide/proto/identity";
import { projectsFor } from "../AgentList";
import { local } from "../api";
import { dispatchTargets, type Target } from "@aide/core";
import { Icon } from "../icons";
import { errorText, useStore } from "../store";
import { ErrorLine, useAction } from "./common";

/** The built-in avatars a contact can have (docs/personas.md §2), the same on the phone. */
export const personaIcons: Record<string, string> = { robot: "🤖", owl: "🦉", fox: "🦊", cat: "🐱", rocket: "🚀", wrench: "🔧", book: "📚", bug: "🐞" };
export const personaColors = ["#3b82f6", "#10b981", "#f59e0b", "#ef4444", "#8b5cf6", "#64748b"];

export function PersonaAvatar({ p }: { p: Pick<Persona, "icon" | "color" | "name"> }) {
  return (
    <span className="av lg" aria-hidden style={{ background: p.color || personaColors[5] }}>
      {personaIcons[p.icon] ?? [...p.name][0]?.toUpperCase()}
    </span>
  );
}

type Draft = Pick<Persona, "personaId" | "name" | "icon" | "color" | "machineId" | "projectId" | "agent" | "model" | "effort" | "mode">;
const blank: Draft = { personaId: "", name: "", icon: "robot", color: personaColors[0], machineId: "", projectId: "", agent: "", model: "", effort: "", mode: "" };

/**
 * Hand Work To (工单 #41, docs/personas.md): every coding agent the machines detected, grouped by machine, ready
 * to be written to with no contact made first (pick, choose its project, write: a task starts there). A contact
 * is only a custom face for one of them (name, avatar, its own presets), shown on the same line; writing to it
 * goes on in the session it talks in. Contacts are kept on the owner's server; when it keeps none yet (an older
 * relay), the agents are listed all the same and the note says so in words.
 */
/** Whom work was handed to last, while the app runs. */
let lastChosen = "";

export function ContactsSheet() {
  const { t } = useTranslation("sheets");
  const st = useStore();
  const [list, setList] = useState<Persona[]>();
  const [listError, setListError] = useState("");
  // Opened again, it is on whom work went last.
  const [chosen, setChosenState] = useState(lastChosen);
  const setChosen = (k: string) => setChosenState((lastChosen = k));
  const [project, setProject] = useState("");
  const [editing, setEditing] = useState<Draft>();
  const [text, setText] = useState("");
  const [key, setKey] = useState(() => crypto.randomUUID());
  const [opened, setOpened] = useState<string>();
  const act = useAction();

  // Contacts are extras: failing to read them never empties the list of agents.
  const load = useCallback(
    () =>
      local.listPersonas({}).then(
        (r) => (setList(r.personas), setListError("")),
        (e) => (setList([]), setListError(errorText(e))),
      ),
    [],
  );
  useEffect(() => void load(), [load]);
  const groups = dispatchTargets(st.machines, list ?? []);
  const all = groups.flatMap((g) => g.targets);
  const target = all.find((x) => x.key === chosen || (chosen.startsWith("p:") && x.persona?.personaId === chosen.slice(2))) ?? all.find((x) => x.ready);
  const p = target?.persona;
  const current = p?.sessionId ? st.sessions.find((s) => s.sessionId === p.sessionId) : undefined;
  const projects = target ? projectsFor(st.projects.filter((x) => x.machineId === target.machineId), target.agent) : [];
  const proj = projects.find((x) => x.projectId === project) ?? projects.find((x) => x.projectId === p?.projectId) ?? projects[0];

  // The session opens once the list has it, as after New Task.
  const { set } = st;
  const has = opened !== undefined && st.sessions.some((s) => s.sessionId === opened);
  useEffect(() => {
    if (!opened) return;
    const open = () => set({ sheet: null, selection: { kind: "session", id: opened } });
    if (has) return open();
    const late = setTimeout(open, 10_000);
    return () => clearTimeout(late);
  }, [opened, has, set]);

  const canSend = !!target?.ready && !!text.trim() && !act.busy && !opened && (!!p || !!proj);
  const send = async (e: FormEvent) => {
    e.preventDefault();
    if (!target || !canSend) return;
    setChosen(target.persona ? `p:${target.persona.personaId}` : target.key);
    await act.go(async () => {
      if (p && (!proj || proj.projectId === p.projectId)) {
        const res = await local.talkToPersona({ personaId: p.personaId, text, idempotencyKey: key });
        st.show(t(res.started ? "contacts.started" : "contacts.sent", { name: p.name, title: displayTitle(res.session!) }));
        setOpened(res.session!.sessionId);
      } else {
        const res = await local.startSession({ machineId: target.machineId, session: { idempotencyKey: key, agent: target.agent, projectId: proj!.projectId, prompt: text } });
        st.show(t("contacts.startedAgent", { agent: codingAgentTitle(target.agent), title: displayTitle(res.session!) }));
        setOpened(res.session!.sessionId);
      }
      setKey(crypto.randomUUID());
    });
  };

  if (editing) return <ContactForm draft={editing} done={(saved) => (setEditing(undefined), saved !== undefined && (setChosen(saved ? `p:${saved}` : ""), load()))} />;
  const why = (x: Target) => (x.why ? t(`contacts.why.${x.why}`) : "");
  return (
    <form onSubmit={send}>
      <h3 id="sheet-title">{t("contacts.title")}</h3>
      <p className="sub">{t("contacts.sub")}</p>
      {listError && <p className="sheet-note muted" role="status">{listError}</p>}
      {groups.length === 0 && <p className="sheet-note muted">{t("contacts.none")}</p>}
      <div className="contacts" role="listbox" aria-label={t("contacts.title")}>
        {groups.map((g) => (
          <div key={g.machine.machineId} className="contact-group" role="group" aria-label={g.machine.machineName}>
            <div className="contact-gh">
              <Icon name={g.machine.local ? "laptop" : "server"} />
              <b>{g.machine.machineName}</b>
              <span className="muted">{g.machine.online || g.machine.local ? t("contacts.online") : t("contacts.offline")}</span>
            </div>
            {g.targets.map((x) => (
              <div
                key={x.key}
                role="option"
                aria-selected={x.key === target?.key}
                aria-disabled={!x.ready}
                className={`contact ${x.key === target?.key ? "on" : ""} ${x.ready ? "" : "off"}`}
                onClick={() => (setChosen(x.key), setProject(""))}
              >
                {x.persona ? <PersonaAvatar p={x.persona} /> : <AgentTile agent={x.agent} size={30} />}
                <span className="contact-t">
                  <b>{x.persona ? x.persona.name : codingAgentTitle(x.agent)}</b>
                  <span className="muted">
                    {x.persona ? `${codingAgentTitle(x.agent)} · ` : ""}
                    {/* Green only when it is ready now; not confirmed, it says so, in a quiet colour (工单 #56). */}
                    <span className={x.state?.usable === "yes" ? "ok" : x.ready ? "" : "warn"}>{x.state?.usable === "yes" ? t("contacts.ready") : x.ready ? t("contacts.unverified") : why(x)}</span>
                  </span>
                  {/* What the machine page says of it, in its words. */}
                  {x.state && <span className="muted contact-line">{x.state.line}</span>}
                </span>
                {x.persona ? (
                  <button type="button" className="link" onClick={() => setEditing({ ...x.persona! })}>{t("contacts.edit")}</button>
                ) : (
                  !listError && (
                    <button type="button" className="link" onClick={() => setEditing({ ...blank, machineId: x.machineId, agent: x.agent, name: codingAgentTitle(x.agent) })}>{t("contacts.customize")}</button>
                  )
                )}
              </div>
            ))}
          </div>
        ))}
      </div>
      {target?.ready && (
        <>
          {projects.length > 1 && (
            <div className="fgroup">
              <div className="frow">
                <label className="fl" htmlFor="ct-project">{t("newSession.project")}</label>
                <span className="fv">
                  <select id="ct-project" className="field" value={proj?.projectId ?? ""} onChange={(e) => setProject(e.target.value)}>
                    {projects.map((x) => (
                      <option key={x.projectId} value={x.projectId}>{projectTitle(x)}</option>
                    ))}
                  </select>
                </span>
              </div>
            </div>
          )}
          {projects.length === 0 && !p && <p className="sheet-note muted">{t("contacts.noProject", { agent: codingAgentTitle(target.agent) })}</p>}
          <p className="sheet-note muted">
            {current ? (
              <>
                {t("contacts.current")}{" "}
                <button type="button" className="link" onClick={() => set({ sheet: null, selection: { kind: "session", id: current.sessionId } })}>{displayTitle(current)}</button>
              </>
            ) : proj ? (
              t("contacts.freshIn", { project: projectTitle(proj) })
            ) : (
              t("contacts.fresh")
            )}
          </p>
          <label className="wk-lab" htmlFor="ct-text">{t("contacts.message", { name: p?.name ?? codingAgentTitle(target.agent) })}</label>
          <textarea
            id="ct-text"
            className="field ns-prompt"
            rows={4}
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && (e.metaKey || e.ctrlKey) && !e.nativeEvent.isComposing && send(e)}
          />
        </>
      )}
      <ErrorLine text={act.error} />
      <div className="sheet-foot">
        {!listError && <button type="button" className="btn" onClick={() => setEditing({ ...blank })}>{t("contacts.add")}</button>}
        <button type="button" className="btn" onClick={() => st.set({ sheet: null })}>{t("cancel")}</button>
        <button type="submit" className="btn primary" disabled={!canSend}>{t("contacts.send")}</button>
      </div>
    </form>
  );
}

/** Adding or changing a contact. done gets the id saved, or undefined. */
function ContactForm({ draft, done }: { draft: Draft; done: (saved?: string) => void }) {
  const { t } = useTranslation("sheets");
  const tf = useTranslation("firstrun").t;
  const st = useStore();
  const [d, setD] = useState(draft);
  const put = (patch: Partial<Draft>) => setD((x) => ({ ...x, ...patch }));
  const act = useAction();
  const machine = st.machines.find((m) => m.machineId === d.machineId) ?? st.machines.find((m) => m.local) ?? st.machines[0];
  const mid = machine?.machineId ?? "";
  const agents = (machine?.status?.agents ?? []).filter((a) => a.available || a.name === d.agent);
  const a = agents.find((x) => x.name === d.agent) ?? agents[0];
  const projects = projectsFor(st.projects.filter((x) => x.machineId === mid), a?.name ?? "");
  const project = (projects.find((x) => x.projectId === d.projectId) ?? projects[0])?.projectId ?? "";
  const efforts = a ? (a.models.find((x) => x.id === (d.model || a.defaults?.model || a.own?.model))?.efforts ?? [...new Set(a.models.flatMap((x) => x.efforts))]) : [];
  const ready = d.name.trim() !== "" && !!mid && !!a && project !== "";

  const save = async (e: FormEvent) => {
    e.preventDefault();
    if (!ready || act.busy) return;
    await act.go(async () => {
      const res = await local.savePersona({ persona: { ...d, machineId: mid, agent: a!.name, projectId: project } });
      done(res.persona!.personaId);
    });
  };
  const remove = () => act.go(async () => (await local.deletePersona({ personaId: d.personaId }), done("")));

  return (
    <form onSubmit={save}>
      <h3 id="sheet-title">{d.personaId ? t("contacts.editTitle") : t("contacts.addTitle")}</h3>
      <div className="fgroup">
        <div className="frow">
          <label className="fl" htmlFor="ct-name">{t("contacts.name")}</label>
          <span className="fv">
            <input id="ct-name" className="field" style={{ width: 260 }} maxLength={64} value={d.name} onChange={(e) => put({ name: e.target.value })} />
          </span>
        </div>
        <div className="frow">
          <span className="fl">{t("contacts.avatar")}</span>
          <span className="fv">
            <div className="seg" role="group" aria-label={t("contacts.avatar")}>
              {Object.entries(personaIcons).map(([id, glyph]) => (
                <button type="button" key={id} aria-label={id} className={d.icon === id ? "on" : ""} aria-pressed={d.icon === id} onClick={() => put({ icon: id })}>{glyph}</button>
              ))}
            </div>
            <div className="seg" role="group" aria-label={t("contacts.color")}>
              {personaColors.map((c) => (
                <button type="button" key={c} aria-label={c} className={d.color === c ? "on" : ""} aria-pressed={d.color === c} onClick={() => put({ color: c })}>
                  <span className="av" style={{ background: c, width: 12, height: 12 }} />
                </button>
              ))}
            </div>
          </span>
        </div>
        <div className="frow">
          <span className="fl">{t("newSession.machine")}</span>
          <span className="fv">
            <div className="seg" role="group" aria-label={t("newSession.machine")}>
              {st.machines.map((m) => (
                <button type="button" key={m.machineId} className={m.machineId === mid ? "on" : ""} aria-pressed={m.machineId === mid} onClick={() => put({ machineId: m.machineId, agent: "", projectId: "", model: "", effort: "", mode: "" })}>
                  {m.machineName}
                </button>
              ))}
            </div>
          </span>
        </div>
        {agents.length > 0 && (
          <div className="frow">
            <span className="fl">{t("newSession.agent")}</span>
            <span className="fv">
              <div className="seg" role="group" aria-label={t("newSession.agent")}>
                {agents.map((x) => (
                  <button type="button" key={x.name} className={x.name === a?.name ? "on" : ""} aria-pressed={x.name === a?.name} onClick={() => put({ agent: x.name, projectId: "", model: "", effort: "", mode: "" })}>
                    {codingAgentTitle(x.name)}
                  </button>
                ))}
              </div>
            </span>
          </div>
        )}
        {a && (
          <div className="frow">
            <label className="fl" htmlFor="ct-project">{t("newSession.project")}</label>
            <span className="fv">
              {projects.length ? (
                <select id="ct-project" className="psel" style={{ width: 260 }} value={project} onChange={(e) => put({ projectId: e.target.value })}>
                  {projects.map((x) => (
                    <option key={x.projectId} value={x.projectId}>{x.projectId}</option>
                  ))}
                </select>
              ) : (
                t("newSession.noProjects", { machine: machine!.machineName, agent: codingAgentTitle(a.name) })
              )}
            </span>
          </div>
        )}
        {a && a.models.length > 0 && (
          <div className="frow">
            <label className="fl" htmlFor="ct-model">{t("newSession.model")}</label>
            <span className="fv">
              <select id="ct-model" className="psel" style={{ width: 260 }} value={d.model} onChange={(e) => put({ model: e.target.value, effort: "" })}>
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
            <label className="fl" htmlFor="ct-effort">{t("newSession.effort")}</label>
            <span className="fv">
              <select id="ct-effort" className="psel" style={{ width: 260 }} value={d.effort} onChange={(e) => put({ effort: e.target.value })}>
                <option value="">{t("newSession.default")}</option>
                {efforts.map((x) => (
                  <option key={x} value={x}>{tf(`prepare.efforts.${x}`, { defaultValue: x })}</option>
                ))}
              </select>
            </span>
          </div>
        )}
        {a && a.modes.length > 0 && (
          <div className="frow">
            <label className="fl" htmlFor="ct-mode">{t("newSession.mode")}</label>
            <span className="fv">
              <select id="ct-mode" className="psel" style={{ width: 260 }} value={d.mode} onChange={(e) => put({ mode: e.target.value })}>
                <option value="">{t("newSession.default")}</option>
                {a.modes.map((x) => (
                  <option key={x.id} value={x.id}>{x.name || x.id}</option>
                ))}
              </select>
            </span>
          </div>
        )}
      </div>
      <p className="sheet-note muted">{t("contacts.presetsNote")}</p>
      <ErrorLine text={act.error} />
      <div className="sheet-foot">
        {d.personaId && <button type="button" className="btn" onClick={remove}>{t("contacts.delete")}</button>}
        <button type="button" className="btn" onClick={() => done()}>{t("cancel")}</button>
        <button type="submit" className="btn primary" disabled={!ready || act.busy}>{t("contacts.save")}</button>
      </div>
    </form>
  );
}
