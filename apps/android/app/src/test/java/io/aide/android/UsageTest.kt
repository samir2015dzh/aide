package io.aide.android

import io.aide.android.api.ConnectClient
import io.aide.android.api.QueryWorkResponse
import io.aide.android.api.TurnCount
import io.aide.android.api.WatchResponse
import io.aide.android.api.WorkGroup
import io.aide.android.model.UsageGroup
import io.aide.android.model.World
import io.aide.android.model.tokensText
import io.aide.android.model.usageParts
import io.aide.android.model.usageRequest
import io.aide.android.model.usageSince
import io.aide.android.model.usageTotals
import java.time.LocalDate
import java.time.ZoneId
import java.time.temporal.ChronoUnit
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.encodeToJsonElement
import kotlinx.serialization.json.jsonObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** The usage card's grouping (工单 #142): by coding agent or by machine, the same rules and cases as the desktop's (packages/core usage.test.ts). */
class UsageTest {
    // Tuesday 20 October 2026.
    private val today = LocalDate.of(2026, 10, 20)
    private val since = usageSince(today)
    private fun turn(date: String, agent: String, machine: String, tokens: Long) =
        TurnCount(bucket = ChronoUnit.DAYS.between(since, LocalDate.parse(date)).toInt(), group = WorkGroup(agent = agent, machineId = machine), inputTokens = tokens)

    @Test
    fun aYearOfWeeksAskedByAgentAndMachineInOneQuestion() {
        assertEquals(LocalDate.of(2025, 10, 19), since)
        val q = usageRequest(today, ZoneId.of("Asia/Shanghai"))
        assertEquals("2025-10-19", q.firstDay)
        assertEquals(367, q.days)
        assertEquals("WORK_BUCKET_DAY", q.bucket)
        assertEquals(listOf("WORK_DIMENSION_AGENT", "WORK_DIMENSION_MACHINE"), q.groupBy)
        // As protojson: lowerCamelCase, enums by name.
        val json = ConnectClient.json.encodeToJsonElement(q).jsonObject
        assertEquals("\"Asia/Shanghai\"", json["timeZone"].toString())
        assertEquals("[\"WORK_DIMENSION_AGENT\",\"WORK_DIMENSION_MACHINE\"]", json["groupBy"].toString())
    }

    @Test
    fun byAgentAndByMachineThePartsAddUpToTheYearAndADayToTheDay() {
        val res = QueryWorkResponse(
            turns = listOf(
                turn("2026-10-20", "claude", "mac", 400),
                turn("2026-10-20", "claude", "vps", 300),
                turn("2026-10-20", "codex", "vps", 100),
                turn("2026-10-19", "gemini", "mac", 200),
                turn("2026-09-10", "codex", "mini", 150),
            ),
        )
        val totals = usageTotals(res, today)
        assertEquals(1150L, totals.total)
        assertEquals(800L, totals.today)
        val agents = usageParts(res, today, UsageGroup.Agent)
        assertEquals(listOf("claude" to 700L, "codex" to 250L, "gemini" to 200L), agents.map { it.id to it.tokens })
        val machines = usageParts(res, today, UsageGroup.Machine)
        assertEquals(listOf("mac" to 600L, "vps" to 400L, "mini" to 150L), machines.map { it.id to it.tokens })
        for (parts in listOf(agents, machines)) {
            assertEquals(totals.total, parts.sumOf { it.tokens })
            assertEquals(1.0, parts.sumOf { it.share }, 1e-9)
            assertEquals(totals.today, parts.sumOf { it.days[today] ?: 0L })
        }
        assertEquals(400L, machines.first { it.id == "vps" }.days[today])
        assertTrue(usageParts(null, today, UsageGroup.Machine).isEmpty())
    }

    @Test
    fun pastThePartsNamedTheLeastBusyAreOtherAndStillAddUp() {
        val agents = listOf("claude", "codex", "gemini", "cursor", "qwen", "opencode", "copilot")
        val res = QueryWorkResponse(turns = agents.mapIndexed { i, a -> turn("2026-10-20", a, "mac", 70L - i * 10) })
        val parts = usageParts(res, today, UsageGroup.Agent)
        assertEquals(listOf("claude", "codex", "gemini", "cursor", ""), parts.map { it.id })
        assertTrue(parts.last().other)
        assertEquals(60L, parts.last().tokens)
        assertEquals(60L, parts.last().days[today])
        assertEquals(usageTotals(res, today).total, parts.sumOf { it.tokens })
        assertFalse(usageParts(QueryWorkResponse(turns = res.turns.take(5)), today, UsageGroup.Agent).any { it.other })
    }

    @Test
    fun theAnswerAndTheWorkVersionAreReadAsProtojsonHasThem() {
        val res: QueryWorkResponse = ConnectClient.json.decodeFromJsonElement(
            ConnectClient.json.parseToJsonElement(
                """{"turns":[{"bucket":366,"group":{"agent":"claude","machineId":"m1"},"turns":2,"inputTokens":"1200","outputTokens":"34"}],"sessions":[{"bucket":366,"group":{"agent":"claude","machineId":"m1"},"sessions":1}],"sources":[{"machineId":"m1","live":true}]}""",
            ),
        )
        assertEquals(1234L, usageTotals(res, today).today)
        assertEquals("m1", usageParts(res, today, UsageGroup.Machine).single().id)
        // The Watch says when the counts moved; a message without it keeps the last.
        val w: WatchResponse = ConnectClient.json.decodeFromJsonElement(ConnectClient.json.parseToJsonElement("""{"workVersion":"7"}"""))
        val world = World().apply(w)
        assertEquals(7L, world.workVersion)
        assertEquals(7L, world.apply(WatchResponse()).workVersion)
    }

    @Test
    fun tokensAreSaidAsTheDesktopSaysThem() {
        assertEquals("950", tokensText(950))
        assertEquals("12K", tokensText(12_345))
        assertEquals("3.4M", tokensText(3_400_000))
        assertEquals("1.2M", tokensText(1_250_000))
    }
}
