import type { Persona } from "@aide/proto/identity";
import type { Machine } from "@aide/proto/local";
import type { AgentStatus } from "@aide/proto/machine";
import { codingBotState, type BotState } from "./roster";

// Handing work out (工单 #41): whom to, listed by themselves. Each machine's
// coding agents the machine detected are there, grouped by machine, with no
// contact to make first; a contact is only a custom face for one of them
// (its name, avatar, default project), shown on the same line, never a
// second one. A contact whose machine or agent is not detected now stays,
// under its machine, as what it is.

export type Target = {
  key: string;
  machineId: string;
  agent: string;
  /** The contact that is this agent's custom face, if any. */
  persona?: Persona;
  /** Work can be sent to it now: its state is ready, or not confirmed (unknown, checking) while its machine answers. */
  ready: boolean;
  /** Its one state, as the machine page and the roster say it (工单 #56); unset for a contact whose agent is not there. */
  state?: BotState;
  /** Why not, or why it is not confirmed. */
  why?: BotState["why"];
};

export type TargetGroup = { machine: Machine; targets: Target[] };

/** Detected: found and working, not hidden (an agent the machine is unsure of is not offered as there). */
function detected(a: AgentStatus): boolean {
  return a.available && !a.hidden;
}

export function dispatchTargets(machines: readonly Machine[], personas: readonly Persona[], now: Date = new Date()): TargetGroup[] {
  const groups: TargetGroup[] = [];
  const used = new Set<string>();
  const sorted = [...machines].sort((a, b) => Number(b.local) - Number(a.local) || Number(b.online) - Number(a.online) || a.machineName.localeCompare(b.machineName));
  for (const m of sorted) {
    const reachable = m.online || m.local;
    const targets: Target[] = [];
    for (const a of m.status?.agents ?? []) {
      if (!detected(a)) continue;
      const persona = personas.find((p) => p.machineId === m.machineId && p.agent === a.name && !used.has(p.personaId));
      if (persona) used.add(persona.personaId);
      // The one state of it (工单 #56): an unknown one is never passed off as ready, nor refused for want of a check.
      const state = codingBotState(m, a.name, now);
      const why = !reachable ? "offline" : state?.why;
      const ready = reachable && (state ? state.usable !== "no" : a.loggedIn && !a.pause);
      targets.push({ key: `${m.machineId}/${a.name}`, machineId: m.machineId, agent: a.name, persona, ready, state, why });
    }
    // Contacts of this machine whose agent is not detected now: kept, said as such.
    for (const p of personas) {
      if (p.machineId !== m.machineId || used.has(p.personaId)) continue;
      used.add(p.personaId);
      targets.push({ key: `p:${p.personaId}`, machineId: m.machineId, agent: p.agent, persona: p, ready: false, why: reachable ? "missing" : "offline" });
    }
    if (targets.length) groups.push({ machine: m, targets });
  }
  return groups;
}

/** The contacts on machines that are no longer there. */
export function orphanPersonas(machines: readonly Machine[], personas: readonly Persona[]): Persona[] {
  return personas.filter((p) => !machines.some((m) => m.machineId === p.machineId));
}
