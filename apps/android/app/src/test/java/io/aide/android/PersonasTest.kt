package io.aide.android

import io.aide.android.api.AgentMode
import io.aide.android.api.AgentModel
import io.aide.android.api.AgentStatus
import io.aide.android.api.LocalApi
import io.aide.android.api.Machine
import io.aide.android.api.MachineStatus
import io.aide.android.api.Persona
import io.aide.android.api.Project
import io.aide.android.api.Session
import io.aide.android.api.TalkToPersonaResponse
import io.aide.android.model.World
import io.aide.android.model.personaChoices
import io.aide.android.model.personaColor
import io.aide.android.model.personaGlyph
import io.aide.android.model.personaReady
import io.aide.android.model.talkedText
import io.aide.android.api.ConnectClient
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.Locale

/** Contacts (docs/personas.md): what the phone sends and works out of them. */
class PersonasTest {
    private val world = World(
        machines = listOf(Machine("m_1", "mac", status = MachineStatus(agents = listOf(
            AgentStatus("deepseek", available = true, models = listOf(AgentModel("pro", efforts = listOf("low", "high")), AgentModel("flash", efforts = listOf("max"))), modes = listOf(AgentMode("ask"), AgentMode("code"))),
            AgentStatus("codex", available = false),
        )))),
        projects = listOf(Project("m_1", "demo"), Project("m_1", "claude-only", agents = listOf("claude")), Project("m_2", "far")),
    )

    @Test fun choicesAreTheMachinesForTheAgentPicked() {
        val c = personaChoices(world, Persona(machineId = "m_1", agent = "deepseek"))
        assertEquals(listOf("deepseek"), c.agents.map { it.name }) // codex is not available there
        assertEquals(listOf("demo"), c.projects.map { it.projectId })
        assertEquals(listOf("low", "high", "max"), c.efforts)
        assertEquals(listOf("max"), personaChoices(world, Persona(machineId = "m_1", agent = "deepseek", model = "flash")).efforts)
        // One kept with an agent no longer available still shows it.
        assertEquals(listOf("deepseek", "codex"), personaChoices(world, Persona(machineId = "m_1", agent = "codex")).agents.map { it.name })
        assertNull(personaChoices(world, Persona(machineId = "m_9", agent = "deepseek")).agent)
    }

    @Test fun avatarsAndReadiness() {
        assertEquals("🦊", personaGlyph(Persona(name = "x", icon = "fox")))
        assertEquals("审", personaGlyph(Persona(name = " 审阅员")))
        assertEquals(0xFF3B82F6, personaColor(Persona(color = "#3b82f6")))
        assertEquals(0xFF64748B, personaColor(Persona()))
        assertFalse(personaReady(Persona(name = " ", machineId = "m_1", agent = "a", projectId = "p")))
        assertTrue(personaReady(Persona(name = "A", machineId = "m_1", agent = "a", projectId = "p")))
    }

    @Test fun whatTheAppSaysOnceSent() {
        Locale.setDefault(Locale.CHINA)
        val s = Session(sessionId = "s_1", title = "修登录")
        assertEquals("已发给 小助手（「修登录」）", talkedText("小助手", TalkToPersonaResponse(session = s)))
        assertEquals("小助手 新开了任务「修登录」：原任务已归档", talkedText("小助手", TalkToPersonaResponse(session = s, started = true, reason = "archived")))
        assertEquals("小助手 新开了任务「修登录」", talkedText("小助手", TalkToPersonaResponse(session = s, started = true, reason = "first")))
        Locale.setDefault(Locale.US)
    }

    @Test fun talkingSendsTheContactAndReadsTheSession() = runBlocking {
        val server = MockWebServer()
        server.start()
        try {
            server.enqueue(MockResponse().setBody("""{"session":{"sessionId":"s_m1_a","agent":"deepseek"},"started":true,"reason":"first","persona":{"personaId":"p_1","name":"R","sessionId":"s_m1_a"}}"""))
            val r = LocalApi(server.port.toLong(), "tok").talkToPersona("p_1", "fix it", "k1")
            assertTrue(r.started)
            assertEquals("s_m1_a", r.session!!.sessionId)
            assertEquals("s_m1_a", r.persona!!.sessionId)
            val req = server.takeRequest()
            assertEquals("/aide.local.v1.LocalService/TalkToPersona", req.path)
            val body = ConnectClient.json.parseToJsonElement(req.body.readUtf8()).jsonObject
            assertEquals("p_1", body["personaId"]!!.jsonPrimitive.content)
            assertEquals("fix it", body["text"]!!.jsonPrimitive.content)
            assertEquals("k1", body["idempotencyKey"]!!.jsonPrimitive.content)

            server.enqueue(MockResponse().setBody("""{"persona":{"personaId":"p_2","name":"B","icon":"owl","machineId":"m_1","projectId":"demo","agent":"deepseek","mode":"code"}}"""))
            val kept = LocalApi(server.port.toLong(), "tok").savePersona(Persona(name = "B", icon = "owl", machineId = "m_1", projectId = "demo", agent = "deepseek", mode = "code"))
            assertEquals("p_2", kept!!.personaId)
            val sent = ConnectClient.json.parseToJsonElement(server.takeRequest().body.readUtf8()).jsonObject["persona"]!!.jsonObject
            assertEquals("code", sent["mode"]!!.jsonPrimitive.content)
            assertNull(sent["sessionId"]) // defaults left out
        } finally {
            server.shutdown()
        }
    }
}
