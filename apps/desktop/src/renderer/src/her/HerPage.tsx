import { useCallback, useEffect, useMemo, useState } from "react";
import { runningAgents } from "@aide/her-client";
import { useTranslation } from "react-i18next";
import type { Methods, Op } from "@aide/her-protocol";
import type { Machine } from "@aide/proto/local";
import { agoText } from "@aide/core";
import { Mark } from "@aide/ui";
import { errorText, useNow, useStore } from "../store";
import type { SendOp } from "./op";
import { AssistantPanel } from "./AssistantPanel";
import { useConversation, useEnvironment, useKept, useWaiting } from "./connection";
import { HerContext, useHer, type Her, type HerSheet, type Panel } from "./context";
import { Conversation, putInComposer, showRun } from "./Conversation";
import { DeliveriesPanel } from "./Deliveries";
import { Progress } from "./Progress";
import { environments, envState, herChoices, herScope } from "./environments";
import { local } from "../api";
import { ErrorLine, useAction } from "../sheets/common";
import { RoutinesPanel } from "./RoutinesPanel";
import { BrowserPage, EnvironmentPage, ModelPage, ProviderPage, SearchPage, SkillPage, SkillsPage, StoragePage } from "./HerPages";
import { FirstRun, Setup } from "./Setup";
import { HerDialog } from "./sheets";
import { TopBar } from "./TopBar";
import "../session/markdown/markdown.css";
import "../session/session.css";
import "./her.css";

// The Her entry (docs/her-server.md §9): a conversation with Her, the
// environment's one assistant the apps show for now, full width; its
// environments one at a time, chosen beside its name when there are several.
// The first run while Her runs nowhere. Connections to her are open only
// while this page shows.

/** The environment shown last. */
function rememberedEnv(): string | undefined {
  try {
    return localStorage.getItem("her-env") ?? undefined;
  } catch {
    return undefined;
  }
}

function rememberEnv(machine: string) {
  try {
    localStorage.setItem("her-env", machine);
  } catch {
    // A convenience: without storage the first environment shows.
  }
}

export default function HerPage() {
  const st = useStore();
  const envs = useMemo(() => environments(st.machines), [st.machines]);
  const wanted = st.selection.kind === "her" || st.selection.kind === "progress" ? st.selection.machine : undefined;
  // This computer's Her, or that it is off; another machine's only once the owner picked it (herScope).
  const scope = herScope(st.machines, wanted, rememberedEnv());
  const machine = scope.kind === "env" ? scope.machine : undefined;
  useEffect(() => {
    if (machine) rememberEnv(machine.machineId);
  }, [machine?.machineId]);
  const { set } = st;
  // Another environment: of the page shown, Her's conversation or what she sent out.
  const progress = st.selection.kind === "progress";
  const choose = useCallback((machineId: string) => set({ selection: { kind: progress ? "progress" : "her", machine: machineId } }), [set, progress]);
  if (scope.kind === "localOff") return <LocalOff machine={scope.machine} envs={envs} choose={choose} />;
  if (!machine) return <FirstRun onDone={choose} />;
  return <Environment key={machine.machineId} machine={machine} envs={envs} choose={choose} />;
}

