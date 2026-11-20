package io.aide.android.connection

import android.content.Context
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * Staying connected in the background (docs/android-app.md §4): the
 * person's switch (on unless they turned it off) and whether they have
 * seen the guide to HyperOS's battery and autostart settings.
 */
class Keepalive(context: Context) {
    private val prefs = context.getSharedPreferences("connection", Context.MODE_PRIVATE)
    private val _enabled = MutableStateFlow(prefs.getBoolean(ENABLED, true))
    val enabled = _enabled.asStateFlow()
    private val _guided = MutableStateFlow(prefs.getBoolean(GUIDED, false))
    /** The guide was shown once (followed or skipped): it is not shown again. */
    val guided = _guided.asStateFlow()

    fun enable(on: Boolean) {
        prefs.edit().putBoolean(ENABLED, on).apply()
        _enabled.value = on
    }

    fun guideDone() {
        prefs.edit().putBoolean(GUIDED, true).apply()
        _guided.value = true
    }

    private companion object {
        const val ENABLED = "enabled"
        const val GUIDED = "guided"
    }
}

/** The core's connection to the relay, as the app shows it. */
enum class Link { Connected, Reconnecting, Disconnected }

/**
 * running: the core follows the relay (the app is in front, or the
 * connection service keeps it); online: the phone has a network.
 * Without one, or with the core paused, it is disconnected; with both and
 * no relay yet, the core is trying again (it backs off, up to 10 seconds,
 * and tries at once when the network changes).
 */
fun link(running: Boolean, online: Boolean, relayConnected: Boolean): Link = when {
    running && relayConnected -> Link.Connected
    running && online -> Link.Reconnecting
    else -> Link.Disconnected
}
