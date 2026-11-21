package io.aide.android

import io.aide.android.model.groupByMachine
import io.aide.android.model.manyMachines
import io.aide.android.model.onMachine
import org.junit.Assert.assertEquals
import org.junit.Test

/** The owner's rule (2026-11-21): with several machines, sessions are told apart by machine; with one, nothing shows. */
class MachineScopeTest {
    private data class S(val id: String, val m: String)

    private val names = mapOf("m1" to "work-pc", "m2" to "box")

    @Test fun onlySeveralMachinesNeedTellingApart() {
        assertEquals(false, manyMachines(0))
        assertEquals(false, manyMachines(1))
        assertEquals(true, manyMachines(2))
    }

    @Test fun sessionsGroupByMachineByNameKeepingTheirOrder() {
        val s = listOf(S("a", "m1"), S("b", "m2"), S("c", "m1"), S("d", "gone"))
        val g = groupByMachine(s, { it.m }, { names[it].orEmpty() })
        assertEquals(listOf("box", "gone", "work-pc"), g.map { it.name })
        assertEquals(listOf("b"), g[0].items.map { it.id })
        assertEquals(listOf("a", "c"), g[2].items.map { it.id })
        assertEquals(emptyList<Any>(), groupByMachine(emptyList<S>(), { it.m }, { it }))
    }

    @Test fun aPickedMachineShowsItsSessionsOnly() {
        val s = listOf(S("a", "m1"), S("b", "m2"), S("c", "m1"))
        assertEquals(listOf("a", "c"), onMachine(s, { it.m }, "m1", names.keys).map { it.id })
        assertEquals(listOf("a", "b", "c"), onMachine(s, { it.m }, null, names.keys).map { it.id })
        // The machine picked is gone (removed): every session, rather than an empty list that hides them.
        assertEquals(listOf("a", "b", "c"), onMachine(s, { it.m }, "m9", names.keys).map { it.id })
    }
}
