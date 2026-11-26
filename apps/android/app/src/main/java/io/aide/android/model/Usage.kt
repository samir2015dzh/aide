package io.aide.android.model

import io.aide.android.api.QueryWorkRequest
import io.aide.android.api.QueryWorkResponse
import java.time.LocalDate
import java.time.ZoneId
import java.time.temporal.ChronoUnit

// The usage card (工单 #142): a year of tokens, split by coding agent or by machine, by the same rules as the
// desktop's (packages/core/src/usage.ts: usageRequest, usageYear, usageParts), from the same QueryWork answer.

/**
 * What the year's tokens are split by: the coding agent that did the work, or the machine it ran on. A personal
 * agent follows the account, not a machine, and is neither: what it asked for is counted under the coding agent
 * that did it and the machine that ran it, as a local coding agent's own work is.
 */
enum class UsageGroup { Agent, Machine }

/** In the order the control shows them; the first is the one the card opens on, as the desktop's. */
val usageGroups = listOf(UsageGroup.Agent, UsageGroup.Machine)

/** A part of the year's tokens: an agent or machine (other: the least busy added up), its tokens, its share and its tokens by day. */
data class UsagePart(val id: String, val other: Boolean, val tokens: Long, val share: Double, val days: Map<LocalDate, Long>)

/** The year's tokens and today's. */
data class UsageTotals(val total: Long, val today: Long)

/** At most this many parts are named; past it the least busy are one, Other. */
const val USAGE_PARTS_SHOWN = 5

/** The first day a year of weeks ending in today's week starts on: a Sunday. */
fun usageSince(today: LocalDate, weeks: Int = 53): LocalDate =
    today.minusDays((today.dayOfWeek.value % 7).toLong() + (weeks - 1) * 7L)

/** The QueryWork question for a year of weeks: from usageSince to today, by day, coding agent and machine (both groupings in one answer). */
fun usageRequest(today: LocalDate, zone: ZoneId = ZoneId.systemDefault(), weeks: Int = 53): QueryWorkRequest {
    val since = usageSince(today, weeks)
    return QueryWorkRequest(
        timeZone = zone.id,
        firstDay = since.toString(),
        days = ChronoUnit.DAYS.between(since, today).toInt() + 1,
        bucket = "WORK_BUCKET_DAY",
        groupBy = listOf("WORK_DIMENSION_AGENT", "WORK_DIMENSION_MACHINE"),
    )
}

/** The year's tokens (the days up to today) and today's, ungrouped: what the parts add up to. */
fun usageTotals(res: QueryWorkResponse?, today: LocalDate, weeks: Int = 53): UsageTotals {
    val last = ChronoUnit.DAYS.between(usageSince(today, weeks), today).toInt()
    var total = 0L
    var now = 0L
    for (t in res?.turns.orEmpty()) {
        if (t.bucket > last) continue
        val n = t.inputTokens + t.outputTokens
        total += n
        if (t.bucket == last) now += n
    }
    return UsageTotals(total, now)
}

/**
 * The year's tokens in parts, the busiest first: they add up to its total, and a day's to the day's, whichever
 * the grouping (past [shown] parts, Other holds the rest).
 */
fun usageParts(res: QueryWorkResponse?, today: LocalDate, group: UsageGroup, weeks: Int = 53, shown: Int = USAGE_PARTS_SHOWN): List<UsagePart> {
    val since = usageSince(today, weeks)
    val last = ChronoUnit.DAYS.between(since, today).toInt()
    val tokens = LinkedHashMap<String, Long>()
    val days = HashMap<String, HashMap<LocalDate, Long>>()
    for (t in res?.turns.orEmpty()) {
        if (t.bucket > last) continue // not a day the year counts, so not in its total
        val n = t.inputTokens + t.outputTokens
        if (n == 0L) continue
        val id = (if (group == UsageGroup.Agent) t.group?.agent else t.group?.machineId).orEmpty()
        tokens[id] = (tokens[id] ?: 0L) + n
        val d = days.getOrPut(id) { HashMap() }
        val day = since.plusDays(t.bucket.toLong())
        d[day] = (d[day] ?: 0L) + n
    }
    val total = tokens.values.sum()
    val parts = tokens.entries.sortedWith(compareByDescending<Map.Entry<String, Long>> { it.value }.thenBy { it.key })
    val named = if (parts.size > shown) parts.take(shown - 1) else parts
    val share = { n: Long -> if (total > 0) n.toDouble() / total else 0.0 }
    val out = named.map { UsagePart(it.key, false, it.value, share(it.value), days[it.key].orEmpty()) }.toMutableList()
    if (named.size < parts.size) {
        val rest = parts.drop(named.size)
        val otherDays = HashMap<LocalDate, Long>()
        for (p in rest) for ((day, n) in days[p.key].orEmpty()) otherDays[day] = (otherDays[day] ?: 0L) + n
        val n = rest.sumOf { it.value }
        out += UsagePart("", true, n, share(n), otherDays)
    }
    return out
}

/** Tokens shortly, as the desktop says them (packages/core tokensText): 950, 12K, 3.4M. */
fun tokensText(n: Long): String = when {
    n >= 1_000_000 -> if (n % 1_000_000 == 250_000L) "${n / 1_000_000}.2M" else String.format(java.util.Locale.ROOT, "%.1fM", n / 1e6)
    n >= 1000 -> "${n / 1000}K"
    else -> "$n"
}
