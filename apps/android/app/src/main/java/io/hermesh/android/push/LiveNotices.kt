package io.hermesh.android.push

import io.hermesh.android.api.Machine
import io.hermesh.android.api.Notice
import io.hermesh.android.api.Session
import io.hermesh.android.api.SessionStatus
import io.hermesh.android.model.World
import io.hermesh.android.model.displayTitle
import io.hermesh.android.model.instant
import java.time.Instant

/**
 * Notices from the core's own connection (docs/android-app.md §4): what
 * changed in the owner's world, as Watch brings it, said as the notices a
 * machine seals for a phone (the kinds in noticeKinds, and requests to
 * connect), so they show as before (show, Push.notify). It follows the
 * desktop's rules (packages/core notices.ts):
 *
 * - A session seen for the first time tells about its question, and is new
 *   when it was created after the first look and not by the owner (the
 *   owner just did); one seen before tells about a new question or a new
 *   failure. Finished turns are not asked for.
 * - Her: an environment whose unread count grew with a newer message from
 *   an assistant that is not muted. An environment seen the first time is
 *   no news.
 * - Every pending request to connect, once.
 *
 * What is no longer pending (an answered question, a request answered
 * elsewhere or expired, Her read everywhere) is withdrawn.
 */
class LiveNotices {
    private data class Snapshot(val question: String, val failed: Boolean)

    /** What to show, and the threads (notificationId) to take back. */
    data class Changes(val show: List<Notice>, val withdraw: List<String>)

    private var seen = mapOf<String, Snapshot>()
    private var since: Instant? = null
    private var her: Map<String, HerSnapshot>? = null
    private var pairings = setOf<String>()
    private var asking = setOf<String>()
    private var herShown = setOf<String>()

    private data class HerSnapshot(val unread: Int, val latest: String)

    /** A request the person looked up here: no news. */
    fun known(pairingCode: String) {
        pairings = pairings + pairingCode
    }

    fun look(w: World, now: Instant = Instant.now()): Changes {
        val show = mutableListOf<Notice>()
        val withdraw = mutableListOf<String>()
        val status = w.status
        if (!status.initialized || status.removed) return Changes(show, withdraw)

        // Sessions.
        for (s in w.sessions.values) {
            val before = seen[s.sessionId]
            val q = pendingQuestion(s)
            if (before == null && since?.let { instant(s.createdAt) >= it } == true && s.createdBy != "owner") {
                show += notice(w, s, "session_started")
            }
            if (q != null && q.questionId != before?.question) {
                show += notice(w, s, "question").copy(question = q)
            }
            if (before != null && failed(s) && !before.failed) {
                show += notice(w, s, "failure").copy(failure = s.progress?.lastFailure?.kind.orEmpty(), failureMessage = s.progress?.lastFailure?.message.orEmpty())
            }
        }
        val nowAsking = w.sessions.values.filter { pendingQuestion(it) != null }.map { it.sessionId }.toSet()
        // A question answered here or elsewhere: its notification goes (a failure since replaced it).
        for (id in asking - nowAsking) if (show.none { it.sessionId == id }) withdraw += id
        asking = nowAsking
        seen = w.sessions.mapValues { (_, s) -> Snapshot(pendingQuestion(s)?.questionId.orEmpty(), failed(s)) }
        if (since == null) since = now

        // Her.
        val environments = w.machines.filter { it.her?.enabled == true }
        val before = her
        if (before != null) {
            for (m in environments) {
                val h = m.her!!
                val l = h.latest ?: continue
                val b = before[m.machineId] ?: continue
                if (l.muted || h.unread <= b.unread || latestKey(m) == b.latest) continue
                show += Notice(
                    kind = "her", machineId = m.machineId, machine = m.machineName, botId = l.botId,
                    botName = if (environments.size > 1) "${l.botName.ifEmpty { "Her" }} · ${m.machineName}" else l.botName,
                    text = l.text,
                    herKind = when (l.kind) {
                        "HER_LATEST_KIND_QUESTION" -> "question"
                        "HER_LATEST_KIND_APPROVAL" -> "approval"
                        else -> "message"
                    },
                )
                herShown = herShown + herThread(m.machineId, l.botId)
            }
            // Read everywhere: Her's notifications of that environment go.
            for (m in environments) {
                if (m.her!!.unread != 0) continue
                val gone = herShown.filter { it.startsWith("her:${m.machineId}:") }
                withdraw += gone
                herShown = herShown - gone.toSet()
            }
        }
        her = environments.associate { it.machineId to HerSnapshot(it.her!!.unread, latestKey(it)) }

        // Requests to connect: each once; taken back once gone.
        val pending = w.pairings.map { it.code }.toSet()
        for (p in w.pairings) {
            if (p.code in pairings) continue
            show += Notice(
                kind = "pairing", pairingKind = p.kind, pairingLabel = p.label,
                verificationCode = p.verificationCode.ifEmpty { p.code }, pairingCode = p.code,
            )
        }
        // They share one notification (show's thread "pairing"): it goes with the last.
        if (pairings.isNotEmpty() && pending.isEmpty()) withdraw += "pairing"
        pairings = pending
        return Changes(show, withdraw)
    }

    private fun notice(w: World, s: Session, kind: String) = Notice(
        kind = kind, sessionId = s.sessionId, title = displayTitle(s), agent = s.agent, project = s.projectId,
        machine = w.machineName(s.machineId),
        grant = w.clients.firstOrNull { it.clientId == s.createdBy }?.let { if (it.herMachineId.isNotEmpty()) "Her" else it.label }.orEmpty(),
    )

    private fun latestKey(m: Machine): String =
        m.her?.latest?.let { "${it.botId}\u0000${it.at}\u0000${it.text}" }.orEmpty()

    companion object {
        /** The thread a notice of Her's shows under (show). */
        fun herThread(machineId: String, botId: String) = "her:$machineId:$botId"

        /** The question a session waits on (as the desktop's pendingQuestion). */
        fun pendingQuestion(s: Session) = if (s.status == SessionStatus.WAITING_INPUT) s.progress?.pendingQuestion else null

        /** A session that failed (as the desktop's isFailed): crashed, or a turn's failure other than an interruption. */
        fun failed(s: Session): Boolean = s.status == SessionStatus.CRASHED || (
            s.status !in setOf(SessionStatus.WAITING_INPUT, SessionStatus.ARCHIVED, "", "SESSION_STATUS_UNSPECIFIED") &&
                s.progress?.lastFailure != null && s.progress.lastFailure.kind != "FAILURE_KIND_INTERRUPTED"
            )
    }
}
