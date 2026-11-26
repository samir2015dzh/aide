package io.aide.android

import io.aide.android.api.RoomMessage
import io.aide.android.ui.claimedText
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import java.util.Locale

/** 工单 #163: a shadow letter is put by the post office's time; its sender's own is said beside it only when it is off. */
class ShadowTimeTest {
    @Before fun chinese() = Locale.setDefault(Locale.SIMPLIFIED_CHINESE)

    @Test fun theSendersTimeIsSaidOnlyWhenItIsOff() {
        assertEquals(null, claimedText(RoomMessage(at = "2026-11-26T10:00:00Z", claimedAt = "2026-11-26T10:02:00Z")))
        assertEquals(null, claimedText(RoomMessage(at = "2026-11-26T10:00:00Z")))
        val off = claimedText(RoomMessage(at = "2026-11-26T10:00:00Z", claimedAt = "2026-11-26T12:00:00Z"))
        assertTrue(off, off!!.startsWith("对方自报 "))
    }
}
