package io.hermesh.android.model

import io.hermesh.android.api.GetStatusResponse
import io.hermesh.android.api.Machine
import io.hermesh.android.api.Session
import io.hermesh.android.api.SessionStatus
import io.hermesh.android.ui.t
import java.time.Duration
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter

// What every screen says of a session (docs/session-states.md): what it is doing (SessionState) and how fresh
// that is (SessionLink), never folded into one. The same table as internal/sessionstate (Go) and packages/core
// states.ts; StatesTest checks it against the cases they share (internal/sessionstate/testdata/cases.json).

enum class SessionState { UNKNOWN, QUEUED, RUNNING, WAITING_INPUT, WAITING_APPROVAL, DONE_UNSEEN, DONE, FAILED, INTERRUPTED, ARCHIVED }

enum class SessionLink { UNKNOWN, LIVE, STALLED, CHECKING, LOST, SYNCING }

data class StateLimits(val lostAfter: Duration = Duration.ofSeconds(30), val stallAfter: Duration = Duration.ofMinutes(5)) {
    companion object {
        /** The limits the core says, else the defaults. */
        fun of(status: GetStatusResponse?): StateLimits {
            val l = status?.stateLimits
            if (l == null || l.lostAfterSeconds == 0 || l.stallAfterSeconds == 0) return StateLimits()
            return StateLimits(Duration.ofSeconds(l.lostAfterSeconds.toLong()), Duration.ofSeconds(l.stallAfterSeconds.toLong()))
        }
    }
}

/** What is known of a session's machine; heardAt is set once it is not heard from. */
data class Liveness(
    val known: Boolean,
    val local: Boolean = false,
    val unreachable: String = "",
    val syncing: Boolean = false,
    val heardAt: Instant? = null,
    val unreachableSince: Instant? = null,
) {
    companion object {
        fun of(m: Machine?): Liveness =
            if (m == null) Liveness(known = false)
            else Liveness(true, m.local, m.unreachable, m.syncing, instantOrNull(m.heardAt), instantOrNull(m.unreachableSince))
    }
}

data class StateView(
    val state: SessionState,
    val link: SessionLink,
    val lastEventAt: Instant? = null,
    /** How long since its last event, while it runs. */
    val quiet: Duration = Duration.ZERO,
    val heardAt: Instant? = null,
    /** How long its machine has not been heard from, while it is not. */
    val silent: Duration = Duration.ZERO,
) {
    /** What is shown is current: the session may be acted on. */
    val live: Boolean get() = link == SessionLink.LIVE || link == SessionLink.STALLED
}

fun instantOrNull(ts: String): Instant? = if (ts.isEmpty()) null else runCatching { Instant.parse(ts) }.getOrNull()?.takeIf { it.isAfter(Instant.EPOCH) }

/** What a session is doing, from its status and progress alone. */
fun stateOf(s: Session): SessionState = when (s.status) {
    SessionStatus.QUEUED, SessionStatus.PREPARING -> SessionState.QUEUED
    SessionStatus.RUNNING -> SessionState.RUNNING
    SessionStatus.WAITING_INPUT -> SessionState.WAITING_INPUT
    SessionStatus.ARCHIVED -> SessionState.ARCHIVED
    SessionStatus.CRASHED -> SessionState.FAILED
    SessionStatus.IDLE -> when (s.progress?.lastFailure?.kind) {
        null -> SessionState.DONE
        "FAILURE_KIND_INTERRUPTED" -> SessionState.INTERRUPTED
        else -> SessionState.FAILED
    }
    else -> SessionState.UNKNOWN
}

/** When a session last recorded anything: its last event, else (an older machine) when it last changed. */
fun lastEventAt(s: Session): Instant? = instantOrNull(s.progress?.lastEventAt.orEmpty()) ?: instantOrNull(s.updatedAt)

private fun nonNegative(d: Duration): Duration = if (d.isNegative) Duration.ZERO else d

