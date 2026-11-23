import { useState } from "react";
import { useTranslation } from "react-i18next";
import { projectTitle, type Bot } from "@aide/core";
import { botLook } from "@aide/core/avatar";
import type { Machine } from "@aide/proto/local";
import { AgentPage, PauseSection, projectsFor, usageText } from "../AgentList";
import { local } from "../api";
import { connDot } from "../Connections";
import { Icon } from "../icons";
import { Choices, FluentControls } from "../Picker";
import { GrantDetail } from "../settings/Grants";
import { Row } from "../settings/common";
import { blankPersona, lookFields } from "../sheets/Contacts";
import { errorText, useNow, useStore } from "../store";
import { BotFace, BotPill, PersonaFace } from "./Bots";
import "./botsettings.css";

/**
 * A bot's settings, a page of their own (工单 #97): its page keeps its state, its conversations and what it is working
 * on; every setting it has is here, each once, with the way back to its page. A coding agent's: how it stands (its
 * plan's usage with it), what it may use, its kind, its name and face, its projects and how it runs, pausing it, its
 * grant when it has one, and taking it off Aide. A personal agent's: how it stands and its grant (what it may use, its
 * kind, running commands, its name and face, disconnecting it). Its controls are drawn the Fluent way, here alone.
 */
export function BotSettings({ b, m }: { b: Bot; m?: Machine }) {
  const st = useStore();
  const { t } = useTranslation("common");
  const coding = !!m && !!b.agent && !b.key.startsWith("c:");
  const back = () => st.set({ selection: { kind: "bot", key: b.key } });
  return (
    <div className="bot-page bset-page" role="region" aria-label={t("bots.settingsOf", { name: b.name })}>
      <header className="bot-head bset-head drag">
        <button className="bset-back" onClick={back} aria-label={t("bots.settingsBack", { name: b.name })} title={t("bots.settingsBack", { name: b.name })}>
          <Icon name="chevL" />
          <span>{b.name}</span>
        </button>
        <BotFace b={b} size={32} dot still />
        <div className="bot-head-t">
          <div className="bot-head-l1">
            <h1>{t("bots.settings")}</h1>
            <BotPill b={b} />
          </div>
        </div>
      </header>
      <div className="bset">
        <FluentControls.Provider value>
          <div className="bset-col">
            <section aria-label={t("bots.connection")}>
              <div className="bset-h">{t("bots.connection")}</div>
              <div className="fgroup">
                <div className="frow bot-state-row">
                  <span className="conn-state" data-state={b.state.state}>
                    <i className={`dotc ${connDot(b.state.state)}`} />
                    {b.state.word}
                  </span>
                  <span className="muted">{b.state.line}</span>
                </div>
                {coding && <Usage b={b} m={m!} />}
              </div>
            </section>
            {coding && <AgentSettings b={b} m={m!} />}
            {b.client && (
              <section aria-label={t("bots.grant")}>
                <div className="bset-h">{t("bots.grant")}</div>
                {/* A coding agent's name and face are its contact's (above), so its grant's look isn't offered again here. */}
                <GrantDetail c={b.client} bare look={!coding} />
              </section>
            )}
            {coding && <Hide b={b} m={m!} />}
          </div>
        </FluentControls.Provider>
      </div>
    </div>
  );
}

/** Its plan's usage (Claude Code's, #86), with how it stands. */
function Usage({ b, m }: { b: Bot; m: Machine }) {
  const { t } = useTranslation("common");
  const tf = useTranslation("firstrun").t;
  const now = useNow(30_000);
  const a = m.status?.agents.find((x) => x.name === b.agent);
  const usage = a && b.agent === "claude" ? usageText(a, tf, now) : "";
  return usage ? <Row k={t("agentSettings.usage")}><span className="mono agent-usage">{usage}</span></Row> : null;
}

/**
 * A coding agent's own settings (工单 #93), laid out as a connected agent's grant page is (GrantDetail): what it may
 * use (its machine, its projects), its kind (told from what it is until the owner says otherwise, #52), its name and
 * face, its projects and how it runs, and pausing it. An agent its machine found has no grant: what a grant has and it
 * hasn't (expiry, the exec channel, a fingerprint) is not shown. One with a grant of its own (DSH) has its kind on that
 * grant's part, so not here.
 */
