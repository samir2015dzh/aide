package io.aide.android.model

import io.aide.android.api.Question
import io.aide.android.api.SessionEvent
import io.aide.android.api.WorkspaceRequest

/** One thing a conversation shows, in order. */
sealed interface Item {
    val key: String

    /** from: who wrote it ("owner", an agent's client id, or "" when not known); fromName: an agent's name. */
    data class User(override val key: String, val text: String, val from: String = "", val fromName: String = "") : Item
    data class Agent(override val key: String, val text: String) : Item
    /** The reply the agent is writing now; gone once a stored event follows. */
    data class Draft(override val key: String, val text: String) : Item
    /** A run of tool calls, one line until opened. */
    data class Tools(override val key: String, val calls: List<Tool>) : Item
    data class Asked(override val key: String, val question: Question, val resolution: String?) : Item
    data class Message(override val key: String, val from: String, val text: String, val sent: Boolean) : Item
    /** A managed workspace's request after one change: shown only, answered by the coordinator through Aide's messages. */
    data class Workspace(override val key: String, val request: WorkspaceRequest) : Item
    /**
     * text: the agent's raw message, shown only under Details; kind and provider: what the machine made of it (工单 #105).
     * auth: it failed not signed in (工单 #108), which signing in from Aide fixes for Claude Code.
     */
    data class Error(override val key: String, val text: String, val kind: String = "", val provider: String = "") : Item {
        val auth get() = kind == "FAILURE_KIND_AUTH"
    }
    data class TurnEnd(override val key: String, val failed: Boolean, val text: String, val kind: String = "", val provider: String = "") : Item {
        val auth get() = kind == "FAILURE_KIND_AUTH"
    }
    /** The session was rolled back to the end of turn: turns turn+1 to lastTurn, above, were discarded. */
    data class Rollback(override val key: String, val turn: Int, val lastTurn: Int) : Item
}

data class Tool(val callId: String, val name: String, val summary: String, val result: String? = null, val isError: Boolean = false, val title: String = "") {
    /**
     * The line it shows by: the machine's title, else its summary when that reads as one (Aide's own
     * sessions), never raw arguments spread across the row (a machine from before titles): "" then,
     * the name alone shows and the arguments fold under it.
     */
    val line: String get() = title.ifEmpty { if (summary.trimStart().startsWith("{")) "" else summary.lineSequence().firstOrNull().orEmpty() }
}

/**
 * A session's events folded into what its page shows (as the iPhone's
 * AideSessions Transcript): consecutive tool calls in one group, each
 * with its result; questions with how they were resolved; the draft being
 * written last, until a stored event ends it.
 */
class Transcript {
    private val items = mutableListOf<Item>()
    private var draft: String? = null
    var lastSeq: Long = 0
        private set

    fun add(e: SessionEvent) {
        if (e.agentDraft != null) {
            draft = if (e.agentDraft.whole || draft == null) e.agentDraft.text else draft + e.agentDraft.text
            return
        }
        if (e.seq != 0L && e.seq <= lastSeq) return // seen already (a stream again after a reconnect)
        if (e.seq != 0L) lastSeq = e.seq
        draft = null
        val key = "e${e.seq}"
        when {
            e.userMessage != null -> items += Item.User(key, e.userMessage.text, e.userMessage.from, e.userMessage.fromName)
            e.agentMessage != null -> if (e.agentMessage.text.isNotBlank()) items += Item.Agent(key, e.agentMessage.text)
            e.toolCall != null -> {
                val t = Tool(e.toolCall.callId, e.toolCall.name.ifEmpty { e.toolCall.kind }, e.toolCall.summary, title = e.toolCall.title)
                val last = items.lastOrNull()
                if (last is Item.Tools) items[items.lastIndex] = last.copy(calls = last.calls + t) else items += Item.Tools(key, listOf(t))
            }
            e.toolResult != null -> {
                val i = items.indexOfLast { it is Item.Tools && it.calls.any { c -> c.callId == e.toolResult.callId } }
                if (i >= 0) {
                    val g = items[i] as Item.Tools
                    items[i] = g.copy(calls = g.calls.map { c ->
                        if (c.callId == e.toolResult.callId) c.copy(result = e.toolResult.summary, isError = e.toolResult.isError) else c
                    })
                }
            }
            e.questionAsked?.question != null -> items += Item.Asked(key, e.questionAsked.question, null)
            e.questionResolved != null -> {
                val i = items.indexOfLast { it is Item.Asked && it.question.questionId == e.questionResolved.questionId }
                if (i >= 0) items[i] = (items[i] as Item.Asked).copy(resolution = e.questionResolved.resolution.ifEmpty { "QUESTION_RESOLUTION_ANSWERED" })
            }
            e.messageReceived != null -> items += Item.Message(key, e.messageReceived.fromName.ifEmpty { e.messageReceived.from }, e.messageReceived.text, sent = false)
            e.messageSent != null -> items += Item.Message(key, e.messageSent.toName.ifEmpty { e.messageSent.to }, e.messageSent.text, sent = true)
            e.workspaceRequest != null -> items += Item.Workspace(key, e.workspaceRequest)
            e.rolledBack != null -> items += Item.Rollback(key, e.rolledBack.turn, e.rolledBack.lastTurn)
            e.error != null -> items += Item.Error(key, e.error.message.ifEmpty { e.error.failure?.message.orEmpty() }, e.error.failure?.kind.orEmpty(), e.error.failure?.provider.orEmpty())
            e.turnCompleted != null -> {
                val f = e.turnCompleted.failure
                if (e.turnCompleted.isError || f != null) items += Item.TurnEnd(key, true, f?.message?.ifEmpty { null } ?: e.turnCompleted.result, f?.kind.orEmpty(), f?.provider.orEmpty())
            }
        }
    }

    /** What to show now: the stored items, then the draft. */
    fun items(): List<Item> = draft?.takeIf { it.isNotBlank() }?.let { items + Item.Draft("draft", it) } ?: items.toList()
}
