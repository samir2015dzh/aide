package io.aide.android

import io.aide.android.api.Client
import io.aide.android.model.NotReady
import io.aide.android.model.Presence
import io.aide.android.model.Usable
import io.aide.android.model.personalBotState
import io.aide.android.ui.deliveryWords
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import java.time.Instant
import java.util.Locale

/** Whether a message can go to it is part of its state (工单 #161), and a message not taken a day on says so (工单 #160), as on the desktop. */
class MailableTest {
    @Before
    fun chinese() = Locale.setDefault(Locale.SIMPLIFIED_CHINESE)

    private val now = Instant.parse("2026-11-26T10:00:00Z")
    private fun state(online: Boolean, mailable: Boolean?) = personalBotState(Client(clientId = "c1", label = "Bot", online = online, mailable = mailable), now)

    @Test
    fun onlineWithNoInboxIsNotUsable() {
        state(online = true, mailable = false).let {
            assertEquals("在线但收不了消息", it.word)
            assertEquals(Usable.NO, it.usable)
            assertEquals(NotReady.NO_INBOX, it.why)
            assertEquals(Presence.UNCONFIRMED, it.presence)
            assertTrue(it.line.contains("信箱还没开通"))
        }
        state(online = false, mailable = false).let {
            assertEquals("收不了消息", it.word)
            assertEquals(Usable.NO, it.usable)
        }
    }

    @Test
    fun withItsInboxOrFromAnOlderRelayAsBefore() {
        assertEquals(Usable.YES, state(online = true, mailable = true).usable)
        assertEquals(Usable.YES, state(online = true, mailable = null).usable)
        assertEquals(Usable.UNVERIFIED, state(online = false, mailable = true).usable)
    }

    @Test
    fun notTakenADayOnMayNotArrive() {
        assertEquals("Bot 已一天没来取信，可能送不到" to true, deliveryWords("ROOM_DELIVERY_STATE_SENT", "Bot", "", reminded = true, untaken = true))
        assertEquals("已发出：等 Bot 取信" to false, deliveryWords("ROOM_DELIVERY_STATE_SENT", "Bot", "", reminded = false))
    }
}
