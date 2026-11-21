package io.aide.android

import io.aide.android.api.AgentPause
import io.aide.android.api.AgentStatus
import io.aide.android.api.ConnectClient
import io.aide.android.api.ClaudeUsage
import io.aide.android.api.UsageWindow
import io.aide.android.api.WatchResponse
import io.aide.android.model.pausedUntil
import io.aide.android.model.usageResetAt
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import java.time.Instant

/** The same cases as the desktop's agentPause.test.ts. */
class AgentPauseTest {
    private val now = Instant.parse("2026-11-22T10:00:00Z")
    private fun claude(vararg w: UsageWindow) = AgentStatus(name = "claude", usage = ClaudeUsage(w.toList()))

    @Test fun latestResetOfTheWindowsUsedUp() {
        val a = claude(UsageWindow(utilization = 100.0, resetsAt = "2026-11-22T12:00:00Z"), UsageWindow(utilization = 100.0, resetsAt = "2026-11-26T07:00:00Z"), UsageWindow(utilization = 40.0, resetsAt = "2026-11-22T11:00:00Z"))
        assertEquals(Instant.parse("2026-11-26T07:00:00Z"), usageResetAt(a, now))
    }

    @Test fun noneUsedUpTheFullestWindowsReset() {
        val a = claude(UsageWindow(utilization = 30.0, resetsAt = "2026-11-22T11:00:00Z"), UsageWindow(utilization = 95.0, resetsAt = "2026-11-25T00:00:00Z"))
        assertEquals(Instant.parse("2026-11-25T00:00:00Z"), usageResetAt(a, now))
    }

    @Test fun noReadingNoTimeGuessed() {
        assertNull(usageResetAt(AgentStatus(name = "claude"), now))
        assertNull(usageResetAt(claude(UsageWindow(utilization = 100.0)), now))
        assertNull(usageResetAt(claude(UsageWindow(utilization = 100.0, resetsAt = "2026-11-22T09:00:00Z")), now))
    }

    @Test fun thePauseAsTheStatusSaysIt() {
        assertNull(pausedUntil(AgentStatus(name = "codex")))
        assertNull(pausedUntil(AgentStatus(name = "codex", pause = AgentPause(since = "2026-11-22T09:00:00Z"))))
        assertEquals(Instant.parse("2026-11-26T07:00:00Z"), pausedUntil(AgentStatus(name = "codex", pause = AgentPause(since = "2026-11-22T09:00:00Z", resumeAt = "2026-11-26T07:00:00Z"))))
        // As the core's Watch sends it.
        val w = ConnectClient.json.decodeFromString(
            WatchResponse.serializer(),
            """{"machines":[{"machineId":"m_1","status":{"agents":[{"name":"codex","available":true,"pause":{"since":"2026-11-22T09:00:00Z","resumeAt":"2026-11-26T07:00:00Z"},"autoResumedAt":"2026-11-21T07:00:00Z"}]}}]}""",
        )
        val a = w.machines[0].status!!.agents[0]
        assertEquals(Instant.parse("2026-11-26T07:00:00Z"), pausedUntil(a))
        assertEquals("2026-11-21T07:00:00Z", a.autoResumedAt)
    }
}
