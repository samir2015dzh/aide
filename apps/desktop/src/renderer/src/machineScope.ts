import type { Machine } from "@aide/proto/local";

// Whose sessions are whose (the owner's rule, 2026-11-21): with more than one machine, every list of
// sessions is grouped by machine, every session's page says its machine where its title is, and every
// way to write to one (take over, message) names the machine it goes to. With one machine, none of it
// shows: there is nothing to tell apart. Worked out from the machines alone, so it can be checked
// without a window.

type M = Pick<Machine, "machineId" | "machineName" | "local">;

/** More than one machine: sessions need telling apart by machine. */
export const manyMachines = (machines: readonly M[]): boolean => machines.length > 1;

/** A machine's name as a session's tag shows it; one no longer listed by its id. */
export function machineTag(machines: readonly M[], id: string): { name: string; local: boolean } {
  const m = machines.find((x) => x.machineId === id);
  return { name: m?.machineName || id, local: !!m?.local };
}

export type MachineGroup<T> = { machineId: string; name: string; local: boolean; items: T[] };

/**
 * items in groups by machine: this computer's first, then by name, each keeping the items' order.
 * One group (with no header to show) while there is only one machine, or every item is of one.
 */
export function groupByMachine<T>(items: readonly T[], machineOf: (item: T) => string, machines: readonly M[]): MachineGroup<T>[] {
  const groups = new Map<string, MachineGroup<T>>();
  for (const it of items) {
    const id = machineOf(it);
    let g = groups.get(id);
    if (!g) {
      const tag = machineTag(machines, id);
      g = { machineId: id, name: tag.name, local: tag.local, items: [] };
      groups.set(id, g);
    }
    g.items.push(it);
  }
  return [...groups.values()].sort((a, b) => Number(b.local) - Number(a.local) || a.name.localeCompare(b.name));
}

/** Whether a list's groups show their machine: more than one machine, so the reader can't assume which. */
export const showGroups = (machines: readonly M[]): boolean => manyMachines(machines);
