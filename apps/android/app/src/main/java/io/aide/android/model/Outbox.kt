package io.aide.android.model

import io.aide.android.api.ApiError
import io.aide.android.api.AttachmentIn
import io.aide.android.api.SessionEvent
import kotlinx.serialization.Serializable
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import java.io.File
import java.io.IOException
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
    /**
     * The session's last event seq the phone held when it was enqueued: on a server that echoes no
     * client_message_id, only later events can be this message (reconcileWithEvents).
     */
    val afterSeq: Long? = null,
    /** A send was tried at least once: it may have reached the machine although no answer came back. */
    val attempted: Boolean = false,
    /** Its files were too large to keep across a restart, and the app restarted before it went. */
    val attachmentsLost: Boolean = false,
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

    /** At most this many base64 characters of attachments are kept on disk for the whole queue (about 1.5 MB of files). */
    const val PERSIST_MAX_ATTACHMENT_CHARS = 2 * 1024 * 1024

    /** Reads the queue; call it off the main thread. */
    fun load(file: File): OutboxState {
        if (!file.exists()) return OutboxState()
        return runCatching {
            val decoded = json.decodeFromString(OutboxState.serializer(), file.readText())
            settleLoaded(normalize(decoded))
        }.getOrDefault(OutboxState())
    }

    /**
     * What a persisted copy says once read: a delivered message keeps no file bytes; one not yet delivered
     * whose bytes were left out (forPersist) cannot go as it was, and waits for the person, said so.
     */
    fun settleLoaded(state: OutboxState): OutboxState = state.copy(items = state.items.map { m ->
        when {
            m.status == OutboxStatus.SENT -> m.copy(attachments = m.attachments.map { it.copy(data = "") })
            m.attachmentsLost -> m.copy(status = OutboxStatus.FAILED)
            else -> m
        }
    })

    /**
     * The copy of the queue to keep on disk: files of messages not yet delivered are kept oldest first while they
     * fit in maxChars; the others are left out of the copy (in memory only) and returned, for the page to say so.
     * One large message never stops the rest of the queue from being kept.
     */
    fun forPersist(state: OutboxState, maxChars: Int = PERSIST_MAX_ATTACHMENT_CHARS): Pair<OutboxState, Set<String>> {
        var left = maxChars.toLong()
        val drop = mutableSetOf<String>()
        for (m in state.items.sortedWith(compareBy<OutboxMessage>({ it.createdAt }, { it.clientSeq }))) {
            val n = m.attachments.sumOf { it.data.length.toLong() }
            if (n == 0L) continue
            if (n <= left) left -= n else drop += m.id
        }
        if (drop.isEmpty()) return state to drop
        return state.copy(items = state.items.map { m ->
            if (m.id in drop) m.copy(attachments = m.attachments.map { it.copy(data = "") }, attachmentsLost = true) else m
        }) to drop
    }

    /**
     * Writes the queue (call it off the main thread): files too large to keep stay in memory only; when even that
     * cannot be written, the queue without any file bytes. Returns the messages kept in memory only, or null when
     * nothing could be written.
     */
    fun save(file: File, state: OutboxState): Set<String>? {
        val (kept, memoryOnly) = forPersist(state)
        if (write(file, kept)) return memoryOnly
        val (bare, none) = forPersist(state, 0)
        return if (write(file, bare)) none else null
    }

    private fun write(file: File, state: OutboxState): Boolean = runCatching {
        file.parentFile?.mkdirs()
        val text = json.encodeToString(state)
        val tmp = File(file.parentFile, "${file.name}.tmp")
        tmp.writeText(text)
        if (!tmp.renameTo(file)) {
            file.writeText(text)
            tmp.delete()
        }
    }.isSuccess

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
        afterSeq: Long? = null,
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
            afterSeq = afterSeq,
        )
        val next = state.copy(
            nextSeq = state.nextSeq + (sessionId to (seq + 1L)),
            items = state.items + item,
        )
        return next to item
    }

    fun markSending(state: OutboxState, id: String, now: String = Instant.now().toString()): OutboxState =
        state.copy(items = state.items.map { if (it.id == id) it.copy(status = OutboxStatus.SENDING, error = null, attempted = true, updatedAt = now) else it })

    /** Delivered: its files' bytes are dropped (their names stay for the bubble). */
    fun markSent(state: OutboxState, id: String, now: String = Instant.now().toString()): OutboxState =
        state.copy(items = state.items.map {
            if (it.id == id) it.copy(status = OutboxStatus.SENT, error = null, updatedAt = now, attachments = it.attachments.map { a -> a.copy(data = "") }) else it
        })

    fun markError(state: OutboxState, id: String, error: String, transient: Boolean, now: String = Instant.now().toString()): OutboxState {
        val st = if (transient) OutboxStatus.PENDING else OutboxStatus.FAILED
        return state.copy(items = state.items.map { if (it.id == id) it.copy(status = st, error = error, updatedAt = now) else it })
    }

    fun retry(state: OutboxState, id: String, now: String = Instant.now().toString()): OutboxState =
        state.copy(items = state.items.map {
            if (it.id == id && !it.attachmentsLost && (it.status == OutboxStatus.FAILED || it.status == OutboxStatus.PENDING)) {
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

    /** The client_message_ids a stored event echoes: the sends it records, or the one that answered a question. */
    fun echoedIds(e: SessionEvent): List<String> =
        e.userMessage?.clientMessageIds ?: e.questionResolved?.clientMessageIds ?: emptyList()

    private const val ATTACHED_NOTE = "[Aide] Attached files"

    private fun isEchoText(m: OutboxMessage, recorded: String): Boolean {
        val sent = m.text.trim()
        val got = recorded.trim()
        if (m.attachments.isEmpty()) return sent.isNotEmpty() && got == sent
        // The machine lists the saved files after the text.
        return if (sent.isEmpty()) got.startsWith(ATTACHED_NOTE) else got.startsWith("$sent\n\n$ATTACHED_NOTE")
    }

    private fun enqueuedBefore(m: OutboxMessage, e: SessionEvent): Boolean {
        m.afterSeq?.let { return e.seq > it }
        // Kept before afterSeq existed: by time instead.
        return runCatching { !Instant.parse(e.ts).isBefore(Instant.parse(m.createdAt).minusSeconds(1)) }.getOrDefault(false)
    }

    /**
     * Drops the entries the machine has recorded, as the session's events show them (mirrors
     * packages/core/src/outbox.ts reconcileOutboxWithEvents): any entry whose client_message_id an event echoes
     * (UserMessage, or QuestionResolved for a message that answered the agent's question), whatever its status.
     * Unconfirmed SENT markers stay until the stream catches up. With a machine that echoes no id at all (no
     * event carries one), a SENT entry is matched by its text, only against UserMessage events after the one it
     * was enqueued behind, each event used once.
     */
    fun reconcileWithEvents(state: OutboxState, sessionId: String, events: List<SessionEvent>): OutboxState {
        val mine = state.items.filter { it.sessionId == sessionId }
        if (mine.isEmpty()) return state
        val echoed = HashSet<String>()
        for (e in events) echoed.addAll(echoedIds(e))
        val drop = mine.filter { it.id in echoed }.mapTo(HashSet()) { it.id }
        if (echoed.isEmpty()) {
            val users = events.filter { it.userMessage != null }
            val used = BooleanArray(users.size)
            for (m in mine.filter { it.status == OutboxStatus.SENT }.sortedBy { it.clientSeq }) {
                val i = users.indices.firstOrNull { !used[it] && enqueuedBefore(m, users[it]) && isEchoText(m, users[it].userMessage!!.text) }
                if (i != null) {
                    used[i] = true
                    drop += m.id
                }
            }
        }
        if (drop.isEmpty()) return state
        return state.copy(items = state.items.filterNot { it.id in drop })
    }

    /**
     * Mirrors packages/core/src/outbox.ts isTransientSendError: the core's errors by their code (and words); any
     * other exception only when it is a network one (an IOException: the core not reachable, a cut connection).
     */
    fun isTransientError(e: Throwable): Boolean {
        if (e !is ApiError) return e is IOException
        val code = e.code.lowercase()
        val msg = e.message.lowercase()
        if (code in setOf("failed_precondition", "invalid_argument", "permission_denied", "not_found") ||
            msg.contains("archived") || msg.contains("permission") || msg.contains("not found") || msg.contains("invalid")
        ) {
            return false
        }
        return code in setOf("unavailable", "deadline_exceeded", "unknown", "canceled") ||
            msg.contains("offline") || msg.contains("network") || msg.contains("fetch") || msg.contains("timeout") ||
            msg.contains("timed out") || msg.contains("unavailable") || msg.contains("connect") ||
            msg.contains("disconnected") || msg.contains("reset") || msg.contains("eof")
    }
}
