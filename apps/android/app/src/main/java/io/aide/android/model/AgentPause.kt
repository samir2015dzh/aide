package io.aide.android.model

import io.aide.android.api.AgentStatus
import java.time.Instant

/**
 * When the agent's plan usage resets, to pause it until then (SetAgentPaused resumeAt), as far as its machine read it
 * (Claude Code only): the latest reset of the windows used up, since it runs again only once all of them have reset;
 * with none used up, the reset of the fullest. Null without a reading of a reset still to come: no time is guessed.
 * As on the desktop (agentPause.ts).
 */
fun usageResetAt(a: AgentStatus, now: Instant): Instant? {
    val windows = a.usage?.windows.orEmpty().mapNotNull { w -> instantOrNull(w.resetsAt)?.takeIf { it.isAfter(now) }?.let { w to it } }
    val full = windows.filter { it.first.utilization >= 100.0 }
    if (full.isNotEmpty()) return full.maxOf { it.second }
    return windows.maxByOrNull { it.first.utilization }?.second
}

/** Until when the paused agent stays paused, when it resumes by itself; null when only the owner resumes it. */
fun pausedUntil(a: AgentStatus): Instant? = a.pause?.let { instantOrNull(it.resumeAt) }
