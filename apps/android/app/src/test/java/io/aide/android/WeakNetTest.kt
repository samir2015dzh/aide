package io.aide.android

import io.aide.android.api.AgentDraft
import io.aide.android.api.AgentMessage
import io.aide.android.api.Answer
import io.aide.android.api.ApiError
import io.aide.android.api.AttachmentIn
import io.aide.android.api.QuestionResolved
import io.aide.android.api.SessionEvent
import io.aide.android.api.UserMessage
import io.aide.android.model.Item
import io.aide.android.model.OutboxCodec
import io.aide.android.model.OutboxState
import io.aide.android.model.OutboxStatus
import io.aide.android.model.SessionFeed
import io.aide.android.model.Transcript
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.IOException
import java.nio.file.Files

class WeakNetTest {
    @Test
    fun outboxPersistsRecoversOnBootAndPreservesFifoOrder() {
        val dir = Files.createTempDirectory("aide-outbox-test").toFile()
        try {
            val file = dir.resolve("outbox.json")
            var st = OutboxState()
            val (s1, m1) = OutboxCodec.enqueue(st, "sess-1", "first offline", id = "cmid-1", now = "2026-11-25T00:00:01Z")
            val (s2, m2) = OutboxCodec.enqueue(s1, "sess-1", "second offline", id = "cmid-2", now = "2026-11-25T00:00:02Z")
            val (s3, m3) = OutboxCodec.enqueue(s2, "sess-2", "other session", id = "cmid-3", now = "2026-11-25T00:00:03Z")
            st = OutboxCodec.markSending(s3, m1.id)

            assertEquals(1L, m1.clientSeq)
            assertEquals(2L, m2.clientSeq)
            assertEquals(1L, m3.clientSeq)
            // While m1 is sending, m2 must wait behind it while sess-2 is still ready.
            assertEquals(listOf("cmid-3"), OutboxCodec.nextReady(st).map { it.id })

            OutboxCodec.save(file, st)

            // Simulate app kill + relaunch: SENDING becomes PENDING with same idempotency id.
            val reloaded = OutboxCodec.recoverOnBoot(OutboxCodec.load(file), "2026-11-25T00:01:00Z")
            val s1Items = OutboxCodec.forSession(reloaded, "sess-1")
            assertEquals(listOf("cmid-1", "cmid-2"), s1Items.map { it.id })
            assertEquals(listOf(OutboxStatus.PENDING, OutboxStatus.PENDING), s1Items.map { it.status })
            assertEquals(listOf("cmid-1"), OutboxCodec.nextReady(reloaded, "sess-1").map { it.id })

            // Enqueuing after restart continues monotonic clientSeq (#3).
            val (s4, m4) = OutboxCodec.enqueue(reloaded, "sess-1", "third after boot", id = "cmid-4")
            assertEquals(3L, m4.clientSeq)

            // Permanent failure blocks later messages until retried or removed.
            val failed = OutboxCodec.markError(s4, "cmid-1", "session archived", transient = false)
            assertEquals(OutboxStatus.FAILED, OutboxCodec.forSession(failed, "sess-1").first().status)
            assertTrue(OutboxCodec.nextReady(failed, "sess-1").isEmpty())

            val retried = OutboxCodec.retry(failed, "cmid-1")
            assertEquals(listOf("cmid-1"), OutboxCodec.nextReady(retried, "sess-1").map { it.id })

            val sent1 = OutboxCodec.markSent(retried, "cmid-1")
            assertEquals(listOf("cmid-2"), OutboxCodec.nextReady(sent1, "sess-1").map { it.id })

            // Reconcile drops SENT once the machine's UserMessage echoing its id appears in the stream.
            val reconciled = OutboxCodec.reconcileWithEvents(sent1, "sess-1", listOf(SessionEvent(seq = 1, userMessage = UserMessage("first offline", clientMessageIds = listOf("cmid-1")))))
            assertEquals(listOf("cmid-2", "cmid-4"), OutboxCodec.forSession(reconciled, "sess-1").map { it.id })
        } finally {
            dir.deleteRecursively()
        }
    }

