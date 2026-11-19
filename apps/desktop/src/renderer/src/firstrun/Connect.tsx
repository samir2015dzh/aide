import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { PairingDetails } from "@hermesh/proto/local";
import { scopeComplete } from "@hermesh/core";
import { Mark, personalAgents } from "@hermesh/ui";
import { PersonalAgentIcon } from "../components";
import { ConnectGuide, LinePicker, type Place, type Tab } from "../ConnectGuide";
import { useConnect, Waiting } from "../connect";
import { Icon } from "../icons";
import { kindName } from "../personal";
import { SheetHost } from "../Sheets";
import { ErrorLine, SignerWaiting } from "../sheets/common";
import { SelfReport } from "../sheets/Pairing";
import { ScopeRows } from "../sheets/Scope";
import { useStore } from "../store";
import { Frame, StepIcon } from "./Frame";
import { setProgress } from "./progress";

// The first run's Connect step: a separate guide for each coding agent
// (DeepSeek Harness, Claude, Codex, any other ACP agent: ConnectGuide), and
// the assistants that give them tasks on their own tab. There, which
// assistant (only its name differs in the message), the message to send it,
// and once any assistant's request arrives, what it may use and approving
// it (useConnect); the verification code is compared in the signer's
// window. The first task follows once no request waits, with the first
// assistant approved. A request that arrives on another tab is said at the
// top.

export function Connect() {
  const { t } = useTranslation("firstrun");
  const { t: ts } = useTranslation("sheets");
  const tc = useTranslation("connect").t;
  const st = useStore();
  const c = useConnect();
  const { request, act } = c;
  const [first, setFirst] = useState<string>();
  const [tab, setTab] = useState<Tab>("deepseek");
  const assistants = tab === "assistants";

  const approve = async (p: PairingDetails) => {
    const rest = c.waiting.length;
    if (!(await c.approve(p))) return;
    if (rest) {
      setFirst((f) => f ?? p.label);
      st.show(ts("pairing.approvedClient", { name: p.label }));
    } else {
      setProgress({ step: "first", agent: first ?? p.label });
    }
  };
  // A task the guide started, or DSH's own sessions: the first run ends there.
  const place: Place = {
    openSession: (id) => (setProgress(null), st.select(id)),
    openDSH: () => (setProgress(null), st.set({ selection: { kind: "dsh" } })),
  };

  return (
    <Frame
      sheet={st.sheet?.kind === "addProject" ? <SheetHost /> : undefined}
      foot={
        <>
          <p className="promise">
            <Icon name="shield" />
            {assistants ? t("connect.promise") : t("prepare.promise")}
          </p>
          {!act.busy && (
            <>
              {st.status?.localMachineEnabled && (
                <button className="btn" onClick={() => setProgress({ step: "prepare" })}>
                  {t("back")}
                </button>
              )}
              {assistants ? (
                <button className="link" onClick={() => setProgress(first ? { step: "first", agent: first } : null)}>
                  {t("connect.later")}
                </button>
              ) : (
                <button className="btn" onClick={() => setProgress(first ? { step: "first", agent: first } : null)}>
                  {t("firstTask.enter")}
                </button>
              )}
            </>
          )}
        </>
      }
    >
      <StepIcon name="link" />
      <h2 className="ob-h">{tc("title")}</h2>
      <p className="ob-p">{tc("lede")}</p>
      <LinePicker tab={tab} set={setTab} />
      {!assistants && request && (
        <p className="cg-note cg-notice" role="status">
          {tc("notice.request", { name: request.label })}{" "}
          <button className="link" onClick={() => setTab("assistants")}>
            {tc("notice.review")}
          </button>
        </p>
      )}
      {assistants ? <Assistants c={c} approve={approve} /> : <ConnectGuide line={tab} place={place} />}
    </Frame>
  );
}

/** The assistants' tab: the message to send one, then its request to approve. */
function Assistants({ c, approve }: { c: ReturnType<typeof useConnect>; approve: (p: PairingDetails) => void }) {
  const { t } = useTranslation("firstrun");
  const { t: ts } = useTranslation("sheets");
  const tc = useTranslation("connect").t;
  const st = useStore();
  const { request, act } = c;
  return (
    <>
      <p className="ob-p">{tc("assistants.intro")}</p>
      <p className="ob-p">{request ? ts(request.confirmed ? "newGrant.requestBodyConfirmed" : "newGrant.requestBody", { name: request.label }) : ts("newGrant.messageBody")}</p>
      {request ? (
        <div className="pa-step" key={request.code}>
          {!act.busy && <Waiting c={c} another />}
          <p className="sheet-q pa-who">
            <PersonalAgentIcon kind={c.requestKind!} size={28} />
            {ts("pairing.canUse", { name: request.label })}
          </p>
          <ScopeRows scope={c.scope} onChange={c.setScope} />
          <p className="sheet-note muted">{ts("pairing.scopeNote")}</p>
          <p className="sheet-note muted">
            <SelfReport p={request} />
          </p>
          <ErrorLine text={act.error} />
          {act.busy ? (
            <SignerWaiting detail={ts("newGrant.signerDetail", { name: request.label })} />
          ) : (
            <div className="sheet-actions">
              <button className="btn" onClick={() => c.deny(request, ts("newGrant.denied"))}>
                {ts("deny")}
              </button>
              <button className="btn primary" disabled={!scopeComplete(c.scope)} onClick={() => approve(request)}>
                <Icon name="lock" />
                {ts("approve")}
              </button>
            </div>
          )}
        </div>
      ) : (
        <>
          <Waiting c={c} />
          <div className="pa-pick" role="radiogroup" aria-label={ts("newGrant.pick")}>
            {personalAgents.map((k) => (
              <button
                key={k.id}
                className={`pa ${k.id === c.kind.id ? "on" : ""}`}
                role="radio"
                aria-checked={k.id === c.kind.id}
                aria-label={kindName(k)}
                onClick={() => c.pick(k.id)}
              >
                <PersonalAgentIcon kind={k} size={44} />
                <b>{kindName(k)}</b>
              </button>
            ))}
          </div>
          <div className="pa-step">
            {c.other && (
              <div className="pa-name">
                <label htmlFor="cf-name">{ts("newGrant.name")}</label>
                <input id="cf-name" className="field" maxLength={64} value={c.otherName} onChange={(e) => c.rename(e.target.value)} />
              </div>
            )}
            <div className="pa-msg">
              <div className="msgbox sel-text">{c.message}</div>
              <button className={`btn ${c.copied ? "" : "primary"}`} onClick={c.copy}>
                <Icon name={c.copied ? "check" : "copy"} />
                {c.copied ? t("copied") : t("copy")}
              </button>
            </div>
            <div className="pa-wait">
              <Mark size={16} variant="seq" />
              {c.copied ? ts("newGrant.waiting", { name: c.label }) : ts("newGrant.copyHint", { name: c.label })}
            </div>
          </div>
        </>
      )}
    </>
  );
}
