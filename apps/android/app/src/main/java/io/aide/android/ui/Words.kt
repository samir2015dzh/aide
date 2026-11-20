package io.aide.android.ui

import io.aide.android.model.instant
import java.time.Duration
import java.time.Instant
import java.util.Locale

/** English, or Chinese on a phone set to it: the words of the app's screens, side by side. */
fun t(en: String, zh: String): String = if (Locale.getDefault().language == "zh") zh else en

/** How long ago ts was, shortly: "now", "5m", "3h", "2d". */
fun relativeTime(ts: String, now: Instant = Instant.now()): String {
    if (ts.isEmpty()) return ""
    val s = Duration.between(instant(ts), now).seconds
    return when {
        s < 60 -> t("now", "刚刚")
        s < 3600 -> t("${s / 60}m", "${s / 60} 分钟前")
        s < 86400 -> t("${s / 3600}h", "${s / 3600} 小时前")
        else -> t("${s / 86400}d", "${s / 86400} 天前")
    }
}
