import { useCallback, useEffect, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { codingAgentTitle, displayTitle } from "@aide/core";
import type { Persona } from "@aide/proto/identity";
import { projectsFor } from "../AgentList";
import { local } from "../api";
import { useStore } from "../store";
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
 * Contacts (docs/personas.md): the owner picks one and writes to it. The message goes on in the session the contact
 * talks in, or starts one with the contact's machine, project, agent, model, effort and mode; that session then
 * opens, as any other. Contacts are kept on the owner's server, the same on every device.
 */
export function ContactsSheet() {
  const { t } = useTranslation("sheets");
  const st = useStore();
  const [list, setList] = useState<Persona[]>();
  const [chosen, setChosen] = useState("");
  const [editing, setEditing] = useState<Draft>();
  const [text, setText] = useState("");
  const [key, setKey] = useState(() => crypto.randomUUID());
  const [opened, setOpened] = useState<string>();
  const act = useAction();

  const load = useCallback(() => act.go(async () => setList((await local.listPersonas({})).personas)), []); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => void load(), [load]);
  const p = list?.find((x) => x.personaId === chosen) ?? list?.[0];
  const current = p?.sessionId ? st.sessions.find((s) => s.sessionId === p.sessionId) : undefined;

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

  const send = async (e: FormEvent) => {
    e.preventDefault();
    if (!p || !text.trim() || act.busy) return;
    await act.go(async () => {
      const res = await local.talkToPersona({ personaId: p.personaId, text, idempotencyKey: key });
      st.show(t(res.started ? "contacts.started" : "contacts.sent", { name: p.name, title: displayTitle(res.session!) }));
      setKey(crypto.randomUUID());
      setOpened(res.session!.sessionId);
    });
  };

  if (editing) return <ContactForm draft={editing} done={(saved) => (setEditing(undefined), saved !== undefined && (setChosen(saved), load()))} />;
  return (
    <form onSubmit={send}>
      <h3 id="sheet-title">{t("contacts.title")}</h3>
      <p className="sub">{t("contacts.sub")}</p>
      {list && list.length === 0 && <p className="sheet-note muted">{t("contacts.none")}</p>}
      <div className="contacts" role="listbox" aria-label={t("contacts.title")}>
        {list?.map((x) => (
          <div key={x.personaId} role="option" aria-selected={x.personaId === p?.personaId} className={`contact ${x.personaId === p?.personaId ? "on" : ""}`} onClick={() => setChosen(x.personaId)}>
            <PersonaAvatar p={x} />
            <span className="contact-t">
              <b>{x.name}</b>
              <span className="muted">
                {codingAgentTitle(x.agent)} · {x.projectId} · {st.machines.find((m) => m.machineId === x.machineId)?.machineName ?? x.machineId}
              </span>
            </span>
            <button type="button" className="link" onClick={() => setEditing({ ...x })}>{t("contacts.edit")}</button>
          </div>
        ))}
      </div>
      {p && (
        <>
          <p className="sheet-note muted">
            {current ? (
              <>
                {t("contacts.current")}{" "}
                <button type="button" className="link" onClick={() => set({ sheet: null, selection: { kind: "session", id: current.sessionId } })}>{displayTitle(current)}</button>
              </>
            ) : (
              t("contacts.fresh")
            )}
          </p>
          <label className="wk-lab" htmlFor="ct-text">{t("contacts.message", { name: p.name })}</label>
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
        <button type="button" className="btn" onClick={() => setEditing({ ...blank })}>{t("contacts.add")}</button>
        <button type="button" className="btn" onClick={() => st.set({ sheet: null })}>{t("cancel")}</button>
        <button type="submit" className="btn primary" disabled={!p || !text.trim() || act.busy || !!opened}>{t("contacts.send")}</button>
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