function AgentSettings({ b, m }: { b: Bot; m: Machine }) {
  const st = useStore();
  const { t } = useTranslation("common");
  const a = m.status?.agents.find((x) => x.name === b.agent);
  const mine = projectsFor(st.projects.filter((p) => p.machineId === m.machineId), b.agent);
  const all = mine.length > 0 && mine.every((p) => p.agents.length === 0) && st.projects.filter((p) => p.machineId === m.machineId).length === mine.length;
  const scope = () => st.set({ sheet: { kind: "agentScope", machineId: m.machineId, agent: b.agent } });
  return (
    <>
      <section aria-label={t("agentSettings.canUse")}>
        <div className="bset-h">{t("agentSettings.canUse")}</div>
        <div className="fgroup">
          <Row k={t("agentSettings.machine")}>{t("agentSettings.machineValue", { machine: m.machineName })}</Row>
          <Row k={t("agentSettings.projects")}>{all ? t("agentSettings.allProjects") : mine.length ? mine.map(projectTitle).join(t("agentSettings.separator")) : t("agentSettings.noProjects")}</Row>
          <button className="frow act" onClick={scope}>{t("agentSettings.changeScope")}<Icon name="chevR" className="sm chev" /></button>
        </div>
      </section>
      {!b.client && a && (
        <section aria-label={t("category.label")}>
          <div className="fgroup">
            <div className="frow">
              <AgentCategoryControl m={m} agent={b.agent} category={a.category} />
            </div>
          </div>
        </section>
      )}
      <Identity b={b} m={m} />
      <section aria-label={t("bots.manage")}>
        <div className="bset-h">{t("bots.manage")}</div>
        <AgentPage m={m} agent={b.agent} bare />
      </section>
      {/* Stopping it (until resumed, or until its usage resets). */}
      {a && <PauseSection a={a} m={m} />}
    </>
  );
}

/**
 * Its name and face, once (工单 #97: the settings' row and the names-and-faces block were the same thing twice): the
 * owner's contacts for it (docs/personas.md), the first its name and face in the roster, each with a pencil (工单 #65);
 * none yet is a short button, no sentence standing in for a name.
 */
function Identity({ b, m }: { b: Bot; m: Machine }) {
  const st = useStore();
  const { t } = useTranslation("common");
  return (
    <section aria-label={t("agentSettings.rename")}>
      <div className="bset-h">{t("agentSettings.rename")}</div>
      <div className="fgroup">
        {b.personas.map((p) => (
          <div key={p.personaId} className="frow bot-persona">
            <PersonaFace b={b} p={p} size={28} />
            <span className="bot-persona-t">
              <b>{p.name}</b>
              <span className="muted">{[p.projectId, p.model, p.effort, p.mode].filter(Boolean).join(" · ")}</span>
            </span>
            <button className="tb" title={t("bots.identityEditTip", { name: p.name })} aria-label={t("bots.identityEditTip", { name: p.name })} onClick={() => st.set({ sheet: { kind: "persona", draft: { ...blankPersona, ...p } } })}>
              <Icon name="edit" />
            </button>
          </div>
        ))}
        <button className="frow act" title={t("bots.identityAddTip")} onClick={() => st.set({ sheet: { kind: "persona", draft: { ...blankPersona, ...(b.personas.length ? {} : lookFields(botLook(b))), machineId: m.machineId, agent: b.agent, name: b.personas.length ? "" : b.name } } })}>
          <Icon name="plus" className="sm" />
          {t(b.personas.length ? "bots.identityAdd" : "bots.identityFirst")}
        </button>
      </div>
    </section>
  );
}

/** Taking it off Aide's lists (SetAgentHidden); deleting its entry for good goes with this too (工单 #82). */
function Hide({ b, m }: { b: Bot; m: Machine }) {
  const st = useStore();
  const { t } = useTranslation("common");
  const a = m.status?.agents.find((x) => x.name === b.agent);
  if (!a || a.hidden) return null;
  const hide = () =>
    st.set({
      sheet: {
        kind: "confirm",
        title: t("agentSettings.hideTitle", { name: b.name }),
        message: t("agentSettings.hideMessage", { machine: m.machineName }),
        button: t("agentSettings.hideButton"),
        run: () => local.setAgentHidden({ machineId: m.machineId, agent: b.agent, hidden: true }),
      },
    });
  return (
    <section aria-label={t("agentSettings.hide")}>
      <div className="fgroup">
        <button className="frow act danger" onClick={hide}>{t("agentSettings.hide")}</button>
      </div>
    </section>
  );
}

/**
 * Its kind (工单 #52, #93): told from what it is (a coding agent of its machine) until the owner files it otherwise;
 * kept with the agent on its machine, so every screen says the same; "told from what it is" again clears the say.
 */
function AgentCategoryControl({ m, agent, category }: { m: Machine; agent: string; category: string }) {
  const { t } = useTranslation("common");
  const st = useStore();
  const [error, setError] = useState("");
  const set = (v: string) => {
    setError("");
    local.setAgentCategory({ machineId: m.machineId, agent, category: v }).then(
      () => st.show(t(v === "" ? "category.cleared" : v === "personal" ? "category.setPersonal" : "category.setLocal")),
      (e) => setError(errorText(e)),
    );
  };
  const personal = category === "personal";
  return (
    <div className="conn-cat">
      <span className="bset-k">{t("category.label")}</span>
      <Choices value={category} label={t("category.label")} onChange={set}
        options={[
          { value: "", label: t("category.auto", { kind: category ? "" : ` · ${t("category.local")}` }) },
          { value: "personal", label: t("category.personal") },
          { value: "local", label: t("category.localOn", { machine: m.machineName }) },
        ]} />
      <span className="muted">{t(personal ? "agentSettings.personalMeans" : "category.localMeans")}</span>
      {error && <span className="err" role="alert">{error}</span>}
    </div>
  );
}
