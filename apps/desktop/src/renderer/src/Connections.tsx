import { useTranslation } from "react-i18next";
import { Choices, FluentControls, Picker } from "./Picker";
import { useContext, useState } from "react";
import type { Client, Machine } from "@aide/proto/local";
import { agentName, AgentLogo } from "@aide/ui";
import { agoText, clientCategory, dateOf, grantsByCategory, machineConnections, type ConnRow, type ConnState } from "@aide/core";
import { Install } from "./dsh/DSHSessions";
import { local } from "./api";
import { GrantAvatar } from "./components";
import { Icon } from "./icons";
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
          <ConnRowView key={r.id} r={r} m={m} recheck={recheck} />
        ))}
        {grants.map((g) => (
          <GrantRow key={g.clientId} g={g} />
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

/** One connection row: what it is, its state, what that means, its fix, and when it was checked. */
export function ConnRowView({ r, m, recheck, logo = true, bare = false, part = false }: { r: ConnRow; m: Machine; recheck: () => void; logo?: boolean; bare?: boolean; part?: boolean }) {
  const { t } = useTranslation("common");
  const now = useNow(30_000);
  return (
    <div className="frow conn-row" data-state={r.state} role="group" aria-label={r.title}>
      {logo && <span className="conn-logo"><AgentLogo agent={r.agent} size={18} /></span>}
      <div className="conn-main">
        {/* bare: the entry it sits under already says its name and state (an agent with one row, 工单 #53). */}
        {/* part: one of its entry's rows, by its short name, in plain words: the entry above has the one state (工单 #53). */}
        {!bare && part && (
          <div className="conn-line conn-part">
            <span className="conn-part-name">{r.id === "claude-cli" ? t("conn.partCli") : r.id === "deepseek-bridge" ? t("conn.partBridge") : r.title}</span>
            <span>{r.text}</span>
          </div>
        )}
        {!bare && !part && (
          <div className="conn-line">
            <b>{r.title}</b>
            <span className="conn-state" data-state={r.state}>
              <i className={`dotc ${dot[r.state]}`} />
              {r.text}
            </span>
          </div>
        )}
        {r.note && <p className="conn-note">{r.note}</p>}
        {r.lacks && <p className="conn-lacks">{r.lacks}</p>}
        <Fix r={r} m={m} recheck={recheck} />
        <small className="conn-when">{r.checkedAt ? t("conn.checkedAt", { when: agoText(r.checkedAt, now) }) : r.state === "checking" ? "" : t("conn.neverChecked")}</small>
      </div>
    </div>
  );
}

/** The state dot's class for a connection state. */
export const connDot = (s: ConnState) => dot[s];

/** A coding agent connected to Aide from a machine with a grant of its own (DSH on …): it follows the machine. */
function GrantRow({ g }: { g: Client }) {
  const { t } = useTranslation("common");
  return (
    <div className="frow conn-row" data-state="ok">
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
  );
}

/**
 * The coding agents connected from machine m with grants of their own, on its one list (工单 #53); then Aide's own
 * client logged in there (client@<machine>): a connection of the account on this device, not an agent (工单 #64).
 */
export function LocalGrants({ m }: { m: Machine }) {
  const st = useStore();
  const { t } = useTranslation("common");
  const now = useNow(30_000);
  const by = grantsByCategory(st.clients, st.machines);
  const grants = by.local.get(m.machineId) ?? [];
  const clients = by.aideClients.get(m.machineId) ?? [];
  if (!grants.length && !clients.length) return null;
  return (
    <>
      {grants.length > 0 && (
        <div className="fgroup">
          {grants.map((g) => (
            <GrantRow key={g.clientId} g={g} />
          ))}
        </div>
      )}
      {clients.length > 0 && (
        <div className="fgroup" role="list" aria-label={t("conn.aideClients")}>
          {clients.map((c) => {
            const seen = dateOf(c.lastSeen);
            return (
              <div key={c.clientId} className="frow conn-row" role="listitem" data-state={c.online ? "ok" : "unknown"}>
                <span className="conn-logo"><Icon name="terminal" /></span>
                <div className="conn-main">
                  <div className="conn-line">
                    <b>{t("conn.aideClient")}</b>
                    <span className="conn-state" data-state={c.online ? "ok" : "unknown"}>
                      <i className={`dotc ${c.online ? "on" : ""}`} />
                      {c.online ? t("conn.aideClientOnline") : seen ? t("conn.aideClientSeen", { when: agoText(seen, now) }) : t("conn.aideClientNever")}
                    </span>
                  </div>
                  <p className="conn-lacks">{t("conn.aideClientNote", { label: c.label })}</p>
                </div>
              </div>
            );
          })}
        </div>
      )}
    </>
  );
}

/** What fixes a row, where Aide can do or show it; otherwise only what to do, said in lacks. */
function Fix({ r, m, recheck }: { r: ConnRow; m: Machine; recheck: () => void }) {
  const { t } = useTranslation("common");
  const bridge = m.status?.agents.find((a) => a.name === "deepseek")?.dshBridge;
  switch (r.fix) {
    case "prepareBridge":
    case "installBridge":
      return <Install machineId={m.machineId} packagePath={bridge?.packagePath} install={bridge?.install} onPrepared={recheck} />;
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
  const options = [
    { value: "", label: t("category.auto", { kind: k.set ? "" : ` · ${t(k.category === "personal" ? "category.personal" : "category.local")}` }) },
    { value: "personal", label: t("category.personal") },
    ...st.machines.map((m) => ({ value: `local:${m.machineId}`, label: t("category.localOn", { machine: m.machineName }) })),
  ];
  // On a bot's settings page its choices are laid out at once (工单 #97 补充三).
  const fluent = useContext(FluentControls);
  return (
    <div className="conn-cat">
      {fluent ? (
        <>
          <span className="bset-k">{t("category.label")}</span>
          <Choices value={value} label={t("category.label")} onChange={set} options={options} />
        </>
      ) : (
        <label>
          <span className="muted">{t("category.label")}</span>
          <Picker size="sm" value={value} label={t("category.label")} onChange={set} options={options} />
        </label>
      )}
      <span className="muted">{t(k.category === "personal" ? "category.personalMeans" : "category.localMeans")}</span>
      {error && <span className="err" role="alert">{error}</span>}
    </div>
  );
}
