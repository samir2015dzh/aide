import { useTranslation } from "react-i18next";
import { useState } from "react";
import type { Client, Machine } from "@aide/proto/local";
import { agentName, AgentLogo } from "@aide/ui";
import { agoText, clientCategory, grantsByCategory, machineConnections, type ConnRow, type ConnState } from "@aide/core";
import { Install } from "./dsh/DSHSessions";
import { local } from "./api";
import { GrantAvatar } from "./components";
import { errorText, useNow, useStore } from "./store";
import "./connections.css";

// This machine's connections (工单 #49): what its coding agents are
// connected to, in plain words, row by row, from @aide/core's one account
// of it (machineConnections), so this card, Connect Agent and the overview
// never say different things. Each row says what works, what doesn't and
// why, where to fix it, and when it was checked; a finding the machine has
// not confirmed lately says it is unknown rather than pass for now.

const dot: Record<ConnState, string> = { ok: "on", partial: "warn", missing: "bad", unknown: "", checking: "" };

export function ConnectionsCard({ m, only }: { m: Machine; only?: (r: ConnRow) => boolean }) {
  const { t } = useTranslation("common");
  const st = useStore();
  const now = useNow(30_000);
  const c = machineConnections(m, now);
  const rows = only ? c.rows.filter(only) : c.rows;
  // Coding agents connected to Aide from this machine with a grant of their own (DSH on …): they follow the machine, not the account.
  const grants = only ? [] : (grantsByCategory(st.clients, st.machines).local.get(m.machineId) ?? []);
  if (only && rows.length === 0) return null;
  const recheck = () => void local.checkAgents({ machineId: m.local ? "" : m.machineId }).catch(() => {});
  return (
    <div className="conn" role="region" aria-label={t("conn.title")}>
      {!only && (
        <div className="conn-head">
          <b>{t("conn.title")}</b>
          <span>{t("conn.followsMachine")}</span>
        </div>
      )}
      <div className="fgroup">
        {rows.length === 0 && <div className="frow muted">{m.online || m.local ? t("conn.noneYet") : t("conn.offline")}</div>}
        {rows.map((r) => (
          <div key={r.id} className="frow conn-row" data-state={r.state}>
            <span className="conn-logo"><AgentLogo agent={r.agent} size={18} /></span>
            <div className="conn-main">
              <div className="conn-line">
                <b>{r.title}</b>
                <span className="conn-state" data-state={r.state}>
                  <i className={`dotc ${dot[r.state]}`} />
                  {r.text}
                </span>
              </div>
              {r.lacks && <p className="conn-lacks">{r.lacks}</p>}
              <Fix r={r} m={m} recheck={recheck} />
              <small className="conn-when">{r.checkedAt ? t("conn.checkedAt", { when: agoText(r.checkedAt, now) }) : r.state === "checking" ? "" : t("conn.neverChecked")}</small>
            </div>
          </div>
        ))}
        {grants.map((g) => (
          <div key={g.clientId} className="frow conn-row" data-state="ok">
            <span className="conn-logo"><GrantAvatar c={g} size="sm" /></span>
            <div className="conn-main">
              <div className="conn-line">
                <b>{agentName(g.label)}</b>
                <span className="conn-state" data-state="ok">
                  <i className={`dotc ${g.online ? "on" : ""}`} />
                  {g.online ? t("conn.grantOnline") : t("conn.grantIdle")}
                </span>
              </div>
              <p className="conn-lacks">{t("conn.localGrant")}</p>
              <CategoryControl c={g} />
            </div>
          </div>
        ))}
      </div>
      {!only && (
        <p className="set-note">
          {t("conn.personalNote")}{" "}
          {(m.online || m.local) && <button className="link" onClick={recheck}>{t("conn.recheck")}</button>}
        </p>
      )}
    </div>
  );
}

/** What fixes a row, where Aide can do or show it; otherwise only what to do, said in lacks. */
function Fix({ r, m, recheck }: { r: ConnRow; m: Machine; recheck: () => void }) {
  const { t } = useTranslation("common");
  const bridge = m.status?.agents.find((a) => a.name === "deepseek")?.dshBridge;
  switch (r.fix) {
    case "prepareBridge":
      return <Install machineId={m.machineId} onPrepared={recheck} />;
    case "installBridge":
      return <Install machineId={m.machineId} packagePath={bridge?.packagePath} onPrepared={recheck} />;
    case "claudeCLI":
      return <p className="conn-fix">{t("conn.fixClaudeCLI")}</p>;
    case "claudeLogin":
      return <ClaudeLogin m={m} waiting={r.state === "checking"} />;
    default:
      return null;
  }
}

/**
 * Signing Claude Code's command line in on m (工单 #51, the owner's choice A): Aide starts its own sign-in there, a
 * browser opens on that computer, and the owner signs in in it; Aide never sees the account or the token. The row
 * above says how it goes (waiting, done, failed, cancelled), from the machine's own report.
 */
function ClaudeLogin({ m, waiting }: { m: Machine; waiting: boolean }) {
  const { t } = useTranslation("common");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const go = (cancel: boolean) => {
    setBusy(true);
    setError("");
    local.startMachineAgentLogin({ machineId: m.machineId, agent: "claude", cancel }).then(
      () => setBusy(false),
      (e) => (setError(errorText(e)), setBusy(false)),
    );
  };
  const where = m.local ? t("conn.thisComputer") : m.machineName;
  return (
    <div className="conn-fix conn-acts">
      {waiting ? (
        <button className="btn sm" disabled={busy} onClick={() => go(true)}>{t("conn.loginCancel")}</button>
      ) : (
        <button className="btn sm primary" disabled={busy || !(m.online || m.local)} onClick={() => go(false)}>{t("conn.loginOn", { where })}</button>
      )}
      {!waiting && <span className="muted">{t("conn.loginNote", { where })}</span>}
      {error && <span className="err" role="alert">{error}</span>}
    </div>
  );
}

/**
 * An agent's kind, as the owner sets it (工单 #52): told from its name until they say otherwise, which then wins
 * everywhere (kept with its grant); "Tell from its name" gives it back. Changing it says what that changes.
 */
export function CategoryControl({ c }: { c: Client }) {
  const { t } = useTranslation("common");
  const st = useStore();
  const k = clientCategory(c, st.machines);
  const [error, setError] = useState("");
  const value = !k.set ? "" : k.category === "personal" ? "personal" : `local:${c.categoryMachineId}`;
  const set = (v: string) => {
    setError("");
    const [category, machineId = ""] = v.split(":");
    local.setClientCategory({ grantId: c.grantId, category, machineId }).then(
      () => st.show(t(category === "" ? "category.cleared" : category === "personal" ? "category.setPersonal" : "category.setLocal")),
      (e) => setError(errorText(e)),
    );
  };
  return (
    <div className="conn-cat">
      <label>
        <span className="muted">{t("category.label")}</span>
        <select className="field sm" value={value} aria-label={t("category.label")} onChange={(e) => set(e.target.value)}>
          <option value="">{t("category.auto", { kind: k.set ? "" : ` · ${t(k.category === "personal" ? "category.personal" : "category.local")}` })}</option>
          <option value="personal">{t("category.personal")}</option>
          {st.machines.map((m) => (
            <option key={m.machineId} value={`local:${m.machineId}`}>{t("category.localOn", { machine: m.machineName })}</option>
          ))}
        </select>
      </label>
      <span className="muted">{t(k.category === "personal" ? "category.personalMeans" : "category.localMeans")}</span>
      {error && <span className="err" role="alert">{error}</span>}
    </div>
  );
}
