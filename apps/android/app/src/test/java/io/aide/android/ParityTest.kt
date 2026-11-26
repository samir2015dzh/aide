package io.aide.android

import io.aide.android.api.AgentPause
import io.aide.android.api.AgentStatus
import io.aide.android.api.Client
import io.aide.android.api.DSHBridge
import io.aide.android.api.Machine
import io.aide.android.api.MachineStatus
import io.aide.android.api.RoomDelivery
import io.aide.android.api.RoomMessage
import io.aide.android.api.Session
import io.aide.android.api.SessionStatus
import io.aide.android.model.BotConversations
import io.aide.android.model.ConnRow
import io.aide.android.model.ConnState
import io.aide.android.model.Dot
import io.aide.android.model.DotHue
import io.aide.android.model.Presence
import io.aide.android.model.codingBotState
import io.aide.android.model.convCountHint
import io.aide.android.model.machineConnections
import io.aide.android.model.personalBotState
import io.aide.android.model.presenceDot
import io.aide.android.model.reachedNobody
import io.aide.android.model.resendable
import io.aide.android.model.rowPresence
import io.aide.android.model.timeLines
import io.aide.android.model.turnTimeLine
import io.aide.android.ui.Tone
import io.aide.android.ui.presenceTone
import java.time.Instant
import java.time.ZoneOffset
import java.util.Locale
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

/**
 * 工单 #145 第 5 条：安卓端补齐 #135 P02/P25/P33 与 #132 P28，与桌面端同口径 —— 状态点颜色与文字一一对应且各页同源
 * （Presence）、没送到任何人的消息标失败可重发、对话计数与列表对齐、时间线的时间分隔行。
 */
class ParityTest {
    private val now = Instant.parse("2026-11-26T12:00:00Z")
    private val fresh = "2026-11-26T11:58:00Z"

    private val was = Locale.getDefault()

    @Before fun zh() = Locale.setDefault(Locale.SIMPLIFIED_CHINESE)

    @After fun back() = Locale.setDefault(was)

    // P25: one dot per meaning, one-to-one.

    @Test
    fun eachPresenceHasItsOwnDotAndTone() {
        assertEquals(Dot(DotHue.GREEN), presenceDot(Presence.ONLINE))
        assertEquals(Dot(DotHue.BLUE), presenceDot(Presence.SIGNED_IN))
        assertEquals(Dot(DotHue.ORANGE), presenceDot(Presence.UNCONFIRMED))
        assertEquals(Dot(DotHue.RED), presenceDot(Presence.SIGNED_OUT))
        assertEquals(Dot(DotHue.BLUE, ring = true), presenceDot(Presence.CHECKING))
        assertEquals(Dot(DotHue.GREY), presenceDot(Presence.UNKNOWN))
        // No two meanings drawn alike.
        assertEquals(Presence.entries.size, Presence.entries.map { presenceDot(it) }.toSet().size)
        assertEquals(Presence.entries.size, Presence.entries.map { presenceTone(it) to presenceDot(it).ring }.toSet().size)
        assertEquals(Tone.Warn, presenceTone(Presence.UNCONFIRMED))
        assertEquals(Tone.Live, presenceTone(Presence.SIGNED_IN))
        assertEquals(Tone.Good, presenceTone(Presence.ONLINE))
    }

    @Test
    fun aRowsDotIsItsOwnPresenceElseItsState() {
        fun row(s: ConnState, p: Presence? = null) = ConnRow("x", "x", "X", s, "", presence = p)
        assertEquals(Presence.SIGNED_IN, rowPresence(row(ConnState.OK)))
        assertEquals(Presence.UNCONFIRMED, rowPresence(row(ConnState.PARTIAL)))
        assertEquals(Presence.SIGNED_OUT, rowPresence(row(ConnState.MISSING)))
        assertEquals(Presence.CHECKING, rowPresence(row(ConnState.CHECKING)))
        assertEquals(Presence.UNKNOWN, rowPresence(row(ConnState.UNKNOWN)))
        assertEquals(Presence.UNCONFIRMED, rowPresence(row(ConnState.UNKNOWN, Presence.UNCONFIRMED)))
        assertEquals(Presence.SIGNED_IN, rowPresence(row(ConnState.PARTIAL, Presence.SIGNED_IN)))
    }

    private fun machine(vararg agents: AgentStatus, online: Boolean = true) = Machine("m1", "work-pc", online = online, status = MachineStatus(agents = agents.toList()))
    private val bridgeOff = DSHBridge("DSH_BRIDGE_STATE_NOT_RUNNING", version = "0.3.1")
    private val bridgeOn = DSHBridge("DSH_BRIDGE_STATE_RUNNING", version = "0.3.1", latestVersion = "0.3.1")

