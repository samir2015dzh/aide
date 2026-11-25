package io.aide.android.model

import io.aide.android.api.ApiError
import io.aide.android.api.AttachmentIn
import kotlinx.serialization.Serializable
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import java.io.File
import java.time.Instant
import java.util.UUID

/**
 * Weak-network persistent outbound message queue (工单 #115).
 * Mirrors packages/core/src/outbox.ts so Android and Desktop share the exact
 * same per-session monotonic clientSeq, clientMessageId deduplication key,
 * status lifecycle (PENDING / SENDING / SENT / FAILED), and kill/relaunch recovery.
 */
@Serializable
enum class OutboxStatus { PENDING, SENDING, SENT, FAILED }

@Serializable
data class OutboxMessage(
    val id: String,
    val sessionId: String,
    val clientSeq: Long,
    val text: String,
    val attachments: List<AttachmentIn> = emptyList(),
    val status: OutboxStatus = OutboxStatus.PENDING,
    val error: String? = null,
    val createdAt: String = "",
    val updatedAt: String = "",
)

@Serializable
data class OutboxState(
    val version: Int = 1,
    val nextSeq: Map<String, Long> = emptyMap(),
    val items: List<OutboxMessage> = emptyList(),
)

object OutboxCodec {
    private val json = Json {
        ignoreUnknownKeys = true
        encodeDefaults = true
    }

    fun load(file: File): OutboxState {
        if (!file.exists()) return OutboxState()
        return runCatching {
            val decoded = json.decodeFromString(OutboxState.serializer(), file.readText())
            normalize(decoded)
        }.getOrDefault(OutboxState())
    }

    fun save(file: File, state: OutboxState) {
        runCatching {
            file.parentFile?.mkdirs()
            val tmp = File(file.parentFile, "${file.name}.tmp")
            tmp.writeText(json.encodeToString(state))
            if (!tmp.renameTo(file)) {
                file.writeText(json.encodeToString(state))
                tmp.delete()
            }
        }
    }

    fun normalize(state: OutboxState): OutboxState {
        val next = state.nextSeq.toMutableMap()
        for (it in state.items) {
            val want = it.clientSeq + 1L
            if ((next[it.sessionId] ?: 1L) < want) next[it.sessionId] = want
        }
        return state.copy(version = 1, nextSeq = next)
    }

    /** On app boot, reset any mid-flight SENDING item back to PENDING keeping its id for server dedup. */
    fun recoverOnBoot(state: OutboxState, now: String = Instant.now().toString()): OutboxState {
        var changed = false
        val items = state.items.map {
            if (it.status == OutboxStatus.SENDING) {
                changed = true
                it.copy(status = OutboxStatus.PENDING, updatedAt = now)
            } else it
        }
        return if (changed) state.copy(items = items) else state
    }

    fun enqueue(
        state: OutboxState,
        sessionId: String,
        text: String,
        attachments: List<AttachmentIn> = emptyList(),
        id: String = UUID.randomUUID().toString(),
        now: String = Instant.now().toString(),
    ): Pair<OutboxState, OutboxMessage> {
        val seq = state.nextSeq[sessionId] ?: 1L
        val item = OutboxMessage(
            id = id,
            sessionId = sessionId,
            clientSeq = seq,
            text = text,
            attachments = attachments,
            status = OutboxStatus.PENDING,
            createdAt = now,
            updatedAt = now,
        )
        val next = state.copy(
            nextSeq = state.nextSeq + (sessionId to (seq + 1L)),
            items = state.items + item,
        )
        return next to item
    }

    fun markSending(state: OutboxState, id: String, now: String = Instant.now().toString()): OutboxState =
        state.copy(items = state.items.map { if (it.id == id) it.copy(status = OutboxStatus.SENDING, error = null, updatedAt = now) else it })

    fun markSent(state: OutboxState, id: String, now: String = Instant.now().toString()): OutboxState =
        state.copy(items = state.items.map { if (it.id == id) it.copy(status = OutboxStatus.SENT, error = null, updatedAt = now) else it })

    fun markError(state: OutboxState, id: String, error: String, transient: Boolean, now: String = Instant.now().toString()): OutboxState {
        val st = if (transient) OutboxStatus.PENDING else OutboxStatus.FAILED
        return state.copy(items = state.items.map { if (it.id == id) it.copy(status = st, error = error, updatedAt = now) else it })
    }

    fun retry(state: OutboxState, id: String, now: String = Instant.now().toString()): OutboxState =
        state.copy(items = state.items.map {
            if (it.id == id && (it.status == OutboxStatus.FAILED || it.status == OutboxStatus.PENDING)) {
                it.copy(status = OutboxStatus.PENDING, error = null, updatedAt = now)
            } else it
        })

    fun remove(state: OutboxState, id: String): OutboxState =
        state.copy(items = state.items.filterNot { it.id == id })

    fun forSession(state: OutboxState, sessionId: String): List<OutboxMessage> =
        state.items.filter { it.sessionId == sessionId }.sortedBy { it.clientSeq }

    /** Strict FIFO head per session: later items wait behind an earlier SENDING or FAILED item. */
    fun nextReady(state: OutboxState, sessionId: String? = null): List<OutboxMessage> =
        state.items
            .asSequence()
            .filter { (sessionId == null || it.sessionId == sessionId) && it.status != OutboxStatus.SENT }
            .groupBy { it.sessionId }
            .values
            .mapNotNull { list -> list.minByOrNull { it.clientSeq }?.takeIf { it.status == OutboxStatus.PENDING } }

    /** Drops SENT entries once their confirmed User item has arrived in the session transcript. */
    fun reconcileWithTranscript(state: OutboxState, sessionId: String, items: List<Item>): OutboxState {
        val sent = state.items.filter { it.sessionId == sessionId && it.status == OutboxStatus.SENT }.sortedBy { it.clientSeq }
        if (sent.isEmpty()) return state
        val userTexts = items.filterIsInstance<Item.User>().map { it.text.trim() }
        val toDrop = mutableSetOf<String>()
        var from = 0
        for (m in sent) {
            val needle = m.text.trim()
            if (needle.isEmpty()) {
                if (userTexts.isNotEmpty()) toDrop += m.id
                continue
            }
            val idx = userTexts.drop(from).indexOf(needle)
            if (idx >= 0) {
                toDrop += m.id
                from += idx + 1
            }
        }
        if (toDrop.isEmpty()) return state
        return state.copy(items = state.items.filterNot { it.id in toDrop })
    }

    fun isTransientError(e: Throwable): Boolean {
        val code = (e as? ApiError)?.code?.lowercase().orEmpty()
        val msg = ((e as? ApiError)?.message ?: e.message.orEmpty()).lowercase()
        if (code in setOf("failed_precondition", "invalid_argument", "permission_denied", "not_found") ||
            msg.contains("archived") || msg.contains("permission") || msg.contains("not found") || msg.contains("invalid")
        ) {
            return false
        }
        return code in setOf("unavailable", "deadline_exceeded", "unknown", "canceled", "") ||
            msg.contains("offline") || msg.contains("network") || msg.contains("timeout") ||
            msg.contains("timed out") || msg.contains("unavailable") || msg.contains("connect") ||
            msg.contains("reset") || msg.contains("eof")
    }
}
