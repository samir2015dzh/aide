package io.hermesh.android

import io.hermesh.android.api.Failure
import io.hermesh.android.api.LostSpan
import io.hermesh.android.api.Machine
import io.hermesh.android.api.Session
import io.hermesh.android.api.SessionProgress
import io.hermesh.android.api.SessionStatus
import io.hermesh.android.model.Liveness
import io.hermesh.android.model.SessionLink
import io.hermesh.android.model.StateLimits
import io.hermesh.android.model.World
import io.hermesh.android.model.lostSpanText
import io.hermesh.android.model.sessionStateOf
import io.hermesh.android.model.stateLine
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.boolean
import kotlinx.serialization.json.int
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Test
import java.io.File
import java.time.Duration
import java.time.Instant
import java.time.ZoneOffset
import java.util.Locale

/** The cases internal/sessionstate (Go) and packages/core (states.ts) are checked against too. */
class StatesTest {
    private val now = Instant.parse("2026-11-19T12:00:00Z")
    private fun ago(o: JsonObject, k: String): Instant? = o[k]?.jsonPrimitive?.intOrNull?.let { now.minusSeconds(it.toLong()) }
    private fun ts(o: JsonObject, k: String): String = ago(o, k)?.toString().orEmpty()

    @Test fun theSharedCases() {
        val table = Json.parseToJsonElement(File("../../../internal/sessionstate/testdata/cases.json").readText()).jsonObject
        val l = table["limits"]!!.jsonObject
        val limits = StateLimits(Duration.ofSeconds(l["lost_after_seconds"]!!.jsonPrimitive.int.toLong()), Duration.ofSeconds(l["stall_after_seconds"]!!.jsonPrimitive.int.toLong()))
        for (c in table["cases"]!!.jsonArray.map { it.jsonObject }) {
            val name = c["name"]!!.jsonPrimitive.content
            val ss = c["session"]!!.jsonObject
            val failure = ss["failure"]!!.jsonPrimitive.content
            val s = Session(
                status = "SESSION_STATUS_" + ss["status"]!!.jsonPrimitive.content,
                updatedAt = ts(ss, "updated_ago"),
                progress = SessionProgress(lastEventAt = ts(ss, "last_event_ago"), lastFailure = if (failure.isEmpty()) null else Failure(kind = "FAILURE_KIND_$failure")),
            )
            val m = c["machine"]!!.jsonObject
            val v = sessionStateOf(
                s,
                Liveness(m["known"]!!.jsonPrimitive.boolean, m["local"]!!.jsonPrimitive.boolean, m["unreachable"]!!.jsonPrimitive.content, m["syncing"]!!.jsonPrimitive.boolean, ago(m, "heard_ago"), ago(m, "unreachable_since_ago")),
                limits, now,
            )
            val want = c["want"]!!.jsonObject
            assertEquals(name, want["state"]!!.jsonPrimitive.content, v.state.name.lowercase())
            assertEquals(name, want["link"]!!.jsonPrimitive.content, v.link.name.lowercase())
            assertEquals(name, want["quiet_seconds"]!!.jsonPrimitive.int.toLong(), v.quiet.seconds)
            assertEquals(name, want["silent_seconds"]!!.jsonPrimitive.int.toLong(), v.silent.seconds)
        }
    }

    @Test fun theLine() {
        val keep = Locale.getDefault()
        try {
            val run = Session(status = SessionStatus.RUNNING, progress = SessionProgress(lastEventAt = now.minus(Duration.ofMinutes(12)).toString()))
            val lost = Liveness(true, unreachable = "offline", heardAt = now.minus(Duration.ofMinutes(7)))
            Locale.setDefault(Locale.ENGLISH)
            assertEquals("Running · quiet 12 min", stateLine(sessionStateOf(run, Liveness(true, local = true), StateLimits(), now)))
            assertEquals("Lost · last known Running · 7 min ago", stateLine(sessionStateOf(run, lost, StateLimits(), now)))
            Locale.setDefault(Locale.SIMPLIFIED_CHINESE)
            assertEquals("运行中 · 12 分钟无新事件", stateLine(sessionStateOf(run, Liveness(true, local = true), StateLimits(), now)))
            assertEquals("失联 · 最后已知 运行中 · 7 分钟前", stateLine(sessionStateOf(run, lost, StateLimits(), now)))
        } finally {
            Locale.setDefault(keep)
        }
    }

    @Test fun lostWhenThisPhoneLostTheRelay() {
        // Readable as last listed, but not heard from for the lost limit: not known, though not unreachable.
        val running = Session(sessionId = "r", machineId = "m", status = SessionStatus.RUNNING, updatedAt = now.toString())
        val w = World(machines = listOf(Machine("m", "vps", online = true, heardAt = Instant.now().minusSeconds(120).toString())), sessions = mapOf("r" to running))
        assertEquals(SessionStatus.UNKNOWN, w.shown(running).status)
        assertEquals(SessionLink.LOST, w.view(running).link)
    }

    @Test fun theLastLostSpan() {
        val m = Machine("m", lostSpans = listOf(LostSpan("2026-11-19T10:00:00Z", "2026-11-19T10:05:00Z"), LostSpan("2026-11-19T11:02:00Z", "2026-11-19T11:20:00Z")))
        val keep = Locale.getDefault()
        try {
            Locale.setDefault(Locale.SIMPLIFIED_CHINESE)
            assertEquals("曾失联 11:02–11:20", lostSpanText(m, Session(createdAt = "2026-11-19T11:00:00Z"), ZoneOffset.UTC))
            assertEquals("", lostSpanText(m.copy(lostSpans = listOf(LostSpan("2026-11-19T10:00:00Z", "2026-11-19T10:05:00Z"))), Session(createdAt = "2026-11-19T11:00:00Z"), ZoneOffset.UTC))
            assertEquals("失联自 11:02", lostSpanText(m.copy(lostSpans = listOf(LostSpan("2026-11-19T11:02:00Z"))), Session(createdAt = "2026-11-19T11:00:00Z"), ZoneOffset.UTC))
        } finally {
            Locale.setDefault(keep)
        }
    }
}
