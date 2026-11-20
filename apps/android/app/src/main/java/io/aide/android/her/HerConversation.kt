package io.aide.android.her

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.longOrNull

// Her's protocol, as far as the phone's minimal Her panel reads it (the
// iPhone's HermeshHer, cut down): her's JSON over the core's HerOpen,
// HerCall and HerSend (docs/her-server.md).

internal operator fun JsonElement?.get(key: String): JsonElement? = (this as? JsonObject)?.get(key)
internal val JsonElement?.str: String? get() = (this as? JsonPrimitive)?.takeIf { it.isString }?.contentOrNull
internal val JsonElement?.bool: Boolean? get() = (this as? JsonPrimitive)?.booleanOrNull
internal val JsonElement?.long: Long? get() = (this as? JsonPrimitive)?.longOrNull ?: (this as? JsonPrimitive)?.contentOrNull?.toDoubleOrNull()?.toLong()
internal val JsonElement?.arr: List<JsonElement> get() = (this as? JsonArray) ?: emptyList()

/** An assistant of a Her environment (bots/subscribe), with its routines. */
data class HerBot(
    val id: String,
    val name: String,
    val isDefault: Boolean,
    val unread: Int,
    val typing: Boolean,
    val routines: List<HerRoutine>,
    val routinesError: String?,
) {
    companion object {
        fun parse(j: JsonElement?): HerBot? {
            val id = j["id"].str ?: return null
            return HerBot(
                id, j["name"].str ?: "", j["default"].bool ?: false, (j["unread"].long ?: 0).toInt(), j["typing"].bool ?: false,
                j["routines"].arr.mapNotNull(HerRoutine::parse), j["routines_error"].str,
            )
        }

        /** bots/subscribe's result, or a bots/changed notification applied to list. */
        fun list(result: JsonElement?): List<HerBot> = result["bots"].arr.mapNotNull(::parse)

        fun merge(list: List<HerBot>, changed: JsonElement?): List<HerBot> {
            val deleted = changed["deleted"].arr.mapNotNull { it.str }.toSet()
            val new = list(changed)
            val ids = new.map { it.id }.toSet()
            return list.filter { it.id !in deleted && it.id !in ids } + new.filter { it.id !in deleted }
        }
    }
}

/** A scheduled task of an assistant's (docs/kernel/routine-design.md §8), read-only here. */
data class HerRoutine(val id: String, val name: String, val schedule: String, val state: String, val nextRun: String?, val running: Boolean, val lastOutcome: String?) {
    companion object {
        fun parse(j: JsonElement?): HerRoutine? {
            val id = j["id"].str ?: return null
            return HerRoutine(id, j["name"].str ?: "", j["when"].str ?: "", j["state"].str ?: "active", j["next_run"].str, j["running"].bool ?: false, j["last_run"]["outcome"].str)
        }
    }
}

/** What the conversation shows: the owner's messages, what Her sent with send_to_user, and tasks that failed. */
sealed interface HerLine {
    val key: String

    data class Owner(override val key: String, val text: String) : HerLine
    data class Her(override val key: String, val text: String) : HerLine
    data class Failed(override val key: String, val text: String) : HerLine
}

/**
 * A conversation: the snapshot bots/open returned, then each session/event
 * after it (the iPhone's HerSession, reading only what the panel shows).
 */
class HerConversation(snapshot: JsonElement?) {
    val sessionId: String = snapshot["session"]["session_id"].str ?: ""
    var lastSeq: Long = snapshot["last_seq"].long ?: 0
        private set
    private val entries = mutableListOf<JsonElement>()
    /** A task runs. */
    var working = false
        private set

    init {
        entries += snapshot["transcript"]["items"].arr
        val task = snapshot["active_task"]
        if (task["task_id"].str != null) working = true
    }

    /** An event (her's Event); one it has already changes nothing. */
    fun apply(e: JsonElement?) {
        val seq = e["seq"].long ?: return
        if (seq <= lastSeq) return
        lastSeq = seq
        when (e["type"].str) {
            "task_started" -> working = true
            "message_completed" -> entries += JsonObject(mapOf("type" to JsonPrimitive("message"), "message_id" to (e["message_id"] ?: JsonNull), "message" to (e["message"] ?: JsonNull)))
            "task_completed", "task_interrupted", "session_closed" -> working = false
            "task_failed" -> {
                working = false
                entries += JsonObject(mapOf("type" to JsonPrimitive("task_end"), "outcome" to JsonPrimitive("failed"), "error" to (e["error"] ?: JsonNull)))
            }
        }
    }

    fun lines(): List<HerLine> = entries.flatMapIndexed { i, j ->
        when (j["type"].str) {
            "message" -> {
                val m = j["message"]
                val id = j["message_id"].str ?: "m$i"
                when (m["role"].str) {
                    "user" -> {
                        val text = m["content"].arr.filter { it["type"].str == "text" }.mapNotNull { it["text"].str }.joinToString("\n").trim()
                        if (text.isEmpty() || j["mail"] != null && j["mail"] !is JsonNull) emptyList() else listOf(HerLine.Owner(id, text))
                    }
                    "assistant" -> m["content"].arr
                        .filter { it["type"].str == "tool_call" && it["name"].str == "send_to_user" }
                        .mapNotNull { c -> c["arguments"]["text"].str?.takeIf { it.isNotBlank() }?.let { HerLine.Her(c["id"].str ?: "$id-$i", it) } }
                    else -> emptyList()
                }
            }
            "task_end" -> if (j["outcome"].str == "failed") listOf(HerLine.Failed("f$i", j["error"]["message"].str ?: "Her could not finish")) else emptyList()
            else -> emptyList()
        }
    }
}

/** An assistant's memory (bots/memory): its notes, and the pages it keeps. */
data class HerMemory(val text: String, val pages: List<String>) {
    companion object {
        fun parse(j: JsonElement?) = HerMemory(j["text"].str ?: "", j["pages"].arr.mapNotNull { it["name"].str })
    }
}
