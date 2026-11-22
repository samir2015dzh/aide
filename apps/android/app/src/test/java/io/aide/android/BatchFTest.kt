package io.aide.android

import io.aide.android.api.AgentModel
import io.aide.android.api.AgentStatus
import io.aide.android.api.ClaudeDesktop
import io.aide.android.api.DSHBridge
import io.aide.android.api.Machine
import io.aide.android.api.MachineStatus
import io.aide.android.model.ConnState
import io.aide.android.model.agentTitle
import io.aide.android.model.machineAgents
import io.aide.android.model.needsCheck
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import java.time.Instant
import java.util.Locale

/** 批 F 工单 #53: one list of a machine's agents, each once, by one name, with one state. */
class BatchFTest {
    private val now = Instant.parse("2026-11-22T10:00:00Z")
    private val fresh = "2026-11-22T09:58:00Z"
    private val old = "2026-11-22T09:20:00Z"

    @Before
    fun english() = Locale.setDefault(Locale.ENGLISH)

    private fun machine(vararg agents: AgentStatus, online: Boolean = true) =
        Machine("m1", "workstation", online = online, status = MachineStatus(agents = agents.toList()))

    private val claude = AgentStatus("claude", available = true, loggedIn = true, version = "2.1.330", checkedAt = fresh, desktop = ClaudeDesktop(installed = true, signedIn = true))
    private val dsh = AgentStatus("deepseek", available = true, loggedIn = true, version = "1.4.0", checkedAt = fresh, models = listOf(AgentModel("deepseek-v4", "DeepSeek V4")), dshBridge = DSHBridge("DSH_BRIDGE_STATE_RUNNING", version = "0.3.1"))

    @Test
    fun eachAgentOnceByOneName() {
        val e = machineAgents(machine(claude, dsh), now)
        assertEquals(listOf("claude", "deepseek"), e.map { it.agent })
        assertEquals(listOf("Claude Code", "DSH"), e.map { it.title })
        // DSH is the tool's name everywhere; DeepSeek is only its account.
        assertEquals("DSH", agentTitle("deepseek"))
        assertEquals("Models: DeepSeek V4", e[1].account)
    }

    @Test
    fun claudesTwoRowsAreItsPartsAndItsStateIsTheCommandLines() {
        val c = machineAgents(machine(claude.copy(loggedIn = false)), now).single()
        assertEquals(ConnState.MISSING, c.state)
        assertEquals(listOf("claude-desktop", "claude-cli"), c.parts.map { it.id })
        assertTrue(c.login)
    }

    @Test
    fun theBridgeIsALineUnderDSH() {
        val d = machineAgents(machine(dsh), now).single()
        assertEquals(ConnState.OK, d.state)
        assertEquals(listOf("deepseek-bridge"), d.parts.map { it.id })
    }

    @Test
    fun anOldFindingIsOneUnknownStateNeverSignedInBesideIt() {
        val e = machineAgents(machine(claude.copy(checkedAt = old), dsh.copy(checkedAt = old)), now)
        assertTrue(e.all { it.state == ConnState.UNKNOWN })
        // What it said is said as the past, under it.
        assertTrue(e.flatMap { it.parts }.all { it.was != null })
        assertEquals("Last known: Models: DeepSeek V4", e[1].account)
    }

    @Test
    fun theMachinesPageAsksForANewCheckOnlyWhenOneIsDue() {
        assertFalse(needsCheck(machine(claude, dsh), now))
        assertTrue(needsCheck(machine(claude.copy(checkedAt = old), dsh), now))
        assertTrue(needsCheck(machine(claude.copy(checkedAt = "")), now))
        assertFalse(needsCheck(machine(claude.copy(checkedAt = old), online = false), now))
        assertFalse(needsCheck(machine(claude.copy(checkedAt = old, checking = true)), now))
    }
}
