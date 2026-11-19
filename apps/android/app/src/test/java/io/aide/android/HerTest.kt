package io.aide.android

import io.aide.android.her.HerBot
import io.aide.android.her.HerConversation
import io.aide.android.her.HerLine
import io.aide.android.her.HerMemory
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class HerTest {
    private fun j(s: String): JsonElement = Json.parseToJsonElement(s)

    private val snapshot = j(
        """{"session":{"session_id":"hs1","model":"openai/gpt"},"last_seq":10,
           "transcript":{"items":[
             {"type":"message","message_id":"u1","message":{"role":"user","content":[{"type":"text","text":"remind me at 9"}]}},
             {"type":"message","message_id":"a1","message":{"role":"assistant","content":[{"type":"thinking","text":"hm"},{"type":"tool_call","id":"t1","name":"send_to_user","arguments":{"text":"Done: 9:00 every day."}},{"type":"tool_call","id":"t2","name":"routine_set","arguments":{}}]}},
             {"type":"message","message_id":"u2","mail":{"kind":"routine"},"message":{"role":"user","content":[{"type":"text","text":"routine ran"}]}}
           ]},
           "active_task":{"task_id":"k1"}}""",
    )

    @Test fun theConversationAsThePanelShowsIt() {
        val c = HerConversation(snapshot)
        assertEquals("hs1", c.sessionId)
        assertEquals(listOf(HerLine.Owner("u1", "remind me at 9"), HerLine.Her("t1", "Done: 9:00 every day.")), c.lines())
        assertTrue(c.working)
        // Her acts without asking: an approval event (from an older her) changes nothing shown.
        c.apply(j("""{"seq":11,"type":"approval_requested","call_id":"t9","name":"shell","summary":"rm -rf build"}"""))
        assertTrue(c.working)
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
