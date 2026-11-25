package io.aide.android

import io.aide.android.api.AgentDraft
import io.aide.android.api.AgentMessage
import io.aide.android.api.ApiError
import io.aide.android.api.SessionEvent
import io.aide.android.api.UserMessage
import io.aide.android.model.Item
import io.aide.android.model.OutboxCodec
import io.aide.android.model.OutboxState
import io.aide.android.model.OutboxStatus
import io.aide.android.model.Transcript
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
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

            // Reconcile drops SENT once confirmed User item appears in stream.
            val reconciled = OutboxCodec.reconcileWithTranscript(sent1, "sess-1", listOf(Item.User("e1", "first offline")))
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