/** What is shown of a session at now (internal/sessionstate Of). */
fun sessionStateOf(s: Session, m: Liveness, limits: StateLimits, now: Instant): StateView {
    val state = stateOf(s)
    val last = lastEventAt(s)
    var link = SessionLink.LIVE
    var silent = Duration.ZERO
    if (!m.known) {
        link = SessionLink.LOST
        m.heardAt?.let { silent = nonNegative(Duration.between(it, now)) }
    } else if (!m.local) {
        var since = m.heardAt
        when {
            m.unreachable.isNotEmpty() -> {
                link = SessionLink.CHECKING
                if (since == null) since = m.unreachableSince
                if (since == null) link = SessionLink.LOST
            }
            m.syncing -> link = SessionLink.SYNCING
            since != null -> link = SessionLink.CHECKING // readable a moment ago, but no heartbeat
        }
        if (since != null) {
            silent = nonNegative(Duration.between(since, now))
            if (silent >= limits.lostAfter) link = SessionLink.LOST
        }
    }
    var quiet = Duration.ZERO
    if (state == SessionState.RUNNING && last != null) {
        quiet = nonNegative(Duration.between(last, now))
        if (link == SessionLink.LIVE && quiet >= limits.stallAfter) link = SessionLink.STALLED
    }
    return StateView(state, link, last, quiet, if (m.local) null else m.heardAt, silent)
}

fun stateTitle(st: SessionState): String = when (st) {
    SessionState.QUEUED -> t("Queued", "排队中")
    SessionState.RUNNING -> t("Running", "运行中")
    SessionState.WAITING_INPUT -> t("Waiting for you", "等你回答")
    SessionState.WAITING_APPROVAL -> t("Waiting for approval", "等你审批")
    SessionState.DONE_UNSEEN -> t("Done, not opened", "已完成未看")
    SessionState.DONE -> t("Done", "已完成")
    SessionState.FAILED -> t("Failed", "出错")
    SessionState.INTERRUPTED -> t("Interrupted", "已中断")
    SessionState.ARCHIVED -> t("Archived", "已归档")
    SessionState.UNKNOWN -> t("State unknown", "状态未知")
}

/** A duration as lists say it: "40 s", "12 min". */
fun durationText(d: Duration): String {
    val s = d.seconds
    return when {
        s < 60 -> t("$s s", "$s 秒")
        s < 3600 -> t("${s / 60} min", "${s / 60} 分钟")
        s < 48 * 3600 -> t("${s / 3600} h", "${s / 3600} 小时")
        else -> t("${s / 86400} d", "${s / 86400} 天")
    }
}

/** How long a running turn has been quiet, when past the stall limit: "quiet 12 min". */
fun quietText(v: StateView): String = t("quiet ${durationText(v.quiet)}", "${durationText(v.quiet)}无新事件")

/** The one line every screen shows: "Running · quiet 12 min", "Lost · last known Running · 7 min ago". */
fun stateLine(v: StateView): String {
    val st = stateTitle(v.state)
    return when (v.link) {
        SessionLink.STALLED -> "$st · ${quietText(v)}"
        SessionLink.CHECKING -> t("$st? · machine not answering", "$st？· 机器未应答")
        SessionLink.LOST -> if (v.silent.isZero) t("Lost · last known $st", "失联 · 最后已知 $st")
        else t("Lost · last known $st · ${durationText(v.silent)} ago", "失联 · 最后已知 $st · ${durationText(v.silent)}前")
        SessionLink.SYNCING -> t("$st · catching up", "$st · 正在同步")
        else -> st
    }
}

/** The last time a session's machine was out of touch while the session was there: "Out of touch 14:02–14:20". */
fun lostSpanText(m: Machine?, s: Session, zone: ZoneId = ZoneId.systemDefault()): String {
    val created = instantOrNull(s.createdAt) ?: Instant.EPOCH
    val last = m?.lostSpans?.lastOrNull { instantOrNull(it.from) != null && (instantOrNull(it.to)?.let { to -> to >= created } ?: true) } ?: return ""
    val hm = DateTimeFormatter.ofPattern("HH:mm").withZone(zone)
    val from = hm.format(instantOrNull(last.from))
    return instantOrNull(last.to)?.let { t("Out of touch $from–${hm.format(it)}", "曾失联 $from–${hm.format(it)}") } ?: t("Out of touch since $from", "失联自 $from")
}
