package io.aide.android

import io.aide.android.api.AgentPause
import io.aide.android.api.AgentStatus
import io.aide.android.api.Client
import io.aide.android.api.Machine
import io.aide.android.api.MachineStatus
import io.aide.android.api.Session
import io.aide.android.api.SessionStatus
import io.aide.android.model.ConnState
import io.aide.android.model.NotReady
import io.aide.android.model.Usable
import io.aide.android.model.botCounts
import io.aide.android.model.codingBotState
import io.aide.android.model.dispatchTargets
import io.aide.android.model.roster
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import java.time.Instant
import java.util.Locale

/** 批 G: one state per agent on every page (工单 #56), and the roster (工单 #57). */
class BatchGTest {
    private val now = Instant.parse("2026-11-22T10:00:00Z")
    private val fresh = "2026-11-22T09:58:00Z"
    private val old = "2026-11-22T09:20:00Z"

    @Before
    fun chinese() = Locale.setDefault(Locale.SIMPLIFIED_CHINESE)

    private fun machine(id: String, name: String, vararg agents: AgentStatus, online: Boolean = true) =
        Machine(id, name, online = online, status = MachineStatus(agents = agents.toList()))

    private fun claude(checked: String = fresh) = AgentStatus("claude", available = true, loggedIn = true, version = "2.1.330", checkedAt = checked)
    private fun dsh(checked: String = fresh) = AgentStatus("deepseek", available = true, loggedIn = true, version = "1.4.0", checkedAt = checked)

    @Test
    fun theOwnersScreenshotOneStateOnEveryPage() {
        // alexwork checked 40 minutes ago: the roster, Hand Work To and the counts all say unknown, never green.
        val machines = listOf(machine("m1", "alexwork", claude(old), dsh(old)), machine("m2", "laptop", claude(), dsh(old)))
        val r = roster(machines, emptyList(), emptyList(), now)
        val alexClaude = r.machines.first { it.machine.machineName == "alexwork" }.bots.first { it.agent == "claude" }
        assertEquals("状态未知", alexClaude.state.word)
        assertEquals(Usable.UNVERIFIED, alexClaude.state.usable)
        assertTrue(alexClaude.state.line.contains("之后没再确认"))
        val target = dispatchTargets(machines, emptyList(), now).first { it.machine.machineName == "alexwork" }.targets.first { it.agent == "claude" }
        assertEquals(NotReady.UNKNOWN, target.why)
        assertTrue(target.ready)
        assertEquals(alexClaude.state, target.state)
        val c = botCounts(r.all)
        assertEquals(Triple(1, 3, 4), Triple(c.ready, c.unknown, c.total))
        assertEquals("可用 1 · 未知 3 · 共 4", c.text)
    }

    @Test
    fun pausedIsTheOwnersSayAndComesFirst() {
        val m = machine("m1", "alexwork", claude(old).copy(pause = AgentPause()))
        val s = codingBotState(m, "claude", now)!!
        assertEquals(Triple("已暂停", Usable.NO, NotReady.PAUSED), Triple(s.word, s.usable, s.why))
    }

    @Test
    fun machineAwayIsUnknownAndCantTakeWork() {
        val s = codingBotState(machine("m1", "alexwork", dsh(), online = false), "deepseek", now)!!
        assertEquals(ConnState.UNKNOWN, s.state)
        assertEquals(NotReady.OFFLINE, s.why)
        assertEquals(Usable.NO, s.usable)
    }

    @Test
    fun theRosterPersonalOnTheAccountCodingAgentsUnderTheirMachine() {
        val machines = listOf(machine("m1", "alexwork", claude(), dsh()), machine("m2", "laptop", claude()))
        val clients = listOf(Client(clientId = "c1", label = "Muse", online = true), Client(clientId = "c2", label = "DSH on alexwork"), Client(clientId = "c3", label = "Old", revoked = true))
        val sessions = listOf(Session(sessionId = "s1", machineId = "m1", agent = "claude", title = "Fix the CI", status = SessionStatus.RUNNING, updatedAt = fresh))
        val r = roster(machines, clients, sessions, now)
        assertEquals(listOf("Muse"), r.personal.map { it.name })
        assertEquals("在线", r.personal[0].state.word)
        assertEquals(listOf("Claude Code · alexwork", "DSH · alexwork"), r.machines[0].bots.map { it.name })
        assertEquals("1 个进行中 · Fix the CI", r.machines[0].bots[0].doing)
        // DSH's own grant is on DSH, not a second DSH.
        assertEquals("c2", r.machines[0].bots[1].client?.clientId)
        assertEquals(2, r.machines[0].bots.size)
    }
}