function Environment({ machine, envs, choose }: { machine: Machine; envs: Machine[]; choose: (machineId: string) => void }) {
  const { t } = useTranslation("her");
  const st = useStore();
  const { show, set } = st;
  const env = useEnvironment(machine);
  const kept = useKept(machine.machineId);
  const waiting = useWaiting(machine);
  const [panel, setPanel] = useState<Panel>(null);
  const [sheet, setSheet] = useState<HerSheet>(null);
  const id = machine.machineId;
  // Until her answers, and while the machine is offline: Her and the conversation as this Mac keeps them.
  const bot = env.bots.find((b) => b.default) ?? kept?.bot;
  const live = useConversation(machine, bot, show);
  const conv = live.view || !kept?.view ? live : { ...live, view: kept.view, session: kept.session };
  // Her's sub-agents at work, for the Progress button on her page, while her conversation is followed here. None waits:
  // her asks nobody before an action.
  const followed = live.rpc ? live.view : null;
  const agentsWaiting = followed ? 0 : -1;
  const agentsRunning = followed ? runningAgents(followed).length : -1;
  useEffect(() => {
    set({ herAgents: agentsWaiting < 0 ? null : { waits: agentsWaiting, running: agentsRunning } });
  }, [agentsWaiting, agentsRunning, set]);
  useEffect(() => () => set({ herAgents: null }), [set]);
  // Delivered meanwhile: what waits is asked for again as Her changes.
  const refreshWaiting = waiting.refresh;
  const anyWaiting = waiting.list.length > 0;
  useEffect(() => {
    if (anyWaiting) refreshWaiting();
  }, [bot, anyWaiting, refreshWaiting]);
  const rpc = env.rpc;
  const call = useCallback(
    <M extends keyof Methods & string>(method: M, params: Methods[M][0]): Promise<Methods[M][1]> =>
      rpc ? rpc.request(method, params) : Promise.reject(new Error(t("env.notConnected", { name: machine.machineName }))),
    [rpc, machine.machineName],
  );
  const run = useCallback(
    async (action: () => Promise<unknown>, done?: string) => {
      try {
        await action();
        if (done) show(done);
        return true;
      } catch (e) {
        show(errorText(e));
        return false;
      }
    },
    [show],
  );
  const openConsole = useCallback((page?: string) => {
    window.aide?.openHerConsole(id, page).catch((e: unknown) => show(errorText(e)));
  }, [id, show]);
  // Her's conversation, as the page and the panels beside it act on it.
  const op: SendOp = useCallback(
    async (o: Op) => {
      if (!conv.rpc || !conv.session) {
        show(t("env.notConnected", { name: machine.machineName }));
        return false;
      }
      try {
        await conv.rpc.request("session/op", { session_id: conv.session, op: o });
        return true;
      } catch (e) {
        show(errorText(e));
        return false;
      }
    },
    [conv.rpc, conv.session, machine.machineName, show],
  );
  const her = useMemo<Her>(
    () => ({ machine, envs, choose, env, bot, panel, setPanel, sheet, setSheet, call, run, openConsole }),
    [machine, envs, choose, env, bot, panel, sheet, call, run, openConsole],
  );
  // Progress (docs/her-team.md §15) is this page too: the same connections, so going from one to the other reads nothing again.
  const sel = st.selection;
  const progress = sel.kind === "progress" && !!bot;
  const shown = bot && !progress ? panel : null;
  // Back to Her's page, from a page it opened.
  const home = () => setPanel({ kind: "settings" });
  return (
    <HerContext.Provider value={her}>
      <div className={`her ${shown ? "bgopen" : ""}`}>
        <div className="scol">
          {progress ? (
            <Progress bot={bot} conv={conv} op={op} task={sel.kind === "progress" ? sel.task : undefined} />
          ) : (
            <>
              <TopBar />
              {!machine.local && <NotHere machine={machine} choose={choose} />}
              {bot ? <Conversation bot={bot} conv={conv} op={op} waiting={waiting.list} onWaiting={refreshWaiting} /> : <EnvStatus machine={machine} />}
            </>
          )}
        </div>
        {bot && shown?.kind === "settings" && (
          <AssistantPanel bot={bot} view={conv.view} op={op} files={{ rpc: conv.rpc, session: conv.session ?? "", bot: bot.id }} onClose={() => setPanel(null)} />
        )}
        {bot && shown?.kind === "routines" && (
          <RoutinesPanel
            bot={bot}
            view={conv.view}
            route={shown.route}
            onDraft={(text) => {
              setPanel(null);
              putInComposer(id, text);
            }}
            onShowBubble={(runId) => showRun(id, runId)}
            onBack={home}
            onClose={() => setPanel(null)}
          />
        )}
        {bot && shown?.kind === "files" && <DeliveriesPanel bot={bot} kept={kept?.deliveries ?? null} onBack={home} onClose={() => setPanel(null)} />}
        {bot && shown?.kind === "skills" && <SkillsPage onBack={home} onClose={() => setPanel(null)} />}
        {bot && shown?.kind === "skill" && <SkillPage key={shown.dir} dir={shown.dir} onBack={() => setPanel({ kind: "skills" })} onClose={() => setPanel(null)} />}
        {bot && shown?.kind === "search" && <SearchPage onBack={home} onClose={() => setPanel(null)} />}
        {bot && shown?.kind === "browser" && <BrowserPage onBack={home} onClose={() => setPanel(null)} />}
        {bot && shown?.kind === "model" && <ModelPage view={conv.view} op={op} onBack={home} onClose={() => setPanel(null)} />}
        {bot && shown?.kind === "provider" && <ProviderPage key={shown.id} id={shown.id} onBack={() => setPanel({ kind: "model" })} onClose={() => setPanel(null)} />}
        {bot && shown?.kind === "environment" && <EnvironmentPage onBack={home} onClose={() => setPanel(null)} />}
        {bot && shown?.kind === "storage" && <StoragePage onBack={() => setPanel({ kind: "environment" })} onClose={() => setPanel(null)} />}
        {sheet?.kind === "add" && <AddEnvironmentSheet />}
      </div>
    </HerContext.Provider>
  );
}

