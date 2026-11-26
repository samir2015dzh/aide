package io.aide.android.model

import io.aide.android.api.ApiError
import io.aide.android.api.ConnectClient
import io.aide.android.api.SessionEvent
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.selects.select
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.JsonArray
import java.io.File

/**
 * A conversation's events as kept on disk: its last [SessionFeed.MAX_CACHED_EVENTS] stored events; whole
 * when nothing before them was left out (the first format, a bare list, never says so).
 */
@Serializable
internal data class CachedFeed(val whole: Boolean = false, val events: List<SessionEvent> = emptyList())

/**
 * A session's conversation while its page is open: its events are restored from
 * local disk cache (surviving process kill), then followed via StreamSessionEvents
 * (with follow and drafts), waking immediately on network reconnect or WiFi/cellular
 * switch so missed chunks catch up without gaps or duplicates (工单 #115). A cache
 * that holds the whole conversation goes on after its last event; one that holds
 * only its last events shows them at once while the conversation is read again from
 * its start, so that no earlier turn goes missing. Reading and writing the cache is
 * off the main thread, writes coalesced.
 */
class SessionFeed(private val model: AppModel, val sessionId: String) {
    private var transcript = Transcript()
    /** The last MAX_CACHED_EVENTS stored events, as kept on disk. */
    private val cachedEvents = ArrayDeque<SessionEvent>()
    /** cachedEvents begin at the conversation's start: nothing before them was left out. */
    private var cacheWhole = true
    /** What was kept of the conversations when the page opened (AppModel.clearLocalConversations). */
    private val epoch = model.conversationsEpoch
    private var saveJob: Job? = null
    private var saveDirty = false
    private val _items = MutableStateFlow<List<Item>>(emptyList())
    val items = _items.asStateFlow()
    private val _loading = MutableStateFlow(true)
    val loading = _loading.asStateFlow()
    private val _error = MutableStateFlow<String?>(null)
    val error = _error.asStateFlow()

    private val cacheFile: File = File(model.sessionEventsDir, cacheName(sessionId))
    /** The last stored event's seq this page has (shown or read), for what is sent from it (OutboxMessage.afterSeq). */
    var lastSeq: Long = 0
        private set

    private fun loadCachedEvents(): CachedFeed {
        if (!cacheFile.exists()) return CachedFeed()
        return runCatching {
            val el = ConnectClient.json.parseToJsonElement(cacheFile.readText())
            val feed = if (el is JsonArray) {
                CachedFeed(whole = false, events = ConnectClient.json.decodeFromJsonElement(ListSerializer(SessionEvent.serializer()), el))
            } else {
                ConnectClient.json.decodeFromJsonElement(CachedFeed.serializer(), el)
            }
            feed.copy(events = feed.events.sortedBy { it.seq })
        }.getOrDefault(CachedFeed())
    }

    /** Keeps cachedEvents on disk, a moment after the last change, off the main thread. */
    private fun scheduleSave() {
        saveDirty = true
        if (saveJob?.isActive == true) return
        // The app's scope: what came in just before the page closed is kept too.
        saveJob = model.scope.launch {
            while (saveDirty) {
                delay(SAVE_DELAY_MS)
                saveDirty = false
                if (model.conversationsEpoch != epoch) return@launch // forgotten meanwhile (signed out)
                val snapshot = CachedFeed(cacheWhole, cachedEvents.toList())
                withContext(Dispatchers.IO) { persist(snapshot) }
            }
        }
    }

    private fun persist(feed: CachedFeed) {
        runCatching {
            val dir = cacheFile.parentFile ?: return
            dir.mkdirs()
            val text = ConnectClient.json.encodeToString(CachedFeed.serializer(), feed)
            val tmp = File(dir, "${cacheFile.name}.tmp")
            tmp.writeText(text)
            if (!tmp.renameTo(cacheFile)) {
                cacheFile.writeText(text)
                tmp.delete()
            }
            prune(dir, cacheFile)
        }
    }

    fun start(scope: CoroutineScope): Job = scope.launch {
        val held = withContext(Dispatchers.IO) { loadCachedEvents() }
        // The stream's transcript shows once it gets here: past what was held, read again from the start.
        var showFrom = 0L
        if (held.events.isNotEmpty()) {
            val shown = Transcript()
            for (e in held.events) shown.add(e)
            _items.value = shown.items()
            _loading.value = false
            lastSeq = held.events.last().seq
            model.reconcileOutbox(sessionId, held.events)
            if (held.whole) {
                transcript = shown
                cachedEvents.addAll(held.events)
            } else {
                showFrom = held.events.last().seq
            }
        }
        var backoff = 1000L
        while (isActive) {
            model.foreground.first { it }
            val api = model.api ?: return@launch
            try {
                api.sessionEvents(sessionId, transcript.lastSeq).collect { e ->
                    backoff = 1000L
                    val prevSeq = transcript.lastSeq
                    transcript.add(e)
                    if (transcript.lastSeq > lastSeq) lastSeq = transcript.lastSeq
                    val caughtUp = transcript.lastSeq >= showFrom
                    if (e.seq > prevSeq && e.agentDraft == null) {
                        cachedEvents.addLast(e)
                        while (cachedEvents.size > MAX_CACHED_EVENTS) {
                            cachedEvents.removeFirst()
                            cacheWhole = false
                        }
                        if (caughtUp) {
                            scheduleSave()
                            model.reconcileOutbox(sessionId, cachedEvents.toList())
                        }
                    }
                    if (caughtUp) {
                        _items.value = transcript.items()
                        _loading.value = false
                    }
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
        const val MAX_CACHED_EVENTS = 250
        /** The conversations kept on disk at most, the ones opened last (as the desktop keeps 10). */
        const val MAX_CACHED_SESSIONS = 10
        private const val SAVE_DELAY_MS = 300L

        fun cacheName(sessionId: String): String = "${sessionId.replace(Regex("[^a-zA-Z0-9._-]"), "_")}.json"

        /** Keeps the MAX_CACHED_SESSIONS conversations written last in dir, current among them. */
        internal fun prune(dir: File, current: File, keep: Int = MAX_CACHED_SESSIONS) {
            val files = dir.listFiles { f -> f.isFile && f.name.endsWith(".json") && f != current }.orEmpty()
                .sortedByDescending { it.lastModified() }
            files.drop(keep - 1).forEach { it.delete() }
        }
    }
}
