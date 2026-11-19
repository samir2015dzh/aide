package io.aide.android.her

import io.aide.android.model.AppModel
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive

/**
 * A machine's Her while her page is open (the iPhone's HerChat, minimal):
 * the environment's connection (bots/subscribe: her assistants and their
 * routines), and her default assistant's conversation (bots/open: its
 * snapshot, then its events). Messages go with HerSend; Her acts without
 * asking for approvals (docs/accounts.md §7). Memory is read once asked.
 */
class HerPanel(private val model: AppModel, val machineId: String) {
    private val her = HerClient(model.api!!, machineId)

    private val _bots = MutableStateFlow<List<HerBot>>(emptyList())
    val bots = _bots.asStateFlow()
    private val _lines = MutableStateFlow<List<HerLine>>(emptyList())
    val lines = _lines.asStateFlow()
    private val _working = MutableStateFlow(false)
    val working = _working.asStateFlow()
    private val _connected = MutableStateFlow(false)
    val connected = _connected.asStateFlow()
    private val _error = MutableStateFlow<String?>(null)
    val error = _error.asStateFlow()

    private var envConnection = ""
    private var convConnection = ""
    private var conversation: HerConversation? = null

    val bot: HerBot? get() = _bots.value.firstOrNull { it.isDefault } ?: _bots.value.firstOrNull()

    fun start(scope: CoroutineScope): Job = scope.launch {
        launch { followEnvironment() }
        // The conversation opens once Her's assistant is known.
        val id = _bots.first { it.isNotEmpty() }.let { bot?.id } ?: return@launch
        followConversation(id)
    }

    private suspend fun CoroutineScope.followEnvironment() {
        while (isActive) {
            model.foreground.first { it }
            try {
                her.open(listOf("bots/subscribe" to JsonObject(emptyMap()))).collect { e ->
                    when (e) {
                        is HerEvent.Opened -> {
                            envConnection = e.connectionId
                            e.results.firstOrNull()?.onSuccess { _bots.value = HerBot.list(it) }?.onFailure { _error.value = it.message }
                        }
                        is HerEvent.Notification -> if (e.method == "bots/changed") _bots.value = HerBot.merge(_bots.value, e.params)
                    }
                }
            } catch (e: Exception) {
                _error.value = e.message
            }
            envConnection = ""
            delay(2000)
        }
    }

    private suspend fun CoroutineScope.followConversation(botId: String) {
        val open = JsonObject(mapOf(
            "bot_id" to JsonPrimitive(botId),
            "events" to JsonObject(mapOf("exclude" to JsonArray(listOf("text_delta", "thinking_delta", "tool_call_delta", "tool_progress").map(::JsonPrimitive)))),
            "history_limit" to JsonPrimitive(60),
        ))
        while (isActive) {
            model.foreground.first { it }
            try {
                her.open(listOf("bots/open" to open)).collect { e ->
                    when (e) {
                        is HerEvent.Opened -> e.results.firstOrNull()?.onSuccess { r ->
                            convConnection = e.connectionId
                            conversation = HerConversation(r["snapshot"])
                            _connected.value = true
                            _error.value = null
                            publish()
                        }?.onFailure { _error.value = it.message }
                        is HerEvent.Notification -> if (e.method == "session/event" && e.params["session_id"].str == conversation?.sessionId) {
                            conversation?.apply(e.params["event"])
                            publish()
                        }
                    }
                }
            } catch (e: Exception) {
                _error.value = e.message
            }
            convConnection = ""
            _connected.value = false
            delay(2000)
        }
    }

    private fun publish() {
        val c = conversation ?: return
        _lines.value = c.lines()
        _working.value = c.working
    }

    /** Sends the owner's words to Her: delivered now on the open conversation, or kept until the machine is back. */
    suspend fun send(text: String): Boolean {
        val b = bot ?: return false
        return try {
            val r = her.send(b.id, text, convConnection, conversation?.sessionId ?: "")
            if (!r.delivered) model.say(io.aide.android.ui.t("Kept on this phone until Her is back", "已保存在手机上，Her 恢复后送达"))
            true
        } catch (e: Exception) {
            model.say(e.message ?: e.toString())
            false
        }
    }

    suspend fun memory(): HerMemory? {
        val b = bot ?: return null
        return try {
            HerMemory.parse(her.call("bots/memory", JsonObject(mapOf("bot_id" to JsonPrimitive(b.id))), envConnection))
        } catch (e: Exception) {
            _error.value = e.message
            null
        }
    }
}
