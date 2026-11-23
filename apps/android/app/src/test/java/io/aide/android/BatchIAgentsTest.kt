package io.aide.android

import io.aide.android.api.AgentStatus
import io.aide.android.api.ClaudeSession
import io.aide.android.api.ClaudeUsage
import io.aide.android.api.Machine
import io.aide.android.api.MachineStatus
import io.aide.android.api.UsageWindow
import io.aide.android.model.roster
import io.aide.android.ui.claudeStatusText
import io.aide.android.ui.readsOwn
import io.aide.android.ui.usageLine
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import java.time.Instant
import java.util.Locale

/** 批 I, group C: a coding agent's own sessions on its page (工单 #76), its kind the owner's say (#93), plan usage with when it was read (#86). */
class BatchIAgentsTest {
    private val now = Instant.parse("2026-11-23T10:00:00Z")
    private val fresh = "2026-11-23T09:58:00Z"

    @Before
    fun chinese() = Locale.setDefault(Locale.SIMPLIFIED_CHINESE)

    @Test
    fun aDetectedAgentFiledPersonalIsListedWithThePersonalAgents() {
        val m = Machine("m1", "work-pc", online = true, status = MachineStatus(agents = listOf(
            AgentStatus("claude", available = true, loggedIn = true, checkedAt = fresh, category = "personal"),
            AgentStatus("codex", available = true, loggedIn = true, checkedAt = fresh),
        )))
        val r = roster(listOf(m), emptyList(), emptyList(), now)
        assertEquals(listOf("m:m1/claude"), r.personal.map { it.key })
        assertTrue("still its machine's coding agent, its page the same", r.personal.first().coding)
        assertEquals(listOf("codex"), r.coding.map { it.agent })
    }

    @Test
    fun ownSessionsAreReadForDshAndClaudeAndSaidSoForTheOthers() {
        assertTrue(readsOwn("deepseek"))
        assertTrue(readsOwn("claude"))
        assertFalse(readsOwn("codex"))
        assertEquals("已完成", claudeStatusText(ClaudeSession(status = "completed")))
        assertEquals("等你处理", claudeStatusText(ClaudeSession(status = "blocked")))
    }

    @Test
    fun planUsageSaysWhenItWasReadAndKeepsTheLastReading() {
        val read = AgentStatus("claude", usage = ClaudeUsage(windows = listOf(UsageWindow("five_hour", 20.4), UsageWindow("seven_day", 59.0)), fetchedAt = "2026-11-23T09:57:00Z"))
        assertEquals("套餐 5h 20% · 周 59% · 3 分钟前读数", usageLine(read, now))
        assertEquals("套餐 5h 20% · 周 59% · 上次读数 · 3 分钟前", usageLine(read.copy(usage = read.usage!!.copy(stale = true)), now))
        assertEquals("套餐用量还没读到", usageLine(AgentStatus("claude", usage = ClaudeUsage(unavailable = "signed_out")), now))
    }
}
