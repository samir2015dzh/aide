import { useState } from "react";
import { useTranslation } from "react-i18next";
import { codingAgentTitle, projectTitle, type Bot } from "@aide/core";
import type { Machine } from "@aide/proto/local";
import { PauseSection, projectsFor, usageText } from "../AgentList";
import { local } from "../api";
import { Icon } from "../icons";
import { Picker } from "../Picker";
import { Row } from "../settings/common";
import { blankPersona } from "../sheets/Contacts";
import { errorText, useNow, useStore } from "../store";
import { BotFace } from "./Bots";

/**
 * A coding agent's own settings on its page (工单 #93), laid out as a connected agent's grant page is (GrantDetail):
 * who it is, what it may use (its machine, its projects), its kind (told from what it is until the owner says otherwise,
 * #52), its name and face, its scope, and stopping or removing it. An agent its machine found has no grant: what a
 * grant has and it hasn't (expiry, the exec channel, a fingerprint) is not shown. One with a grant of its own (DSH) has
 * its kind on that grant's page, so not here.
 */
export function AgentSettings({ b, m }: { b: Bot; m: Machine }) {
  const st = useStore();
  const { t } = useTranslation("common");
  const tf = useTranslation("firstrun").t;
  const now = useNow(30_000);
  const a = m.status?.agents.find((x) => x.name === b.agent);
  const mine = projectsFor(st.projects.filter((p) => p.machineId === m.machineId), b.agent);
  const all = mine.length > 0 && mine.every((p) => p.agents.length === 0) && st.projects.filter((p) => p.machineId === m.machineId).length === mine.length;
  const persona = b.personas[0];
  const rename = () => st.set({ sheet: { kind: "persona", draft: persona ? { ...blankPersona, ...persona } : { ...blankPersona, machineId: m.machineId, agent: b.agent, name: b.name } } });
  const scope = () => st.set({ sheet: { kind: "agentScope", machineId: m.machineId, agent: b.agent } });
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
  const usage = a && b.agent === "claude" ? usageText(a, tf, now) : "";
  return (
    <div className="form agent-settings">
      <div className="dhero">
        <button className="dhero-av" aria-label={t("agentSettings.rename")} onClick={rename}>
          <BotFace b={b} size={52} still />
        </button>
        <div>
          <b>{b.name}</b>
          <span>{t("agentSettings.hero", { app: codingAgentTitle(b.agent), machine: m.machineName })}</span>
          {usage && <span className="mono agent-usage">{usage}</span>}
        </div>
      </div>
      <div>
        <div className="fhead">{t("agentSettings.canUse")}</div>
        <div className="fgroup">
          <Row k={t("agentSettings.machine")}>{t("agentSettings.machineValue", { machine: m.machineName })}</Row>
          <Row k={t("agentSettings.projects")}>{all ? t("agentSettings.allProjects") : mine.length ? mine.map(projectTitle).join(t("agentSettings.separator")) : t("agentSettings.noProjects")}</Row>
        </div>
      </div>
      {!b.client && a && (
        <div className="fgroup">
          <div className="frow">
            <AgentCategoryControl m={m} agent={b.agent} category={a.category} />
          </div>
        </div>
      )}
      <div className="fgroup">
        <button className="frow act" onClick={rename}>{t("agentSettings.rename")}<Icon name="chevR" className="sm chev" /></button>
        <button className="frow act" onClick={scope}>{t("agentSettings.changeScope")}<Icon name="chevR" className="sm chev" /></button>
      </div>
      {/* Stopping it (until resumed, or until its usage resets), and taking it off Aide's lists; deleting its entry for
          good goes with these too (工单 #82). */}
      {a && <PauseSection a={a} m={m} />}
      {a && !a.hidden && (
        <div className="fgroup">
          <button className="frow act danger" onClick={hide}>{t("agentSettings.hide")}</button>
        </div>
      )}
    </div>
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
      <label>
        <span className="muted">{t("category.label")}</span>
        <Picker size="sm" value={category} label={t("category.label")} onChange={set}
          options={[
            { value: "", label: t("category.auto", { kind: category ? "" : ` · ${t("category.local")}` }) },
            { value: "personal", label: t("category.personal") },
            { value: "local", label: t("category.localOn", { machine: m.machineName }) },
          ]} />
      </label>
      <span className="muted">{t(personal ? "agentSettings.personalMeans" : "category.localMeans")}</span>
      {error && <span className="err" role="alert">{error}</span>}
    </div>
  );
}