/**
 * This computer's Her is off (the owner's rule, 2026-11-21): the page says so and turns her on here, in the
 * app, as the first run does (SetHer, which the owner confirms). Another machine's Her shows only when the
 * owner picks it below; never in this one's place.
 */
function LocalOff({ machine, envs, choose }: { machine: Machine; envs: Machine[]; choose: (machineId: string) => void }) {
  const { t } = useTranslation("her");
  const st = useStore();
  const act = useAction();
  const choice = herChoices(st.machines).find((c) => c.machine.machineId === machine.machineId);
  const why =
    choice?.why === "needsUpdate" ? t("setup.needsUpdate")
    : choice?.why === "windows" ? t("setup.windows")
    : choice?.why === "unsupported" ? t("setup.unsupported", { detail: choice.detail })
    : "";
  const others = envs.filter((m) => !m.local);
  return (
    <div className="her-first">
      <div className="ctool drag" />
      <div className="her-first-in">
        <Mark size={44} variant={act.busy ? "pulse" : undefined} />
        <h1>{t("scope.offTitle")}</h1>
        <p className="lede">{t("scope.offSub", { name: machine.machineName })}</p>
        {why ? (
          <p className="her-setup-note">{why}</p>
        ) : (
          <button className="btn primary" disabled={act.busy} onClick={() => void act.go(() => local.setHer({ machineId: machine.machineId, enabled: true }))}>
            {act.busy ? t("setup.turningOn") : t("scope.turnOnHere")}
          </button>
        )}
        <p className="her-setup-note">{t("grant.desc")}</p>
        <ErrorLine text={act.error} />
        {others.length > 0 && (
          <div className="her-choices" role="list" aria-label={t("scope.others")}>
            <p className="her-setup-note">{t("scope.others")}</p>
            {others.map((m) => (
              <div key={m.machineId} className="her-choice" role="listitem">
                <div className="her-choice-t">
                  <b>{m.machineName}</b>
                </div>
                <button className="btn" onClick={() => choose(m.machineId)}>
                  {t("scope.view")}
                </button>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

/** Over another machine's Her, for as long as it shows: whose Her this is, and the way back to this computer's. */
function NotHere({ machine, choose }: { machine: Machine; choose: (machineId: string) => void }) {
  const { t } = useTranslation("her");
  const st = useStore();
  const here = st.machines.find((m) => m.local);
  return (
    <div className="her-nothere" role="status">
      <span>{t("scope.viewing", { name: machine.machineName })}</span>
      {here && (
        <button className="link" onClick={() => choose(here.machineId)}>
          {t("scope.backHere")}
        </button>
      )}
    </div>
  );
}

/** In place of the conversation while the environment has none to show: Her starting there, failing to, or the machine away. */
function EnvStatus({ machine }: { machine: Machine }) {
  const { t } = useTranslation("her");
  const now = useNow(30_000);
  const state = envState(machine);
  const name = machine.machineName;
  const body = (() => {
    switch (state.kind) {
      case "offline":
        return (
          <div className="empty" role="status">
            <Mark size={40} variant="wait" />
            <b>{t("env.offline", { name })}</b>
            <span>{state.seen ? t("env.offlineSeen", { name, ago: agoText(state.seen, now) }) : t("chat.unread", { name })}</span>
          </div>
        );
      case "failed":
        return (
          <div className="empty" role="alert">
            <Mark size={40} variant="fail" />
            <b>{t("env.failed", { name })}</b>
            <span className="sel-text">{state.error}</span>
          </div>
        );
      case "starting":
        return (
          <div className="empty" role="status">
            <Mark size={40} variant="pulse" />
            <b>{t("env.starting", { name })}</b>
          </div>
        );
      default:
        return (
          <div className="empty">
            <span className="mini-spin" />
          </div>
        );
    }
  })();
  return <div className="dwrap">{body}</div>;
}

/** Another environment: the first run's choices, in a sheet. */
function AddEnvironmentSheet() {
  const { t } = useTranslation("her");
  const st = useStore();
  const her = useHer();
  const close = () => her.setSheet(null);
  return (
    <HerDialog label={t("setup.addTitle")} width={520} onClose={close}>
      <h3 id="sheet-title">{t("setup.addTitle")}</h3>
      <p className="sub">{t("setup.sub")}</p>
      <Setup
        onDone={(id) => {
          close();
          her.choose(id);
        }}
        onNewServer={() => {
          close();
          st.set({ sheet: { kind: "addMachine" } });
        }}
      />
      <div className="sheet-foot">
        <button className="btn" onClick={close}>
          {t("sheet.cancel")}
        </button>
      </div>
    </HerDialog>
  );
}
