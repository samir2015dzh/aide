package io.hermesh.android.model

import io.hermesh.android.api.ApiError
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

/**
 * A session's conversation while its page is open: its events read once and
 * then followed (StreamSessionEvents with follow, drafts included), again
 * from the last one after the app comes back to the front.
 */
class SessionFeed(private val model: AppModel, val sessionId: String) {
    private val transcript = Transcript()
    private val _items = MutableStateFlow<List<Item>>(emptyList())
    val items = _items.asStateFlow()
    private val _loading = MutableStateFlow(true)
    val loading = _loading.asStateFlow()
    private val _error = MutableStateFlow<String?>(null)
    val error = _error.asStateFlow()

    fun start(scope: CoroutineScope): Job = scope.launch {
        while (isActive) {
            model.foreground.first { it }
            val api = model.api ?: return@launch
            try {
                api.sessionEvents(sessionId, transcript.lastSeq).collect { e ->
                    transcript.add(e)
                    _items.value = transcript.items()
                    _loading.value = false
                    _error.value = null
                }
            } catch (e: ApiError) {
                _error.value = e.message.ifEmpty { e.code }
            }
            _loading.value = false
            delay(1000)
        }
    }
}
