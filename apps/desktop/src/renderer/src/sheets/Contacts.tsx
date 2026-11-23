import { useState, type FormEvent } from "react";
import { Picker } from "../Picker";
import { useTranslation } from "react-i18next";
import { codingAgentTitle, modeLabel, projectTitle } from "@aide/core";
import type { Persona } from "@aide/proto/identity";
import { colorHex, personaLook, type BotLook } from "@aide/core/avatar";
import { projectsFor } from "../AgentList";
import { local } from "../api";
import { LookEditor } from "../components";
import { useStore } from "../store";
import { ErrorLine, useAction } from "./common";

export type PersonaDraft = Pick<Persona, "personaId" | "name" | "icon" | "color" | "machineId" | "projectId" | "agent" | "model" | "effort" | "mode">;
type Draft = PersonaDraft;
/** A new contact: its look the seed's until one is picked (工单 #83). */
export const blankPersona: PersonaDraft = { personaId: "", name: "", icon: "", color: "", machineId: "", projectId: "", agent: "", model: "", effort: "", mode: "" };

/** A look as a contact keeps it (docs/personas.md §2): its shape as its icon, its colour's swatch as its colour. */
export function lookFields(l: BotLook): Pick<Persona, "icon" | "color"> {
  return { icon: l.shape, color: colorHex(l.color) };
}

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
  // A contact being changed keeps its agent and project while its machine does not list them now (工单 #78): what
  // isn't listed never stops a new name from being saved.
  const editing = draft.personaId !== "";
  const agent = a?.name ?? (editing ? d.agent : "");
  const project = (projects.find((x) => x.projectId === d.projectId) ?? projects[0])?.projectId ?? (editing ? d.projectId : "");
  const efforts = a ? (a.models.find((x) => x.id === (d.model || a.defaults?.model || a.own?.model))?.efforts ?? [...new Set(a.models.flatMap((x) => x.efforts))]) : [];
  const next: Draft = { ...d, machineId: mid, agent, projectId: project };
  // Saving is for a change, any one (only its name too); a new contact is one.
  const changed = !editing || (Object.keys(draft) as (keyof Draft)[]).some((k) => draft[k] !== next[k]);
  const ready = changed && d.name.trim() !== "" && !!mid && agent !== "" && project !== "";
  // Its look (工单 #83): the seed is its bot's, the look kept is the one shown (the seed's until another is picked).
  const seed = `m:${mid}/${agent}`;
  const look = personaLook(d, seed);

  const save = async (e: FormEvent) => {
    e.preventDefault();
    if (!ready || act.busy) return;
    await act.go(async () => {
      const res = await local.savePersona({ persona: { ...next, ...lookFields(look) } });
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
            <LookEditor seed={seed} look={look} onChange={(l) => put(lookFields(l))} />
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
                <Picker className="psel" label={t("newSession.project")} value={project} onChange={(v) => put({ projectId: v })} options={projects.map((x) => ({ value: x.projectId, label: projectTitle(x) }))} />
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
              <Picker className="psel" label={t("newSession.model")} value={d.model} onChange={(v) => put({ model: v, effort: "" })} options={[{ value: "", label: t("newSession.default") }, ...a.models.map((x) => ({ value: x.id, label: x.name || x.id }))]} />
            </span>
          </div>
        )}
        {efforts.length > 0 && (
          <div className="frow">
            <label className="fl" htmlFor="ct-effort">{t("newSession.effort")}</label>
            <span className="fv">
              <Picker className="psel" label={t("newSession.effort")} value={d.effort} onChange={(v) => put({ effort: v })} options={[{ value: "", label: t("newSession.default") }, ...efforts.map((x) => ({ value: x, label: tf(`prepare.efforts.${x}`, { defaultValue: x }) }))]} />
            </span>
          </div>
        )}
        {a && a.modes.length > 0 && (
          <div className="frow">
            <label className="fl" htmlFor="ct-mode">{t("newSession.mode")}</label>
            <span className="fv">
              <Picker className="psel" label={t("newSession.mode")} value={d.mode} onChange={(v) => put({ mode: v })} options={[{ value: "", label: t("newSession.default") }, ...a.modes.map((x) => ({ value: x.id, label: modeLabel(a.name, x) }))]} />
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
