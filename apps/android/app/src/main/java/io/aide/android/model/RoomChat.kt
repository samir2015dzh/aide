package io.aide.android.model

import io.aide.android.api.RoomMessage
import io.aide.android.ui.t
import java.time.Duration
import java.time.Instant
import java.time.ZoneId

// A room's timeline and a personal agent's chat (one and the same, a direct room) as the desktop's RoomTimeline draws
// them: the owner's message that reached nobody marked failed and sent again from there (工单 #139 P02 / #145), and a
// time line before the first message and after a pause (the session page's TimeLine, 工单 #132 P28 / #145).

const val DELIVERY_FAILED = "ROOM_DELIVERY_STATE_FAILED"
const val DELIVERY_REFUSED = "ROOM_DELIVERY_STATE_REFUSED"

private fun ownersOwn(m: RoomMessage, shadow: Boolean) = !shadow && !m.system && !m.isFinal && m.from == "owner"

/** The owner's message reached nobody it was for: every delivery failed or was refused. */
fun reachedNobody(m: RoomMessage, shadow: Boolean = false): Boolean =
    ownersOwn(m, shadow) && m.deliveries.isNotEmpty() && m.deliveries.all { it.state == DELIVERY_FAILED || it.state == DELIVERY_REFUSED }

/** It can be sent again as it was (its text and whom it named): one of its deliveries failed (a refusal would again). */
fun resendable(m: RoomMessage, shadow: Boolean = false): Boolean = ownersOwn(m, shadow) && m.deliveries.any { it.state == DELIVERY_FAILED }

/** A pause this long between two messages puts a time line before the second. */
val TIME_LINE_PAUSE: Duration = Duration.ofMinutes(30)

private val weekdaysEn = listOf("Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun")
private val weekdaysZh = listOf("周一", "周二", "周三", "周四", "周五", "周六", "周日")
private val monthsEn = listOf("Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec")

/**
 * A message's time line, as the desktop's turnTimeLine: only for the first one and after a pause of half an hour —
 * "今天 18:22", "昨天 09:05", "周五 14:30", "10月3日 14:30"; null: none (the same day, without a pause).
 */
fun turnTimeLine(at: Instant, previous: Instant?, now: Instant, zone: ZoneId = ZoneId.systemDefault()): String? {
    if (previous != null && Duration.between(previous, at) < TIME_LINE_PAUSE) return null
    val d = at.atZone(zone)
    val day = d.toLocalDate()
    val today = now.atZone(zone).toLocalDate()
    val name = when {
        day == today -> t("Today", "今天")
        day == today.minusDays(1) -> t("Yesterday", "昨天")
        !day.isBefore(today.minusDays(6)) -> t(weekdaysEn[day.dayOfWeek.value - 1], weekdaysZh[day.dayOfWeek.value - 1])
        else -> t("${monthsEn[day.monthValue - 1]} ${day.dayOfMonth}", "${day.monthValue}月${day.dayOfMonth}日")
    }
    return "%s %02d:%02d".format(name, d.hour, d.minute)
}

/**
 * The time lines of a timeline, by message id, as the desktop's RoomTimeline puts them: the host's own lines have none
 * and do not count as the one before; a message without a time has none and leaves the one before as it was.
 */
fun timeLines(msgs: List<RoomMessage>, now: Instant, zone: ZoneId = ZoneId.systemDefault()): Map<String, String> {
    val out = mutableMapOf<String, String>()
    var prev: Instant? = null
    for (m in msgs) {
        if (m.system) continue
        val at = instantOrNull(m.at) ?: continue
        turnTimeLine(at, prev, now, zone)?.let { out[m.messageId] = it }
        prev = at
    }
    return out
}
