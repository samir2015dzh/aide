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
        // work-pc checked 40 minutes ago: the roster, Hand Work To and the counts all say unknown, never green.
        val machines = listOf(machine("m1", "work-pc", claude(old), dsh(old)), machine("m2", "laptop", claude(), dsh(old)))
        val r = roster(machines, emptyList(), emptyList(), now)
        val workClaude = r.machines.first { it.machine.machineName == "work-pc" }.bots.first { it.agent == "claude" }
        assertEquals("状态未知", workClaude.state.word)
        assertEquals(Usable.UNVERIFIED, workClaude.state.usable)
        assertTrue(workClaude.state.line.contains("之后没再确认"))
        assertEquals(NotReady.UNKNOWN, workClaude.state.why)
        val c = botCounts(r.all)
        assertEquals(Triple(1, 3, 4), Triple(c.ready, c.unknown, c.total))
        assertEquals("可用 1 · 未知 3 · 共 4", c.text)
    }

    @Test
    fun pausedIsTheOwnersSayAndComesFirst() {
        val m = machine("m1", "work-pc", claude(old).copy(pause = AgentPause()))
        val s = codingBotState(m, "claude", now)!!
        assertEquals(Triple("已暂停", Usable.NO, NotReady.PAUSED), Triple(s.word, s.usable, s.why))
    }

    @Test
    fun machineAwayIsUnknownAndCantTakeWork() {
        val s = codingBotState(machine("m1", "work-pc", dsh(), online = false), "deepseek", now)!!
        assertEquals(ConnState.UNKNOWN, s.state)
        assertEquals(NotReady.OFFLINE, s.why)
        assertEquals(Usable.NO, s.usable)
    }

    @Test
    fun theRosterPersonalOnTheAccountCodingAgentsUnderTheirMachine() {
        val machines = listOf(machine("m1", "work-pc", claude(), dsh()), machine("m2", "laptop", claude()))
        val clients = listOf(Client(clientId = "c1", label = "Muse", online = true), Client(clientId = "c2", label = "DSH on work-pc"), Client(clientId = "c3", label = "Old", revoked = true))
        val sessions = listOf(Session(sessionId = "s1", machineId = "m1", agent = "claude", title = "Fix the CI", status = SessionStatus.RUNNING, updatedAt = fresh))
        val r = roster(machines, clients, sessions, now)
        val work = r.machines.first { it.machine.machineName == "work-pc" }
        assertEquals(listOf("Muse"), r.personal.map { it.name })
        assertEquals("在线", r.personal[0].state.word)
        assertEquals(listOf("Claude Code · work-pc", "DSH · work-pc"), work.bots.map { it.name })
        assertEquals("1 个进行中 · Fix the CI", work.bots[0].doing)
        // DSH's own grant is on DSH, not a second DSH.
        assertEquals("c2", work.bots[1].client?.clientId)
        assertEquals(2, work.bots.size)
    }

    @Test
    fun aContactIsItsBotsNameNotASecondEntry() {
        val r = roster(listOf(machine("m1", "work-pc", claude())), emptyList(), emptyList(), now, listOf(io.aide.android.api.Persona(personaId = "p1", name = "Reviewer", machineId = "m1", agent = "claude")))
        assertEquals(listOf("Reviewer"), r.machines[0].bots.map { it.name })
        assertEquals(1, r.machines[0].bots[0].personas.size)
    }

    @Test
    fun theInstallHandedToDSH() {
        fun row(b: io.aide.android.api.DSHBridge) = io.aide.android.model.machineConnections(machine("m1", "work-pc", dsh().copy(dshBridge = b)), now).rows.single { it.id == "deepseek-bridge" }
        val notInstalled = "DSH_BRIDGE_STATE_NOT_INSTALLED"
        val at = row(io.aide.android.api.DSHBridge(notInstalled, install = io.aide.android.api.DSHBridgeInstall("DSH_BRIDGE_INSTALL_STATE_RUNNING", sessionId = "s1")))
        assertEquals(Triple(ConnState.CHECKING, "DSH 正在安装…", "s1"), Triple(at.state, at.text, at.installSession))
        val failed = row(io.aide.android.api.DSHBridge(notInstalled, install = io.aide.android.api.DSHBridgeInstall("DSH_BRIDGE_INSTALL_STATE_FAILED", sessionId = "s1", reason = "别的插件的常驻连接器占着 uv.exe")))
        assertEquals(Triple("DSH 没装成功", "别的插件的常驻连接器占着 uv.exe", true), Triple(failed.text, failed.lacks, failed.install))
        val done = row(io.aide.android.api.DSHBridge("DSH_BRIDGE_STATE_RUNNING", version = "0.3.1", latestVersion = "0.3.1", install = io.aide.android.api.DSHBridgeInstall("DSH_BRIDGE_INSTALL_STATE_FAILED")))
        assertEquals(ConnState.OK, done.state)
    }
}
