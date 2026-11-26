package io.aide.android

import io.aide.android.api.Room
import io.aide.android.api.RoomMemberCheck
import io.aide.android.model.reachProblem
import io.aide.android.model.unreachableKey
import io.aide.android.push.NoticeTarget
import io.aide.android.push.roomNotices
import io.aide.android.push.show
import io.aide.android.ui.roomLine
import java.time.Instant
import java.util.Locale
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test

// Rooms on the phone (G2-b/c): a room's line as the desktop says it, and a room with more not read telling of it.
class RoomsTest {
    @Before fun zh() = Locale.setDefault(Locale.SIMPLIFIED_CHINESE)

    private val table = Room(roomId = "room_1", name = "发版评审", mode = "ROOM_MODE_ROUNDTABLE", maxRounds = 6)

    @Test
    fun aRoundIsAHandOnFromBotToBot() {
        assertEquals("圆桌开始 · 最多接力 6 轮", roomLine(table.copy(run = "ROOM_RUN_RUNNING")))
        assertEquals("圆桌接力中 · 第 2/6 轮", roomLine(table.copy(run = "ROOM_RUN_RUNNING", round = 2)))
        assertEquals("到达轮次上限，已停", roomLine(table.copy(run = "ROOM_RUN_STOPPED_LIMIT")))
        assertEquals("等 bot 回话", roomLine(table.copy(mode = "ROOM_MODE_MANUAL", run = "ROOM_RUN_RUNNING")))
        assertEquals("Muse：好", roomLine(table.copy(lastLine = "Muse：好")))
    }

    @Test
    fun moreNotReadTellsOfItUnlessItsPageIsOpen() {
        val before = listOf(table.copy(unread = 1))
        val now = listOf(table.copy(unread = 2, lastLine = "Muse：[FINAL] 可以发版"), table.copy(roomId = "room_2", name = "周报", unread = 0))
        val n = roomNotices(before, now, open = "")
        assertEquals(listOf("room_1"), n.map { it.roomId })
        val s = show(n[0])
        assertEquals(Triple("发版评审", "Muse：[FINAL] 可以发版", "room:room_1"), Triple(s.title, s.body, s.thread))
        assertEquals(NoticeTarget(room = "room_1"), s.target)
        assertEquals(emptyList<Any>(), roomNotices(before, now, open = "room_1"))
        assertEquals(emptyList<Any>(), roomNotices(now, now, open = ""))
    }

    // 工单 #159 第 1 条: a member checked and reachable says nothing; one that may not take its turn says why, as the
    // desktop does; the set the owner confirms is those, in any order.
    @Test
    fun aMemberThatMayNotTakeItsTurnSaysWhy() {
        val now = Instant.parse("2026-11-26T12:00:00Z")
        assertEquals("", reachProblem(RoomMemberCheck("c:c_ok"), now))
        assertEquals("可能接不了力：它的信箱还没开通（它那边的 Aide 多半要更新）", reachProblem(RoomMemberCheck("c:c_ghost", "ROOM_MEMBER_PROBLEM_NO_INBOX"), now))
        assertEquals("可能接不了力：信箱已开通，但现在没有东西替它取信（上次来是 2 小时前，也没有门铃叫醒它）",
            reachProblem(RoomMemberCheck("c:c_quiet", "ROOM_MEMBER_PROBLEM_NOT_TAKING", lastSeen = "2026-11-26T10:00:00Z"), now))
        assertEquals("可能接不了力：它在那台机器上的登录状态未确认", reachProblem(RoomMemberCheck("m:m_vps1/claude", "ROOM_MEMBER_PROBLEM_AGENT_UNCONFIRMED"), now))
        val a = RoomMemberCheck("c:c_quiet", "ROOM_MEMBER_PROBLEM_NOT_TAKING")
        val b = RoomMemberCheck("m:m_vps1/claude", "ROOM_MEMBER_PROBLEM_MACHINE_OFFLINE")
        assertEquals("", unreachableKey(listOf(RoomMemberCheck("c:c_ok")), now))
        assertEquals(unreachableKey(listOf(a, b), now), unreachableKey(listOf(b, RoomMemberCheck("c:c_ok"), a), now))
    }
}
