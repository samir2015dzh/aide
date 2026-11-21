package io.aide.android.model

// Whose sessions are whose (the owner's rule, 2026-11-21), as on the desktop (machineScope.ts): with more
// than one machine, every list of sessions is grouped by machine or filtered to one, every session's page
// says its machine where its title is, and every way to write to one names the machine it goes to. With
// one machine, none of it shows: there is nothing to tell apart.

/** More than one machine: sessions need telling apart by machine. */
fun manyMachines(machineCount: Int): Boolean = machineCount > 1

data class MachineGroup<T>(val machineId: String, val name: String, val items: List<T>)

/** items in groups by machine, by name, each keeping the items' order; a machine no longer known by its id. */
fun <T> groupByMachine(items: List<T>, machineOf: (T) -> String, nameOf: (String) -> String): List<MachineGroup<T>> {
    val groups = LinkedHashMap<String, MutableList<T>>()
    for (it in items) groups.getOrPut(machineOf(it)) { mutableListOf() }.add(it)
    return groups.map { (id, list) -> MachineGroup(id, nameOf(id).ifEmpty { id }, list) }.sortedBy { it.name.lowercase() }
}

/** items of one machine, or all of them when none is picked (or the one picked is gone from the machines). */
fun <T> onMachine(items: List<T>, machineOf: (T) -> String, picked: String?, machineIds: Collection<String>): List<T> =
    if (picked == null || picked !in machineIds) items else items.filter { machineOf(it) == picked }
