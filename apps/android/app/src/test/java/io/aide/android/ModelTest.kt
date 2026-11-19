package io.aide.android

import io.aide.android.api.Client
import io.aide.android.api.Machine
import io.aide.android.api.PairingDetails
import io.aide.android.api.Question
import io.aide.android.api.QuestionItem
import io.aide.android.api.QuestionOption
import io.aide.android.api.Session
import io.aide.android.api.SessionProgress
import io.aide.android.api.SessionStatus
import io.aide.android.api.WatchResponse
import io.aide.android.api.Answer
import io.aide.android.model.ConnectAddress
import io.aide.android.model.PairingLink
import io.aide.android.model.World
import io.aide.android.model.buildAnswers
import io.aide.android.model.choose
import io.aide.android.model.serverURL
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class WorldTest {
    private fun s(id: String, status: String, at: String, by: String = "c_1") = Session(sessionId = id, status = status, updatedAt = at, createdBy = by)

    @Test fun resetReplacesAllButSessions() {
        var w = World().apply(WatchResponse(reset = true, machines = listOf(Machine("m_1", "b")), pairings = listOf(PairingDetails(code = "P1")),
            sessions = listOf(s("old", SessionStatus.IDLE, "2026-01-01T00:00:00Z"))))
        w = w.apply(WatchResponse(machines = listOf(Machine("m_2", "a")), removedPairingCodes = listOf("P1")))
        assertEquals(listOf("a", "b"), w.machines.map { it.machineName })
        assertEquals(emptyList<PairingDetails>(), w.pairings)
        // Again from the start: the machines it lists alone, the sessions loaded kept.
        w = w.apply(WatchResponse(reset = true, machines = listOf(Machine("m_2", "a")), sessions = listOf(s("new", SessionStatus.RUNNING, "2026-01-02T00:00:00Z"))))
        assertEquals(listOf("m_2"), w.machines.map { it.machineId })
        assertEquals(setOf("old", "new"), w.sessions.keys)
        w = w.apply(WatchResponse(deletedSessionIds = listOf("old")))
        assertEquals(setOf("new"), w.sessions.keys)
    }

    @Test fun aLaterStateWins() {
        var w = World().apply(WatchResponse(sessions = listOf(s("x", SessionStatus.RUNNING, "2026-01-02T00:00:00.5Z"))))
        w = w.apply(WatchResponse(sessions = listOf(s("x", SessionStatus.IDLE, "2026-01-02T00:00:00.25Z")))) // older (shorter fraction)
        assertEquals(SessionStatus.RUNNING, w.sessions["x"]!!.status)
        w = w.apply(WatchResponse(sessions = listOf(s("x", SessionStatus.IDLE, "2026-01-02T00:00:01Z"))))
        assertEquals(SessionStatus.IDLE, w.sessions["x"]!!.status)
    }

    @Test fun bucketsAndWhoSentIt() {
        val asking = s("q", SessionStatus.RUNNING, "2026-01-01T00:00:03Z").copy(progress = SessionProgress(pendingQuestion = Question("q1")))
        val w = World(
            clients = listOf(Client(clientId = "c_1", label = "Muse"), Client(clientId = "c_her", label = "her", herMachineId = "m_1")),
            sessions = listOf(
                asking, s("r", SessionStatus.RUNNING, "2026-01-01T00:00:02Z"), s("i", SessionStatus.IDLE, "2026-01-01T00:00:04Z"),
                s("a", SessionStatus.ARCHIVED, "2026-01-01T00:00:05Z"), s("w", SessionStatus.WAITING_INPUT, "2026-01-01T00:00:01Z", by = "owner"),
            ).associateBy { it.sessionId },
        )
        val b = w.buckets
        assertEquals(listOf("q", "w"), b.needsYou.map { it.sessionId })
        assertEquals(listOf("r"), b.inProgress.map { it.sessionId })
        assertEquals(listOf("i"), b.recent.map { it.sessionId })
        assertEquals("Muse", w.senderName(asking))
        assertEquals("Muse · reviewer", w.senderName(asking.copy(createdAs = "reviewer")))
        assertEquals("Her", w.senderName(asking.copy(createdBy = "c_her")))
        assertEquals("You", w.senderName(asking.copy(createdBy = "owner")))
        assertEquals("Disconnected agent", w.senderName(asking.copy(createdBy = "c_gone")))
    }

    // A session whose machine can't be read is neither at work nor asking: its state is unknown, its time kept,
    // until the machine is back.
    @Test fun stateUnknownWhileItsMachineIsAway() {
        val running = s("r", SessionStatus.RUNNING, "2026-01-01T00:00:02Z").copy(machineId = "m_vps")
        val asking = s("q", SessionStatus.WAITING_INPUT, "2026-01-01T00:00:03Z").copy(machineId = "m_vps", progress = SessionProgress(pendingQuestion = Question("q1")))
        val archived = s("a", SessionStatus.ARCHIVED, "2026-01-01T00:00:04Z").copy(machineId = "m_vps")
        val away = World(machines = listOf(Machine("m_vps", "vps", unreachable = "offline")), sessions = listOf(running, asking, archived).associateBy { it.sessionId })
        val b = away.buckets
        assertEquals(emptyList<String>(), b.needsYou.map { it.sessionId } + b.inProgress.map { it.sessionId })
        assertEquals(listOf(SessionStatus.UNKNOWN, SessionStatus.UNKNOWN), b.recent.map { it.status })
        assertEquals("2026-01-01T00:00:02Z", away.shown(running).updatedAt)
        assertEquals(archived, away.shown(archived))
        val back = away.copy(machines = listOf(Machine("m_vps", "vps", online = true)))
        assertEquals(running, back.shown(running))
        assertEquals(listOf("r"), back.buckets.inProgress.map { it.sessionId })
    }
}

