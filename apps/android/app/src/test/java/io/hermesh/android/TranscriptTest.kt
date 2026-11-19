package io.hermesh.android

import io.hermesh.android.api.AgentDraft
import io.hermesh.android.api.AgentMessage
import io.hermesh.android.api.Question
import io.hermesh.android.api.QuestionAsked
import io.hermesh.android.api.QuestionResolved
import io.hermesh.android.api.SessionEvent
import io.hermesh.android.api.ToolCall
import io.hermesh.android.api.ToolResult
import io.hermesh.android.api.TurnCompleted
import io.hermesh.android.api.Failure
import io.hermesh.android.api.UserMessage
import io.hermesh.android.model.Item
import io.hermesh.android.model.Transcript
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class TranscriptTest {
    @Test fun aTurnAsThePageShowsIt() {
        val t = Transcript()
        t.add(SessionEvent(seq = 1, userMessage = UserMessage("fix the test")))
        t.add(SessionEvent(agentDraft = AgentDraft("Look", whole = true)))
        t.add(SessionEvent(agentDraft = AgentDraft("ing…")))
        assertEquals(Item.Draft("draft", "Looking…"), t.items().last())
        t.add(SessionEvent(seq = 2, toolCall = ToolCall("c1", name = "shell", summary = "go test")))
        assertTrue("a stored event ends the draft", t.items().none { it is Item.Draft })
        t.add(SessionEvent(seq = 3, toolCall = ToolCall("c2", name = "edit", summary = "a.go")))
        t.add(SessionEvent(seq = 4, toolResult = ToolResult("c1", summary = "FAIL", isError = true)))
        t.add(SessionEvent(seq = 5, agentMessage = AgentMessage("Fixed.")))
        t.add(SessionEvent(seq = 5, agentMessage = AgentMessage("Fixed.")))  // the stream again after a reconnect
        val items = t.items()
        assertEquals(3, items.size)
        val tools = items[1] as Item.Tools
        assertEquals(listOf("shell", "edit"), tools.calls.map { it.name })
        assertEquals("FAIL", tools.calls[0].result)
        assertTrue(tools.calls[0].isError)
        assertEquals(Item.Agent("e5", "Fixed."), items[2])
        assertEquals(5L, t.lastSeq)
    }

    @Test fun questionsAndFailures() {
        val t = Transcript()
        t.add(SessionEvent(seq = 1, questionAsked = QuestionAsked(Question("q1"))))
        assertEquals(null, (t.items().single() as Item.Asked).resolution)
        t.add(SessionEvent(seq = 2, questionResolved = QuestionResolved("q1", "QUESTION_RESOLUTION_ANSWERED")))
        assertEquals("QUESTION_RESOLUTION_ANSWERED", (t.items().single() as Item.Asked).resolution)
        t.add(SessionEvent(seq = 3, turnCompleted = TurnCompleted(result = "done")))
        assertEquals(1, t.items().size)
        t.add(SessionEvent(seq = 4, turnCompleted = TurnCompleted(isError = true, failure = Failure(message = "rate limited"))))
        assertEquals(Item.TurnEnd("e4", true, "rate limited"), t.items().last())
    }
}
