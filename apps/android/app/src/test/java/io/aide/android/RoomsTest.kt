package io.aide.android

import io.aide.android.api.Room
import io.aide.android.push.NoticeTarget
import io.aide.android.push.roomNotices
import io.aide.android.push.show
import io.aide.android.ui.roomLine
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
}