class LinksTest {
    @Test fun connectionAddresses() {
        val o = "o_" + "a".repeat(24)
        assertEquals(ConnectAddress("https://mesh.example.com", o), ConnectAddress.parse("mesh.example.com/$o"))
        assertEquals(ConnectAddress("http://203.0.113.7:8080/relay", o), ConnectAddress.parse(" http://203.0.113.7:8080/relay/$o "))
        assertNull(ConnectAddress.parse("mesh.example.com"))
        assertNull(ConnectAddress.parse("mesh.example.com/o_short"))
        assertNull(ConnectAddress.parse("ftp://mesh.example.com/$o"))
    }

    @Test fun pairingCodes() {
        assertEquals("ABCD-EFGH-JK12", PairingLink.code("abcd-efgh-jk12"))
        assertEquals("ABCD-EFGH-JK12", PairingLink.code("hermesh://pair?code=ABCD-EFGH-JK12"))
        assertEquals("ABCD-EFGH-JK12", PairingLink.code("https://hermesh.io/p/ABCD-EFGH-JK12"))
        assertNull(PairingLink.code("https://hermesh.io/x/ABCD-EFGH-JK12"))
        assertNull(PairingLink.code("ABCD"))
    }

    @Test fun serverAddresses() {
        assertEquals("https://mesh.example.com", serverURL("mesh.example.com/"))
        assertEquals("http://203.0.113.7:8080", serverURL("203.0.113.7:8080"))
        assertEquals("http://relay.lan:8080", serverURL("http://relay.lan:8080"))
        assertNull(serverURL("  "))
    }
}

class AnswersTest {
    private val pick = QuestionItem(id = "color", text = "Which?", options = listOf(QuestionOption("红"), QuestionOption("蓝")))
    private val many = QuestionItem(id = "tags", options = listOf(QuestionOption("a"), QuestionOption("b")), multiSelect = true)
    private val note = QuestionItem(id = "note", optional = true)

    @Test fun answersNeedEveryRequiredItem() {
        assertNull(buildAnswers(listOf(pick, note), emptyMap(), emptyMap()))
        assertEquals(listOf(Answer("color", listOf("红"))), buildAnswers(listOf(pick, note), mapOf("color" to setOf("红")), emptyMap()))
        assertEquals(
            listOf(Answer("color", emptyList(), "绿"), Answer("note", emptyList(), "hi")),
            buildAnswers(listOf(pick, note), emptyMap(), mapOf("color" to " 绿 ", "note" to "hi")),
        )
        // Options keep their order, whatever order they were tapped in.
        assertEquals(listOf(Answer("tags", listOf("a", "b"))), buildAnswers(listOf(many), mapOf("tags" to setOf("b", "a")), emptyMap()))
    }

    @Test fun choosing() {
        assertEquals(setOf("蓝"), choose(pick, setOf("红"), "蓝"))
        assertEquals(emptySet<String>(), choose(pick, setOf("红"), "红"))
        assertEquals(setOf("a", "b"), choose(many, setOf("a"), "b"))
        assertEquals(setOf("a"), choose(many, setOf("a", "b"), "b"))
    }
}