    @Test
    fun transientErrorClassification() {
        assertTrue(OutboxCodec.isTransientError(ApiError("unavailable", "offline")))
        assertTrue(OutboxCodec.isTransientError(ApiError("deadline_exceeded", "timed out")))
        assertFalse(OutboxCodec.isTransientError(ApiError("failed_precondition", "session is archived")))
        assertFalse(OutboxCodec.isTransientError(ApiError("permission_denied", "forbidden")))
        // As on the desktop: a network exception is transient, any other exception is not.
        assertTrue(OutboxCodec.isTransientError(IOException("connection refused")))
        assertFalse(OutboxCodec.isTransientError(IllegalStateException("boom")))
        assertFalse(OutboxCodec.isTransientError(ApiError("", "something else")))
    }

    private fun user(seq: Long, text: String, vararg ids: String) =
        SessionEvent(seq = seq, userMessage = UserMessage(text, clientMessageIds = ids.toList()))

    @Test
    fun reconcileByEchoedIdWhateverTheRecordedText() {
        var st = OutboxState()
        st = OutboxCodec.enqueue(st, "s1", "look", listOf(AttachmentIn("notes.txt", "text/plain", "aGVsbG8=")), id = "m1", afterSeq = 3).first
        st = OutboxCodec.enqueue(st, "s1", "!fix", id = "m2", afterSeq = 3).first
        st = OutboxCodec.enqueue(st, "s1", "red", id = "m3", afterSeq = 3).first
        for (id in listOf("m1", "m2", "m3")) st = OutboxCodec.markSent(OutboxCodec.markSending(st, id), id)
        val events = listOf(
            user(4, "look\n\n[Aide] Attached files, saved on this machine outside the worktree:\n- notes.txt", "m1"),
            user(5, "Fix the failing test, then run it again.", "m2"),
        )
        st = OutboxCodec.reconcileWithEvents(st, "s1", events)
        assertEquals(listOf("m3"), OutboxCodec.forSession(st, "s1").map { it.id })
        // An answer to the agent's question records QuestionResolved, not UserMessage.
        val answer = SessionEvent(seq = 6, questionResolved = QuestionResolved(questionId = "q1", answers = listOf(Answer(text = "Red")), clientMessageIds = listOf("m3")))
        st = OutboxCodec.reconcileWithEvents(st, "s1", events + answer)
        assertTrue(OutboxCodec.forSession(st, "s1").isEmpty())
    }

    @Test
    fun olderIdenticalTextDoesNotClearNewMarker() {
        var st = OutboxCodec.enqueue(OutboxState(), "s1", "continue", id = "m1", afterSeq = 10).first
        st = OutboxCodec.markSent(st, "m1")
        // A machine that echoes ids: an older "continue" is not this message.
        st = OutboxCodec.reconcileWithEvents(st, "s1", listOf(user(2, "continue"), user(8, "continue", "older")))
        assertEquals(listOf("m1"), OutboxCodec.forSession(st, "s1").map { it.id })
        // An older machine (no ids anywhere): only events after the one it was enqueued behind.
        val old = listOf(user(2, "continue"), user(8, "continue"))
        st = OutboxCodec.reconcileWithEvents(st, "s1", old)
        assertEquals(listOf("m1"), OutboxCodec.forSession(st, "s1").map { it.id })
        st = OutboxCodec.reconcileWithEvents(st, "s1", old + user(11, "continue"))
        assertTrue(OutboxCodec.forSession(st, "s1").isEmpty())
    }

