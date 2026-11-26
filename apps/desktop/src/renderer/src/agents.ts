import { useEffect } from "react";
import { STALE_AFTER_MS } from "@aide/core";
import { local } from "./api";
import { useStore } from "./store";

// This Mac's coding agents are checked (each CLI run) only when something
// needs to know, never in the background: a page showing them asks.

const check = () => void local.checkAgents({}).catch(() => {}); // the next poll shows what came of it

/** Checks this Mac's coding agents once a page showing them opens (on: it shows them). */
export function useCheckAgents(on = true) {
  useEffect(() => {
    if (on) check();
  }, [on]);
}

/** Checks them again every few seconds while on: waiting for a sign-in in Terminal. */
export function useRecheckAgents(on: boolean) {
  useEffect(() => {
    if (!on) return;
    const id = setInterval(check, 3000);
    return () => clearInterval(id);
  }, [on]);
}

/**
 * While the main window is open, this computer's coding agents are checked again before what was last found of them
 * grows old (工单 #87): past STALE_AFTER_MS with no check, every screen calls them unknown, which with nobody asking
 * read as "all offline at once". A check is asked once two thirds of that has gone by, at most that often.
 */
export function useKeepLocalChecked() {
  const st = useStore();
  const m = st.machines.find((x) => x.local);
  const oldest = Math.min(...(m?.status?.agents ?? []).filter((a) => !a.hidden && a.checkedAt).map((a) => Number(a.checkedAt!.seconds) * 1000));
  useEffect(() => {
    if (!Number.isFinite(oldest) || st.reconnecting) return;
    const due = oldest + (STALE_AFTER_MS * 2) / 3 - Date.now();
    const id = setTimeout(check, Math.max(due, 0));
    return () => clearTimeout(id);
  }, [oldest, st.reconnecting]);
}

/** How long what a machine's agents offer stands before a model list opening asks again: the machine's own TTL for DSH's probe (工单 #166). */
export const MODELS_TTL_MS = 5 * 60_000;
const modelsAsked = new Map<string, number>();

/**
 * A model list is opening (新建会话、设置里的默认模型、会话里换模型，工单 #129 返修): what the machine's coding agents
 * offer is asked again on purpose, at most every MODELS_TTL_MS per machine, so the list is not what the app found at
 * its start. On purpose, an agent that tells its models only in a session (DSH) is asked again once its last answer
 * is past that TTL; the list updates when the machine's status next comes.
 */
export function freshModels(m: { local?: boolean; machineId: string } | undefined) {
  if (!m) return;
  const key = m.local ? "" : m.machineId;
  const now = Date.now();
  if (now - (modelsAsked.get(key) ?? -Infinity) < MODELS_TTL_MS) return;
  modelsAsked.set(key, now);
  void local.checkAgents({ machineId: key, onPurpose: true }).catch(() => {});
}
