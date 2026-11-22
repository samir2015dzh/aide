import { useTranslation } from "react-i18next";
import type { Machine } from "@aide/proto/local";
import { agentName, AgentLogo } from "@aide/ui";
import { agoText, grantsByCategory, machineConnections, type ConnRow, type ConnState } from "@aide/core";
import { Install } from "./dsh/DSHSessions";
import { local } from "./api";
import { GrantAvatar } from "./components";
import { useNow, useStore } from "./store";
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
          <div key={r.id} className={`frow conn-row ${r.state}`}>
            <span className="conn-logo"><AgentLogo agent={r.agent} size={18} /></span>
            <div className="conn-main">
              <div className="conn-line">
                <b>{r.title}</b>
                <span className={`conn-state ${r.state}`}>
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
          <div key={g.clientId} className="frow conn-row ok">
            <span className="conn-logo"><GrantAvatar c={g} size="sm" /></span>
            <div className="conn-main">
              <div className="conn-line">
                <b>{agentName(g.label)}</b>
                <span className="conn-state ok">
                  <i className={`dotc ${g.online ? "on" : ""}`} />
                  {g.online ? t("conn.grantOnline") : t("conn.grantIdle")}
                </span>
              </div>
              <p className="conn-lacks">{t("conn.localGrant")}</p>
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
    default:
      return null;
  }
}
