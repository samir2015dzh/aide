import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Mark, personalAgents, type PersonalAgentKind } from "@aide/ui";
import { PersonalAgentIcon } from "./components";
import { Icon } from "./icons";
import { assistantSteps, byMCP, mcpApp, mcpState } from "./assistantSteps";
import { connectMessage, kindName } from "./personal";
import { useStore } from "./store";
import { i18n } from "../../shared/i18n";

// Connecting an assistant (docs/accounts.md §3): it logs in with the
// owner's account and password itself, and is connected at once; no one
// approves it here. An app that speaks MCP (Claude Desktop, ChatGPT) adds
// the server's MCP address as a connector and logs in on its page; any
// other is sent a message to run the CLI's `aide auth login`. What it may
// use is every machine and project, its own tasks, until signed out;
// changed afterwards from Settings › Access.

export function useConnect() {
  const st = useStore();
  const [kindId, setKindId] = useState(personalAgents[0].id);
  const kind = personalAgents.find((k) => k.id === kindId) ?? personalAgents[0];
  const other = kind.id === "other";
  const [otherName, setOtherName] = useState("My Agent");
  const [copied, setCopied] = useState(false);
  // The agents there when this was opened: one logging in after is the one connected.
  const [before] = useState(() => new Set(st.clients.map((c) => c.clientId)));
  const label = (other ? otherName.trim() : kind.name) || "My Agent";
  const mcp = byMCP.has(kind.id);
  const text = mcp ? (st.status?.mcpUrl ?? "") : connectMessage(st.status, label);
  const shown = assistantSteps(kind.id, mcpState(st.status));
  return {
    kind,
    other,
    otherName,
    label,
    copied,
    mcp,
    /** What to copy: the MCP address, or the message to send. */
    text,
    /** Its steps, whether there is something to copy, and the note in its place when not (assistantSteps). */
    shown,
    /** The agent that logged in since this was opened, if one did. */
    arrived: st.clients.find((c) => !before.has(c.clientId)),
    pick(id: string) {
      setKindId(id);
      setCopied(false);
    },
    rename(name: string) {
      setOtherName(name);
      setCopied(false);
    },
    copy() {
      navigator.clipboard.writeText(text).catch(() => {});
      setCopied(true);
    },
  };
}

/** The assistants' line: pick one, its steps and what to copy, then it shows here once it logged in. */
export function Assistants({ c, size = 44 }: { c: ReturnType<typeof useConnect>; size?: number }) {
  const { t } = useTranslation("connect");
  const st = useStore();
  if (c.arrived)
    return (
      <div className="big-ok">
        <span className="okc">
          <Icon name="check" />
        </span>
        <b style={{ fontSize: 15 }}>{t("assistants.connected", { name: c.arrived.label })}</b>
        <span className="muted" style={{ fontSize: 12.5 }}>{t("assistants.connectedBody")}</span>
        <button className="link" onClick={() => st.set({ sheet: { kind: "scope", clientId: c.arrived!.clientId, renew: false } })}>
          {t("assistants.scope")}
        </button>
      </div>
    );
  return (
    <>
      <div className="pa-pick" role="radiogroup" aria-label={t("assistants.pick")}>
        {personalAgents.map((k: PersonalAgentKind) => (
          <button key={k.id} className={`pa ${k.id === c.kind.id ? "on" : ""}`} role="radio" aria-checked={k.id === c.kind.id} aria-label={kindName(k)} onClick={() => c.pick(k.id)}>
            <PersonalAgentIcon kind={k} size={size} />
            <b>{kindName(k)}</b>
          </button>
        ))}
      </div>
      <div className="pa-step">
        {c.other && (
          <div className="pa-name">
            <label htmlFor="cf-name">{t("assistants.name")}</label>
            <input id="cf-name" className="field" maxLength={64} value={c.otherName} onChange={(e) => c.rename(e.target.value)} />
          </div>
        )}
        {c.shown.steps.length > 0 && (
          <ol className="cg-list">
            {c.shown.steps.map((k) => (
              <li key={k}>{t(`assistants.${k}` as never, { name: c.label, app: mcpApp[c.kind.id] ?? c.kind.name, password: `<${i18n.t("common:personal.passwordArg")}>` })}</li>
            ))}
          </ol>
        )}
        {c.shown.note ? (
          <p className="cg-note" data-testid="mcp-note">{t(`assistants.${c.shown.note}` as never)}</p>
        ) : (
          <div className="pa-msg">
            <div className="msgbox sel-text">{c.text}</div>
            <button className={`btn ${c.copied ? "" : "primary"}`} onClick={c.copy}>
              <Icon name={c.copied ? "check" : "copy"} />
              {c.copied ? t("assistants.copied") : t("assistants.copy")}
            </button>
          </div>
        )}
        <p className="cg-note">{t("assistants.password")}</p>
        <div className="pa-wait">
          <Mark size={16} variant="seq" />
          {t("assistants.waiting", { name: c.label })}
        </div>
      </div>
    </>
  );
}
