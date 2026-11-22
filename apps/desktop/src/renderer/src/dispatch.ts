import type { Persona } from "@aide/proto/identity";
import type { Machine } from "@aide/proto/local";
import type { AgentStatus } from "@aide/proto/machine";

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
  /** Detected, signed in, not paused, its machine online: work can go to it now. */
  ready: boolean;
  /** Why not, when it is not: "offline", "signedOut", "paused", "missing". */
  why?: "offline" | "signedOut" | "paused" | "missing";
};

export type TargetGroup = { machine: Machine; targets: Target[] };

/** Detected: found and working, not hidden (an agent the machine is unsure of is not offered as there). */
function detected(a: AgentStatus): boolean {
  return a.available && !a.hidden;
}

export function dispatchTargets(machines: readonly Machine[], personas: readonly Persona[]): TargetGroup[] {
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
      const why = !reachable ? "offline" : !a.loggedIn ? "signedOut" : a.pause ? "paused" : undefined;
      targets.push({ key: `${m.machineId}/${a.name}`, machineId: m.machineId, agent: a.name, persona, ready: !why, why });
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
