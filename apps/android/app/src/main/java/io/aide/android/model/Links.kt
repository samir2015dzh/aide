package io.aide.android.model

import java.net.URI

/**
 * A server's address as the person types it to log in or set it up:
 * "https://…" kept, a bare name given https, an IP address http (a relay by
 * IP has no certificate).
 */
fun serverURL(s: String): String? {
    val t = s.trim().trimEnd('/')
    if (t.isEmpty()) return null
    if (t.startsWith("https://") || t.startsWith("http://")) return runCatching { URI(t).host?.let { t } }.getOrNull()
    val host = t.substringBefore('/').substringBeforeLast(':').trim('[', ']')
    val ip = host.matches(Regex("""\d{1,3}(\.\d{1,3}){3}""")) || host.contains(':')
    return (if (ip) "http://" else "https://") + t
}