    @Test
    fun theSameWordIsTheSameDotForEveryAgent() {
        // 审计 B：同为「未确认」，DSH 黄点、ACP 灰点；同为「已登录」，各页颜色不一。
        val dshUnconfirmed = codingBotState(machine(AgentStatus("deepseek", available = true, loginUnconfirmed = true, checkedAt = fresh, dshBridge = bridgeOn)), "deepseek", now)!!
        val acpUnconfirmed = codingBotState(machine(AgentStatus("gemini", available = true, loginUnconfirmed = true, checkedAt = fresh)), "gemini", now)!!
        val cliUnconfirmed = codingBotState(machine(AgentStatus("claude", available = true, loginUnconfirmed = true, checkedAt = fresh)), "claude", now)!!
        listOf(dshUnconfirmed, acpUnconfirmed, cliUnconfirmed).forEach {
            assertEquals("未确认", it.word)
            assertEquals(Presence.UNCONFIRMED, it.presence)
        }
        // DSH signed in with its plugin not running is PARTIAL by state, yet "已登录" is signed in, as Claude Code's.
        val dshIn = codingBotState(machine(AgentStatus("deepseek", available = true, loggedIn = true, version = "1.4.0", checkedAt = fresh, dshBridge = bridgeOff)), "deepseek", now)!!
        val cliIn = codingBotState(machine(AgentStatus("claude", available = true, loggedIn = true, version = "2.1.330", checkedAt = fresh)), "claude", now)!!
        assertEquals(ConnState.PARTIAL, dshIn.state)
        listOf(dshIn, cliIn).forEach {
            assertEquals("已登录", it.word)
            assertEquals(Presence.SIGNED_IN, it.presence)
        }
        // The devices tab's rows say the same as the roster's bot.
        val rows = machineConnections(machine(AgentStatus("deepseek", available = true, loggedIn = true, checkedAt = fresh, dshBridge = bridgeOff)), now).rows
        assertEquals(Presence.SIGNED_IN, rowPresence(rows.first { it.id == "deepseek" }))
        assertEquals(Presence.UNCONFIRMED, rowPresence(rows.first { it.id == "deepseek-bridge" }))
        val signedOut = codingBotState(machine(AgentStatus("claude", available = true, checkedAt = fresh)), "claude", now)!!
        assertEquals("未登录", signedOut.word)
        assertEquals(Presence.SIGNED_OUT, signedOut.presence)
    }

    @Test
    fun notKnownNowIsGreyWhateverItSaidBefore() {
        val stale = codingBotState(machine(AgentStatus("deepseek", available = true, loginUnconfirmed = true, checkedAt = "2026-11-26T10:00:00Z")), "deepseek", now)!!
        assertEquals(Presence.UNKNOWN, stale.presence)
        val away = codingBotState(machine(AgentStatus("claude", available = true, loggedIn = true, checkedAt = fresh), online = false), "claude", now)!!
        assertEquals(Presence.UNKNOWN, away.presence)
        val paused = codingBotState(machine(AgentStatus("claude", available = true, loggedIn = true, checkedAt = fresh, pause = AgentPause(since = fresh))), "claude", now)!!
        assertEquals("已暂停", paused.word)
        assertEquals(Presence.UNKNOWN, paused.presence)
    }

    @Test
    fun aPersonalAgentIsOnlineOrNotKnown() {
        assertEquals(Presence.ONLINE, personalBotState(Client(clientId = "c1", label = "Muse", online = true), now).presence)
        assertEquals(Presence.ONLINE, personalBotState(Client(clientId = "c1", label = "Muse", online = true, recent = true, lastSeen = fresh), now).presence)
        assertEquals(Presence.UNKNOWN, personalBotState(Client(clientId = "c1", label = "Muse", lastSeen = fresh), now).presence)
    }

    @Test
    fun theDotIsNotReadFromTheWords() {
        // The same agents in English: the same dots, though every word differs.
        Locale.setDefault(Locale.ENGLISH)
        val dsh = codingBotState(machine(AgentStatus("deepseek", available = true, loginUnconfirmed = true, checkedAt = fresh, dshBridge = bridgeOn)), "deepseek", now)!!
        val acp = codingBotState(machine(AgentStatus("gemini", available = true, loginUnconfirmed = true, checkedAt = fresh)), "gemini", now)!!
        assertEquals("Unconfirmed", dsh.word)
        assertEquals(Presence.UNCONFIRMED, dsh.presence)
        assertEquals(Presence.UNCONFIRMED, acp.presence)
    }

    // P02: the owner's message that reached nobody.

    private fun own(vararg states: String) = RoomMessage(messageId = "m", from = "owner", text = "跑一遍检查", deliveries = states.mapIndexed { i, s -> RoomDelivery("b$i", s, reason = "没送到") })

