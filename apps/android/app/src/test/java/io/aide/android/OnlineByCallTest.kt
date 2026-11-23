package io.aide.android

import io.aide.android.api.Client
import io.aide.android.model.ConnState
import io.aide.android.model.personalBotState
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test
import java.time.Instant
import java.util.Locale

/** An MCP connection is online by its last call (工单 #94), as on the desktop (core roster.test.ts). */
class OnlineByCallTest {
    @Before
    fun chinese() = Locale.setDefault(Locale.SIMPLIFIED_CHINESE)

    private val now = Instant.parse("2026-11-23T10:00:00Z")
    private fun called(min: Long, recent: Boolean) =
        personalBotState(Client(clientId = "c1", label = "Muse", online = recent, recent = recent, lastSeen = now.minusSeconds(min * 60).toString()), now)

    @Test
    fun onlineJustAfterACallRecentlyActiveForTheRestOfTheWindowThenOffline() {
        assertEquals("在线", called(1, true).word)
        called(5, true).let { assertEquals(ConnState.OK, it.state); assertEquals("最近活跃", it.word) }
        called(15, false).let { assertEquals(ConnState.UNKNOWN, it.state); assertEquals("不在线", it.word) }
    }

    @Test
    fun anAgentConnectedNowIsOnlineAsBefore() {
        val s = personalBotState(Client(clientId = "c2", label = "DSH on work-pc", online = true), now)
        assertEquals("在线", s.word)
        assertEquals("现在连着你的账号", s.line)
    }
}
