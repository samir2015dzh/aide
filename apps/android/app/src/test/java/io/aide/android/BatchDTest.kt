package io.aide.android

import io.aide.android.api.AgentLogin
import io.aide.android.api.AgentPause
import io.aide.android.api.AgentStatus
import io.aide.android.api.ApiError
import io.aide.android.api.ClaudeDesktop
import io.aide.android.api.DSHBridge
import io.aide.android.api.Machine
import io.aide.android.api.MachineStatus
import io.aide.android.api.Persona
import io.aide.android.model.BridgeView
import io.aide.android.model.ConnState
import io.aide.android.model.NotReady
import io.aide.android.model.bridgeView
import io.aide.android.model.humanError
import io.aide.android.model.machineConnections
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import java.time.Instant
import java.util.Locale

/** 批 D on the phone: the same connections, hand-work list and plain errors as the desktop. */
class BatchDTest {
    private val now = Instant.parse("2026-11-22T10:00:00Z")
    private val checked = "2026-11-22T09:58:00Z"

    @Before
    fun english() = Locale.setDefault(Locale.ENGLISH)

    private fun machine(vararg agents: AgentStatus, online: Boolean = true) =
        Machine("m1", "work-pc", online = online, status = MachineStatus(agents = agents.toList()))

    @Test
    fun claudeDesktopAndTheCommandLineAreTwoRows() {
        val c = machineConnections(machine(AgentStatus("claude", available = true, loggedIn = false, checkedAt = checked, desktop = ClaudeDesktop(installed = true, signedIn = true))), now)
        assertEquals(listOf("claude-desktop" to ConnState.OK, "claude-cli" to ConnState.MISSING), c.rows.map { it.id to it.state })
        assertTrue(c.rows[1].lacks!!.contains("keeps its own sign-in"))
    }

    @Test
    fun dshAndItsPluginInEachState() {
        fun rows(b: DSHBridge) = machineConnections(machine(AgentStatus("deepseek", available = true, loggedIn = true, version = "1.4.0", checkedAt = checked, dshBridge = b)), now).rows
        // Prepared or not is Aide's business (工单 #59): not installed either way, and DSH can be asked to install it.
        assertEquals("Not installed", rows(DSHBridge("DSH_BRIDGE_STATE_NOT_INSTALLED", latestVersion = "0.3.0"))[1].text)
        assertEquals("Not installed", rows(DSHBridge("DSH_BRIDGE_STATE_NOT_INSTALLED", packagePath = "/x.tgz"))[1].text)
        assertTrue(rows(DSHBridge("DSH_BRIDGE_STATE_NOT_INSTALLED"))[1].install)
        val ok = rows(DSHBridge("DSH_BRIDGE_STATE_RUNNING", version = "0.3.0", latestVersion = "0.3.0"))
        assertEquals(listOf(ConnState.OK, ConnState.OK), ok.map { it.state })
        assertEquals(BridgeView.OUTDATED, bridgeView(DSHBridge("DSH_BRIDGE_STATE_RUNNING", version = "0.2.9", latestVersion = "0.3.0")))
        assertEquals(ConnState.PARTIAL, rows(DSHBridge("DSH_BRIDGE_STATE_NOT_INSTALLED"))[0].state)
    }

    @Test
    fun aFindingIsNotPassedOffAsNow() {
        val off = machineConnections(machine(AgentStatus("codex", available = true, loggedIn = true, checkedAt = checked), online = false), now)
        assertEquals(ConnState.UNKNOWN, off.rows[0].state)
        val old = machineConnections(machine(AgentStatus("codex", available = true, loggedIn = true, checkedAt = "2026-11-22T08:00:00Z")), now)
        assertTrue(old.rows[0].lacks!!.startsWith("Last checked"))
    }

    @Test
    fun rawStatusesAreNeverShown() {
        assertTrue(humanError(ApiError("unimplemented", "contacts_unavailable: the server does not keep contacts yet; update it (unimplemented: 404 Not Found)")).startsWith("Contacts need a newer server"))
        assertFalse(humanError(ApiError("unimplemented", "404 Not Found")).contains("404"))
        assertFalse(humanError(ApiError("unknown", "HTTP 404")).contains("404"))
        assertEquals("machine is offline", humanError(ApiError("unavailable", "machine is offline")))
    }

    @Test
    fun signingClaudeCodeInFromAide() {
        fun row(login: AgentLogin?) = machineConnections(machine(AgentStatus("claude", available = true, loggedIn = false, checkedAt = checked, login = login)), now).rows.single { it.id == "claude-cli" }
        assertTrue(row(null).login)
        assertEquals(ConnState.CHECKING to "Signing in…", row(AgentLogin("AGENT_LOGIN_STATE_WAITING")).let { it.state to it.text })
        assertTrue(row(AgentLogin("AGENT_LOGIN_STATE_FAILED", "the browser was closed")).lacks!!.contains("the browser was closed"))
        assertTrue(row(AgentLogin("AGENT_LOGIN_STATE_TIMED_OUT")).lacks!!.contains("10 minutes"))
    }
}