    @Test
    fun attachmentBytesDroppedOnceDeliveredAndBoundedOnDisk() {
        val dir = Files.createTempDirectory("aide-outbox-att").toFile()
        try {
            val file = dir.resolve("outbox.json")
            var st = OutboxState()
            st = OutboxCodec.enqueue(st, "s1", "small", listOf(AttachmentIn("a.txt", "text/plain", "aGVsbG8=")), id = "m1", now = "2026-11-25T00:00:01Z").first
            st = OutboxCodec.enqueue(st, "s1", "big", listOf(AttachmentIn("b.bin", "", "A".repeat(100))), id = "m2", now = "2026-11-25T00:00:02Z").first
            st = OutboxCodec.enqueue(st, "s1", "after", id = "m3", now = "2026-11-25T00:00:03Z").first

            val (persisted, memoryOnly) = OutboxCodec.forPersist(st, 50)
            assertEquals(setOf("m2"), memoryOnly)
            assertEquals(100, st.items[1].attachments[0].data.length)
            assertEquals(setOf<String>(), OutboxCodec.save(file, st)) // within the default bound
            val restored = OutboxCodec.settleLoaded(OutboxCodec.normalize(persisted))
            val list = OutboxCodec.forSession(restored, "s1")
            assertEquals(listOf(OutboxStatus.PENDING, OutboxStatus.FAILED, OutboxStatus.PENDING), list.map { it.status })
            assertTrue(list[1].attachmentsLost)
            assertEquals(OutboxStatus.FAILED, OutboxCodec.forSession(OutboxCodec.retry(restored, "m2"), "s1")[1].status)

            val sent = OutboxCodec.markSent(st, "m1")
            assertEquals("", OutboxCodec.forSession(sent, "s1")[0].attachments[0].data)
            OutboxCodec.save(file, sent)
            assertFalse(file.readText().contains("aGVsbG8="))
        } finally {
            dir.deleteRecursively()
        }
    }

    @Test
    fun sessionEventCacheKeepsTheTenWrittenLast() {
        val dir = Files.createTempDirectory("aide-events").toFile()
        try {
            val files = (1..12).map { i -> dir.resolve("s$i.json").apply { writeText("[]"); setLastModified(1_000_000L * i) } }
            SessionFeed.prune(dir, files[0])
            val left = dir.listFiles().orEmpty().map { it.name }.toSet()
            assertEquals(SessionFeed.MAX_CACHED_SESSIONS, left.size)
            assertTrue("s1.json" in left) // the one just written
            assertTrue("s12.json" in left)
            assertFalse("s2.json" in left)
        } finally {
            dir.deleteRecursively()
        }
    }

    @Test
    fun transcriptReconnectCatchUpDeduplicatesOverlapAndPreservesDrafts() {
        val tr = Transcript()
        tr.add(SessionEvent(seq = 1, userMessage = UserMessage("hello")))
        tr.add(SessionEvent(seq = 2, agentMessage = AgentMessage("part 1")))
        assertEquals(2L, tr.lastSeq)

        // Simulate reconnect replay overlapping seq 2, then catching up 3 & 4 plus live draft.
        tr.add(SessionEvent(seq = 2, agentMessage = AgentMessage("part 1")))
        tr.add(SessionEvent(seq = 3, userMessage = UserMessage("follow up")))
        tr.add(SessionEvent(agentDraft = AgentDraft(text = "typing...", whole = true)))
        tr.add(SessionEvent(agentDraft = AgentDraft(text = " more", whole = false)))

        val midItems = tr.items()
        assertEquals(4, midItems.size)
        assertTrue(midItems.last() is Item.Draft)
        assertEquals("typing... more", (midItems.last() as Item.Draft).text)

        // Stored final event clears draft and advances lastSeq.
        tr.add(SessionEvent(seq = 4, agentMessage = AgentMessage("typing... more done")))
        assertEquals(4L, tr.lastSeq)
        val finalItems = tr.items()
        assertEquals(4, finalItems.size)
        assertEquals(listOf("e1", "e2", "e3", "e4"), finalItems.map { it.key })
    }
}
