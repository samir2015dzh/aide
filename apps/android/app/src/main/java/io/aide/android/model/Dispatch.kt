package io.aide.android.model

import io.aide.android.api.Machine
import io.aide.android.api.Persona

// Handing work out (工单 #41), as the desktop's dispatch.ts: every coding agent the machines detected, grouped by
// machine, with no contact to make first; a contact is only a custom face for one of them, on the same line.

enum class NotReady { OFFLINE, SIGNED_OUT, PAUSED, MISSING, UNKNOWN, CHECKING }

/**
 * ready: work can be sent to it now — its state is ready, or not confirmed (unknown, checking) while its machine
 * answers (工单 #56); state: its one state, as the roster and the machine page say it (null: a contact whose agent is
 * not there now).
 */
data class Target(val key: String, val machineId: String, val agent: String, val persona: Persona?, val why: NotReady?, val state: BotState? = null) {
    val ready: Boolean get() = why == null || why == NotReady.UNKNOWN || why == NotReady.CHECKING
}

data class TargetGroup(val machine: Machine, val targets: List<Target>)

fun dispatchTargets(machines: List<Machine>, personas: List<Persona>, now: java.time.Instant = java.time.Instant.now()): List<TargetGroup> {
    val used = mutableSetOf<String>()
    val sorted = machines.sortedWith(compareByDescending<Machine> { it.local }.thenByDescending { it.online }.thenBy { it.machineName })
    return sorted.mapNotNull { m ->
        val reachable = m.online || m.local
        val targets = mutableListOf<Target>()
        for (a in m.status?.agents.orEmpty()) {
            if (!a.available || a.hidden) continue
            val p = personas.firstOrNull { it.machineId == m.machineId && it.agent == a.name && it.personaId !in used }
            p?.let { used += it.personaId }
            // The one state of it (工单 #56): an unknown one is never passed off as ready, nor refused for want of a check.
            val state = codingBotState(m, a.name, now)
            val why = when {
                !reachable -> NotReady.OFFLINE
                state != null -> state.why
                !a.loggedIn -> NotReady.SIGNED_OUT
                a.pause != null -> NotReady.PAUSED
                else -> null
            }
            targets += Target("${m.machineId}/${a.name}", m.machineId, a.name, p, why, state)
        }
        for (p in personas) {
            if (p.machineId != m.machineId || p.personaId in used) continue
            used += p.personaId
            targets += Target("p:${p.personaId}", m.machineId, p.agent, p, if (reachable) NotReady.MISSING else NotReady.OFFLINE)
        }
        if (targets.isEmpty()) null else TargetGroup(m, targets)
    }
}
