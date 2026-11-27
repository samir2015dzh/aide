package io.aide.android

import io.aide.android.api.AgentStatus
import io.aide.android.api.Machine
import io.aide.android.api.MachineStatus
import io.aide.android.model.ConnState
import io.aide.android.model.machineConnections
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import java.time.Instant
import java.util.Locale

/**
 * 工单 #175/#176: what the Machines tab says of an agent's check. A check with no answer is said apart from a program
 * that fails ("检查无回应", not "运行不了"); a found agent whose version its check could not read says "版本未知".
 */
class ProbeVerdictTest {
    private val now = Instant.now()

    @Before fun chinese() = Locale.setDefault(Locale.SIMPLIFIED_CHINESE)

    private fun row(a: AgentStatus) =
        machineConnections(Machine("m1", "workstation", online = true, status = MachineStatus(agents = listOf(a))), now).rows.single { it.id == a.name }

    @Test fun aCheckWithNoAnswerIsNotAProgramThatFails() {
        val silent = row(AgentStatus("antigravity", available = false, problem = "AGENT_PROBLEM_TIMED_OUT", checkedAt = now.toString()))
        assertEquals(ConnState.MISSING to "检查无回应", silent.state to silent.text)
        assertTrue(silent.lacks.orEmpty(), silent.lacks.orEmpty().contains("不一定是它坏了"))
        val failing = row(AgentStatus("antigravity", available = false, problem = "AGENT_PROBLEM_FAILED", detail = "initialize: it ended before answering", checkedAt = now.toString()))
        assertEquals("运行不了" to "initialize: it ended before answering", failing.text to failing.lacks)
    }

    @Test fun aFoundAgentWithoutItsVersionSaysItIsUnknown() {
        val cli = machineConnections(Machine("m1", "workstation", online = true, status = MachineStatus(agents = listOf(
            AgentStatus("claude", available = true, loggedIn = true, checkedAt = now.toString()),
        ))), now).rows.single { it.id == "claude-cli" }
        assertEquals(ConnState.OK to "已登录 · 版本未知", cli.state to cli.text)
        assertEquals("可以用 · 版本未知", row(AgentStatus("antigravity", available = true, loggedIn = true, checkedAt = now.toString())).text)
        assertEquals("可以用 · 1.2.3", row(AgentStatus("antigravity", available = true, loggedIn = true, version = "1.2.3", checkedAt = now.toString())).text)
    }
}
