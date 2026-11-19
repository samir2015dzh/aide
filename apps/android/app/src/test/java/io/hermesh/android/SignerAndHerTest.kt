package io.hermesh.android

import io.hermesh.android.her.HerBot
import io.hermesh.android.her.HerConversation
import io.hermesh.android.her.HerLine
import io.hermesh.android.her.HerMemory
import io.hermesh.android.signer.Card
import io.hermesh.android.signer.Cards
import io.hermesh.android.signer.Expectation
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class CardsTest {
    @Test fun cardsAsTheCoreSendsThem() {
        // cmd/hermesh-mobile/android: preview.Card as JSON, empty fields left out.
        val cards = Cards.parse("""[{"title":"Approve a machine","code":"1234 5678","lines":["Machine: mac"]},{"title":"Join","own":true}]""")
        assertEquals(Card(title = "Approve a machine", code = "1234 5678", lines = listOf("Machine: mac")), cards[0])
        assertTrue(cards[1].own)
    }

    @Test fun expectationsSkipOnlyWhatThePersonConfirmed() {
        val machine = listOf(Card(title = "Approve a machine", code = "1234 5678"))
        assertTrue(Expectation.Code("1234 5678").matches(machine))
        assertFalse(Expectation.Code("9999 0000").matches(machine))
        assertFalse(Expectation.Code("").matches(listOf(Card(title = "x"))))
        assertTrue(Expectation.Own.matches(listOf(Card(title = "Join", own = true), Card(title = "Key", own = true))))
        assertFalse(Expectation.Own.matches(listOf(Card(title = "Join", own = true), Card(title = "Grant"))))
        assertFalse(Expectation.Own.matches(emptyList()))
    }

    @Test fun messagesAndSignaturesAsBase64() {
        // Go's json.Marshal of [][]byte.
        val msgs = Cards.messages("""["b25l","AAEC/w=="]""")
        assertEquals("one", msgs[0].decodeToString())
        assertEquals(listOf(0, 1, 2, 255), msgs[1].map { it.toInt() and 0xff })
        assertEquals("""["b25l","AAEC/w=="]""", Cards.signatures(msgs))
    }
}

class HerTest {
    private fun j(s: String): JsonElement = Json.parseToJsonElement(s)

    private val snapshot = j(
        """{"session":{"session_id":"hs1","model":"openai/gpt"},"last_seq":10,
           "transcript":{"items":[
             {"type":"message","message_id":"u1","message":{"role":"user","content":[{"type":"text","text":"remind me at 9"}]}},
             {"type":"message","message_id":"a1","message":{"role":"assistant","content":[{"type":"thinking","text":"hm"},{"type":"tool_call","id":"t1","name":"send_to_user","arguments":{"text":"Done: 9:00 every day."}},{"type":"tool_call","id":"t2","name":"routine_set","arguments":{}}]}},
             {"type":"message","message_id":"u2","mail":{"kind":"routine"},"message":{"role":"user","content":[{"type":"text","text":"routine ran"}]}}
           ]},
           "active_task":{"task_id":"k1","approvals":[{"call_id":"t9","name":"shell","summary":"rm -rf build"}]}}""",
    )

    @Test fun theConversationAsThePanelShowsIt() {
        val c = HerConversation(snapshot)
        assertEquals("hs1", c.sessionId)
        assertEquals(listOf(HerLine.Owner("u1", "remind me at 9"), HerLine.Her("t1", "Done: 9:00 every day.")), c.lines())
        assertTrue(c.working)
        assertEquals("rm -rf build", c.approvals.single().summary)
        c.apply(j("""{"seq":11,"type":"approval_resolved","call_id":"t9","decision":{"type":"approve"}}"""))
        assertTrue(c.approvals.isEmpty())
        c.apply(j("""{"seq":12,"type":"message_completed","message_id":"a2","message":{"role":"assistant","content":[{"type":"tool_call","id":"t3","name":"send_to_user","arguments":{"text":"Cleaned."}}]}}"""))
        c.apply(j("""{"seq":12,"type":"message_completed","message_id":"a2","message":{"role":"assistant","content":[]}}""")) // seen
        c.apply(j("""{"seq":13,"type":"task_completed"}"""))
        assertFalse(c.working)
        assertEquals(HerLine.Her("t3", "Cleaned."), c.lines().last())
        c.apply(j("""{"seq":14,"type":"task_failed","error":{"message":"no model"}}"""))
        assertEquals("no model", (c.lines().last() as HerLine.Failed).text)
        assertEquals(14L, c.lastSeq)
    }

    @Test fun botsRoutinesAndMemory() {
        val bots = HerBot.list(j("""{"bots":[{"id":"b1","name":"Her","default":true,"unread":2,"routines":[{"id":"r1","name":"Morning","when":"every day at 9","state":"active","next_run":"2026-11-17T09:00:00Z","last_run":{"outcome":"reported"}}]},{"id":"b2","name":"Scout"}]}"""))
        assertEquals(listOf("b1", "b2"), bots.map { it.id })
        val r = bots[0].routines.single()
        assertEquals("every day at 9", r.schedule)
        assertEquals("reported", r.lastOutcome)
        val merged = HerBot.merge(bots, j("""{"bots":[{"id":"b1","name":"Her 2","default":true}],"deleted":["b2"]}"""))
        assertEquals(listOf("Her 2"), merged.map { it.name })
        assertEquals(HerMemory("likes tea", listOf("people")), HerMemory.parse(j("""{"text":"likes tea","pages":[{"name":"people","path":"/m/people.md"}]}""")))
    }
}
