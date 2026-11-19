package io.hermesh.android

import io.hermesh.android.api.ConnectClient
import io.hermesh.android.api.Notice
import io.hermesh.android.api.WatchResponse
import io.hermesh.android.connection.Link
import io.hermesh.android.connection.link
import io.hermesh.android.model.World
import io.hermesh.android.push.LiveNotices
import io.hermesh.android.push.show
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import java.io.File
import java.time.Instant
import java.util.Locale

/**
 * The notices a real core's Watch gives, as the app follows it: the Watch
 * of e2e TestAndroidCore (the bound core joined to an owner Mac through a
 * relay; an agent asks to connect, starts sessions that ask and fail, the
 * owner starts one, the relay goes away and comes back while a session
 * asks), recorded as the app reads it (watch-recorded.jsonl; a fresh one
 * from HERMESH_ANDROID_WATCH_RECORD when set), played through World and
 * LiveNotices as AppModel does.
 */
class LiveNoticesReplayTest {
    private lateinit var locale: Locale

    @Before fun english() {
        locale = Locale.getDefault()
        Locale.setDefault(Locale.ENGLISH)
    }

    @After fun restore() = Locale.setDefault(locale)

    private data class Step(val shown: List<Notice>, val withdrawn: List<String>, val link: Link)

    @Test fun aRecordedWatch() {
        val lines = (System.getenv("HERMESH_ANDROID_WATCH_RECORD")?.takeIf { it.isNotEmpty() }?.let { File(it).readLines() }
            ?: javaClass.getResource("/watch-recorded.jsonl")!!.readText().lines()).filter { it.isNotBlank() }
        val live = LiveNotices()
        var w = World()
        val steps = mutableListOf<Step>()
        for (line in lines) {
            val o = ConnectClient.json.parseToJsonElement(line).jsonObject
            w = w.apply(ConnectClient.json.decodeFromJsonElement(WatchResponse.serializer(), o["watch"]!!))
            if (!w.watched) continue
            val c = live.look(w, Instant.parse(o["at"]!!.jsonPrimitive.content))
            steps += Step(c.show, c.withdraw, link(running = true, online = true, relayConnected = w.status.relayConnected))
        }
        val shown = steps.flatMap { it.shown }
        val withdrawn = steps.flatMap { it.withdrawn }

        // The agent's request to connect, once; taken back once approved.
        val pairing = shown.filter { it.kind == "pairing" }
        assertEquals(1, pairing.size)
        assertEquals("“Muse” wants to connect to your computer", show(pairing[0]).title)
        assertTrue(pairing[0].pairingCode.isNotEmpty() && withdrawn.contains("pairing"))

        // Muse's three sessions are news; the owner's own is not.
        val started = shown.filter { it.kind == "session_started" }
        assertEquals(3, started.size)
        started.forEach { assertEquals("Muse", it.grant) }
        assertTrue(show(started[0]).body.startsWith("Muse started it · owner-mac"))
        val owners = w.sessions.values.filter { it.createdBy == "owner" }.map { it.sessionId }
        assertEquals(1, owners.size)
        assertTrue(started.none { it.sessionId in owners })

        // Its questions, each once; the first answered on the phone, so taken back.
        val questions = shown.filter { it.kind == "question" }
        assertEquals(listOf("Which color?", "Which shade?"), questions.map { show(it).body })
        assertTrue(withdrawn.contains(questions[0].sessionId))
        // One failure, of Muse's failing session.
        val failures = shown.filter { it.kind == "failure" }
        assertEquals(1, failures.size)
        assertTrue(failures[0].sessionId in started.map { it.sessionId })

        // The relay went away and came back: the app showed it reconnecting,
        // and the question asked meanwhile came once it was connected again.
        val away = steps.indexOfFirst { it.link == Link.Reconnecting }
        val back = steps.withIndex().indexOfFirst { (i, s) -> i > away && s.link == Link.Connected }
        val shade = steps.indexOfFirst { s -> s.shown.any { it.kind == "question" && it.sessionId == questions[1].sessionId } }
        assertTrue("away $away, back $back, question $shade", away in 0 until back && shade >= back)
    }
}
