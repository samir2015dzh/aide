import { type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { Client } from "@aide/proto/local";
import { FailureKind } from "@aide/proto/session";
import { dateOf, displayTitle, failureLine, isArchivable, pendingQuestion } from "@aide/core";
import { i18n } from "../../../shared/i18n";
import { local } from "../api";
import { opensActivityMonitor, problemBody, problemsOf, problemTitle, severe } from "../health";
import { Icon } from "../icons";
import { archive, senderLine } from "../Sidebar";
import { useStore, type Store } from "../store";
import { QuickAnswer, useTeam } from "../team";

const day = 86_400_000;

const retryableKinds = new Set([
  FailureKind.RATE_LIMIT,
  FailureKind.PROVIDER_ERROR,
  FailureKind.NETWORK,
  FailureKind.AGENT_CRASHED,
  FailureKind.STALLED,
  FailureKind.TIMEOUT,
]);

/** Grants that run out within a week. */
export function expiringClients(st: Store, now: Date): Client[] {
  return st.clients.filter((c) => {
    const d = !c.revoked && dateOf(c.expiresAt);
    return d && d.getTime() - now.getTime() < 7 * day;
  });
}

/**
 * "Needs You": what waits on the owner. Machines kept from working first;
 * then the sessions that ask a question, answered here when one click can
 * (docs/her-team.md §14.4), and those whose turn failed; then grants about
 * to expire.
 * A machine only has only its own health here.
 */
export function Attention({ now }: { now: Date }) {
  const { t } = useTranslation("overview");
  const st = useStore();
  const owner = !st.status?.machineOnly;
  const expiring = owner ? expiringClients(st, now) : [];
  const sick = st.machines.flatMap((m) => problemsOf(m).map((p) => ({ m, p })));
  const team = useTeam();
  const count = sick.length + team.waiting.length + team.failed.length + expiring.length;
  if (count === 0) return null;
  return (
    <section id="ov-attention" aria-label={t("attention.title")}>
      <div className="sec-h">{t("attention.title")}<span className="n">{count}</span></div>
      <div className="card attn" role="list">
        {sick.map(({ m, p }) => (
          <Row key={`${m.machineId}:${p.kind}`} icon="warn" tint={severe(p) ? "r" : "y"} title={problemTitle(m, p)} detail={problemBody(p)} wrap>
            {opensActivityMonitor(m, p) ? (
              <button className="btn" onClick={() => window.aide?.openActivityMonitor()}>{i18n.t("common:health.openActivityMonitor")}</button>
            ) : (
              <button className="btn" onClick={() => window.aide?.openSettings(`machines/${m.machineId}`)}>{i18n.t("common:health.view")}</button>
            )}
          </Row>
        ))}
        {team.waiting.map((s) => {
          const q = pendingQuestion(s);
          const words = q?.items.map((i) => i.text || i.header).filter(Boolean).join(" ");
          return (
            <Row key={s.sessionId} icon="q" tint="y" title={displayTitle(s)} detail={words ? `${senderLine(st, s)} · ${words}` : senderLine(st, s)} wrap>
              <QuickAnswer s={s} />
              <button className="btn" onClick={() => st.select(s.sessionId)}>{t("attention.open")}</button>
            </Row>
          );
        })}
        {team.failed.map((s) => {
          const canRetry = s.createdBy === "owner" && (!s.progress?.lastFailure || retryableKinds.has(s.progress.lastFailure.kind));
          return (
            <Row key={s.sessionId} icon="alert" tint="r" title={displayTitle(s)} detail={`${senderLine(st, s)} · ${failureLine(s) || i18n.t("common:team.lastTurnFailed")}`}>
              {canRetry && (
                <button
                  className="btn"
                  onClick={() =>
                    void st.run(
                      () => local.sendMessage({ sessionId: s.sessionId, text: t("attention.retryPrompt"), attachments: [] }),
                      t("attention.retried", { title: displayTitle(s) }),
                    )
                  }
                >
                  <Icon name="retry" className="sm" />
                  {t("attention.retry")}
                </button>
              )}
              {isArchivable(s) && (
                <button className="btn" onClick={() => archive(st, s)}>
                  {t("attention.archive")}
                </button>
              )}
              <button className="btn" onClick={() => st.select(s.sessionId)}>{t("attention.open")}</button>
            </Row>
          );
        })}
        {expiring.map((c) => {
          const days = Math.max(0, Math.ceil(((dateOf(c.expiresAt)?.getTime() ?? 0) - now.getTime()) / day));
          return (
            <Row key={c.clientId} icon="clock" tint="y" title={t("attention.expires", { label: c.label, count: days })} detail={t("attention.expiresDetail")}>
              <button className="btn" onClick={() => st.set({ sheet: { kind: "scope", clientId: c.clientId, renew: true } })}>{t("attention.renew")}</button>
            </Row>
          );
        })}
      </div>
    </section>
  );
}

function Row({ icon, tint, title, detail, wrap, children }: { icon: string; tint: string; title: string; detail: ReactNode; wrap?: boolean; children: ReactNode }) {
  return (
    <div className="arow" role="listitem">
      <span className={`ai ${tint}`}><Icon name={icon} /></span>
      <div className="at">
        <b>{title}</b>
        <span className={wrap ? "wrap" : undefined}>{detail}</span>
      </div>
      {children}
    </div>
  );
}
