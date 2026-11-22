import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { codingAgentTitle } from "@aide/core";
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

export type PersonaDraft = Pick<Persona, "personaId" | "name" | "icon" | "color" | "machineId" | "projectId" | "agent" | "model" | "effort" | "mode">;
type Draft = PersonaDraft;
export const blankPersona: PersonaDraft = { personaId: "", name: "", icon: "robot", color: personaColors[0], machineId: "", projectId: "", agent: "", model: "", effort: "", mode: "" };

/**
 * A contact (docs/personas.md), opened from its bot's page (工单 #57): a name, an avatar and presets for one coding
 * agent on one machine, and the session it talks in. There is no Hand Work To page beside the roster any more: the
 * roster is whom to, the bot's page is where to write.
 */
export const personasChanged = "aide-personas-changed";

export function PersonaSheet({ draft }: { draft: PersonaDraft }) {
  const st = useStore();
  // The bot pages read the contacts again once one is saved or deleted.
  return <ContactForm draft={draft} done={() => (st.set({ sheet: null }), window.dispatchEvent(new Event(personasChanged)))} />;
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
