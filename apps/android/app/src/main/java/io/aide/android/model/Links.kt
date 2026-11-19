package io.aide.android.model

import java.net.URI

/** A connection address, "relay.example.com/o_…": where devices connect, and the owner (as the iPhone's ConnectAddress). */
data class ConnectAddress(val relay: String, val owner: String) {
    companion object {
        fun parse(s: String): ConnectAddress? {
            var t = s.trim()
            if (t.isEmpty()) return null
            if (!t.contains("://")) t = "https://$t"
            val u = runCatching { URI(t) }.getOrNull() ?: return null
            val scheme = u.scheme?.lowercase() ?: return null
            if (scheme != "https" && scheme != "http") return null
            val host = u.host ?: return null
            val parts = (u.path ?: "").split("/").filter { it.isNotEmpty() }
            val owner = parts.lastOrNull() ?: return null
            if (!owner.startsWith("o_") || owner.length != 26) return null
            val prefix = if (parts.size > 1) "/" + parts.dropLast(1).joinToString("/") else ""
            val port = if (u.port >= 0) ":${u.port}" else ""
            val h = if (host.contains(':') && !host.startsWith("[")) "[$host]" else host
            return ConnectAddress("$scheme://$h$port$prefix", owner)
        }
    }
}

/**
 * A server's address as the person types it to claim it (a new self-hosted
 * relay): "https://…" kept, a bare name given https, an IP address http (a
 * relay by IP has no certificate).
 */
fun serverURL(s: String): String? {
    val t = s.trim().trimEnd('/')
    if (t.isEmpty()) return null
    if (t.startsWith("https://") || t.startsWith("http://")) return runCatching { URI(t).host?.let { t } }.getOrNull()
    val host = t.substringBefore('/').substringBeforeLast(':').trim('[', ']')
    val ip = host.matches(Regex("""\d{1,3}(\.\d{1,3}){3}""")) || host.contains(':')
    return (if (ip) "http://" else "https://") + t
}

/** A pairing code to look up: hermesh://pair?code=…, a center's https://host/p/CODE, or the bare code as a machine shows it. */
object PairingLink {
    private val bare = Regex("[0-9A-Z]{4}-[0-9A-Z]{4}-[0-9A-Z]{4}")

    fun code(s: String): String? {
        val t = s.trim()
        if (t.isEmpty()) return null
        if (t.contains("://")) {
            val u = runCatching { URI(t) }.getOrNull() ?: return null
            if (u.scheme == "hermesh" && u.host == "pair") {
                val q = u.rawQuery ?: return null
                return q.split("&").map { it.split("=", limit = 2) }.firstOrNull { it[0] == "code" }?.getOrNull(1)?.let(::bareCode)
            }
            if (u.scheme != "https" && u.scheme != "http") return null
            val parts = (u.path ?: "").split("/").filter { it.isNotEmpty() }
            return if (parts.size == 2 && parts[0] == "p") bareCode(parts[1]) else null
        }
        return bareCode(t)
    }

    private fun bareCode(s: String): String? = s.uppercase().takeIf { bare.matches(it) }
}