    @Test
    fun reachedNobodyIsFailedAndAFailedOneIsSentAgain() {
        val failed = "ROOM_DELIVERY_STATE_FAILED"
        val refused = "ROOM_DELIVERY_STATE_REFUSED"
        val replied = "ROOM_DELIVERY_STATE_REPLIED"
        assertTrue(reachedNobody(own(failed)))
        assertTrue(resendable(own(failed)))
        assertTrue(reachedNobody(own(failed, refused)))
        assertTrue(resendable(own(failed, refused)))
        // Refused by all: failed, but sending it again would be refused again.
        assertTrue(reachedNobody(own(refused)))
        assertFalse(resendable(own(refused)))
        // Reached someone: not failed, though the one it failed for can have it again.
        assertFalse(reachedNobody(own(failed, replied)))
        assertTrue(resendable(own(failed, replied)))
        assertFalse(reachedNobody(own(replied)))
        assertFalse(reachedNobody(own()))
        assertFalse(resendable(own("ROOM_DELIVERY_STATE_SENT")))
        // Not the owner's, or a read-only shadow conversation: never.
        assertFalse(reachedNobody(own(failed).copy(from = "c:c1")))
        assertFalse(reachedNobody(own(failed), shadow = true))
        assertFalse(resendable(own(failed), shadow = true))
    }

    // P28: time lines, as the session page's.

    @Test
    fun aTimeLineForTheFirstAndAfterAPause() {
        val utc = ZoneOffset.UTC
        fun at(s: String) = Instant.parse(s)
        assertEquals("今天 09:05", turnTimeLine(at("2026-11-26T09:05:00Z"), null, now, utc))
        assertNull(turnTimeLine(at("2026-11-26T09:05:00Z"), at("2026-11-26T08:50:00Z"), now, utc))
        assertEquals("今天 09:05", turnTimeLine(at("2026-11-26T09:05:00Z"), at("2026-11-26T08:20:00Z"), now, utc))
        assertEquals("昨天 23:10", turnTimeLine(at("2026-11-25T23:10:00Z"), null, now, utc))
        assertEquals("周一 14:30", turnTimeLine(at("2026-11-23T14:30:00Z"), null, now, utc))
        assertEquals("10月3日 14:30", turnTimeLine(at("2026-10-03T14:30:00Z"), null, now, utc))
        Locale.setDefault(Locale.ENGLISH)
        assertEquals("Today 09:05", turnTimeLine(at("2026-11-26T09:05:00Z"), null, now, utc))
        assertEquals("Yesterday 23:10", turnTimeLine(at("2026-11-25T23:10:00Z"), null, now, utc))
        assertEquals("Mon 14:30", turnTimeLine(at("2026-11-23T14:30:00Z"), null, now, utc))
        assertEquals("Oct 3 14:30", turnTimeLine(at("2026-10-03T14:30:00Z"), null, now, utc))
    }

    @Test
    fun aTimelinesLinesSkipTheHostsOwn() {
        fun msg(id: String, at: String, system: Boolean = false) = RoomMessage(messageId = id, from = "owner", text = id, at = at, system = system)
        val lines = timeLines(
            listOf(
                msg("a", "2026-11-25T20:00:00Z"),
                msg("b", "2026-11-25T20:10:00Z"),
                msg("sys", "2026-11-26T08:00:00Z", system = true),
                msg("c", "2026-11-26T09:00:00Z"),
                msg("d", ""),
                msg("e", "2026-11-26T09:20:00Z"),
            ),
            now, ZoneOffset.UTC,
        )
        assertEquals(mapOf("a" to "昨天 20:00", "c" to "今天 09:00"), lines)
    }

    // P33: the count and the list say the same.

    @Test
    fun theCountSaysWhatIsShownOfAll() {
        fun s(id: String, status: String = SessionStatus.IDLE) = Session(sessionId = id, status = status)
        assertNull(convCountHint(BotConversations(listOf(s("1")), emptyList(), emptyList()), 1))
        assertNull(convCountHint(BotConversations(emptyList(), emptyList(), emptyList()), 0))
        assertEquals("当前展示 1 条，共 2 条（1 条未归档 · 1 条已归档）", convCountHint(BotConversations(listOf(s("1")), listOf(s("2", SessionStatus.ARCHIVED)), emptyList()), 1))
        assertEquals("当前展示 4 条，共 4 条（2 条未归档 · 1 条已归档 · 1 条由其它 agent 派发）", convCountHint(BotConversations(listOf(s("1"), s("2")), listOf(s("3", SessionStatus.ARCHIVED)), listOf(s("4"))), 4))
        Locale.setDefault(Locale.ENGLISH)
        assertEquals("Showing 1 of 2 (1 open · 1 archived)", convCountHint(BotConversations(listOf(s("1")), listOf(s("2", SessionStatus.ARCHIVED)), emptyList()), 1))
    }
}
