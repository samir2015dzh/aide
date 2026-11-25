package io.aide.android.model

import io.aide.android.api.ApiError
import io.aide.android.api.ConnectClient
import io.aide.android.api.SessionEvent
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.selects.select
import kotlinx.serialization.builtins.ListSerializer
import java.io.File

/**
 * A session's conversation while its page is open: its events are restored from
 * local disk cache (surviving process kill), then followed from transcript.lastSeq
 * via StreamSessionEvents (with follow and drafts), waking immediately on network
 * reconnect or WiFi/cellular switch so missed chunks catch up without gaps or duplicates (工单 #115).
 */
class SessionFeed(private val model: AppModel, val sessionId: String) {
    private val transcript = Transcript()
    private val cachedEvents = mutableListOf<SessionEvent>()
    private val _items = MutableStateFlow<List<Item>>(emptyList())
    val items = _items.asStateFlow()
    private val _loading = MutableStateFlow(true)
    val loading = _loading.asStateFlow()
    private val _error = MutableStateFlow<String?>(null)
    val error = _error.asStateFlow()

    private val cacheFile: File? = model.sessionEventsDir?.let { File(it, "${sessionId.replace(Regex("[^a-zA-Z0-9._-]"), "_")}.json") }

    init {
        val restored = loadCachedEvents()
        if (restored.isNotEmpty()) {
            for (e in restored) {
                transcript.add(e)
                cachedEvents += e
            }
            _items.value = transcript.items()
            _loading.value = false
            model.reconcileOutbox(sessionId, _items.value)
        }
    }

    private fun loadCachedEvents(): List<SessionEvent> {
        val file = cacheFile ?: return emptyList()
        if (!file.exists()) return emptyList()
        return runCatching {
            ConnectClient.json.decodeFromString(ListSerializer(SessionEvent.serializer()), file.readText())
                .sortedBy { it.seq }
        }.getOrDefault(emptyList())
    }

    private fun persistCachedEvents() {
        val file = cacheFile ?: return
        runCatching {
            file.parentFile?.mkdirs()
            val slice = if (cachedEvents.size > MAX_CACHED_EVENTS) cachedEvents.takeLast(MAX_CACHED_EVENTS) else cachedEvents
            val tmp = File(file.parentFile, "${file.name}.tmp")
            tmp.writeText(ConnectClient.json.encodeToString(ListSerializer(SessionEvent.serializer()), slice))
            if (!tmp.renameTo(file)) {
                file.writeText(ConnectClient.json.encodeToString(ListSerializer(SessionEvent.serializer()), slice))
                tmp.delete()
            }
        }
    }

    fun start(scope: CoroutineScope): Job = scope.launch {
        var backoff = 1000L
        while (isActive) {
            model.foreground.first { it }
            val api = model.api ?: return@launch
            try {
                api.sessionEvents(sessionId, transcript.lastSeq).collect { e ->
                    backoff = 1000L
                    val prevSeq = transcript.lastSeq
                    transcript.add(e)
                    if (e.seq > prevSeq && e.agentDraft == null) {
                        cachedEvents += e
                        persistCachedEvents()
                    }
                    val nextItems = transcript.items()
                    _items.value = nextItems
                    model.reconcileOutbox(sessionId, nextItems)
                    _loading.value = false
                    _error.value = null
                }
            } catch (e: ApiError) {
                _error.value = e.message.ifEmpty { e.code }
                backoff = (backoff * 2).coerceAtMost(8000L)
            }
            _loading.value = false
            // Sleep until backoff expires OR connectivity/reconnect pulse fires.
            val waiter = launch { model.reconnectPulse.first() }
            val timer = launch { delay(backoff) }
            select<Unit> {
                waiter.onJoin { backoff = 1000L }
                timer.onJoin {}
            }
            waiter.cancel()
            timer.cancel()
        }
    }

    companion object {
        private const val MAX_CACHED_EVENTS = 250
    }
}
