package io.hermesh.android

import io.hermesh.android.api.ApiError
import io.hermesh.android.api.ConnectClient
import io.hermesh.android.api.LocalApi
import io.hermesh.android.api.SessionEvent
import io.hermesh.android.api.WatchResponse
import kotlinx.coroutines.flow.toList
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okio.Buffer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Before
import org.junit.Test

/** The Connect protocol as connect-go serves it (JSON codec), against a server playing the core. */
class ConnectTest {
    private lateinit var server: MockWebServer

    @Before fun setUp() {
        server = MockWebServer()
        server.start()
    }

    @After fun tearDown() = server.shutdown()

    private fun api() = LocalApi(server.port.toLong(), "tok")

    // What the core sends, from Go's protojson (cmd/hermesh-mobile/android's core, 2026-11-16).
    private val watchJson = """{"reset":true,"status":{"initialized":true,"ownerId":"o_x","relayConnected":true},"machines":[{"machineId":"m_1","machineName":"mac","online":true,"status":{"platform":"darwin/arm64","agents":[{"name":"claude","available":true,"loggedIn":true}],"runningSessions":2},"her":{"enabled":true,"running":true,"unread":3}}],"pairings":[{"kind":"PAIRING_KIND_CLIENT","label":"Muse","expiresAt":"2026-11-16T17:24:05.572Z","verificationCode":"1234 5678","code":"ABCD-EFGH-JKLM"}],"sessions":[{"sessionId":"s_1","machineId":"m_1","agent":"claude","status":"SESSION_STATUS_WAITING_INPUT","createdBy":"c_1","progress":{"pendingQuestion":{"questionId":"q1","items":[{"id":"i1","text":"Which?","options":[{"label":"A"}]}]}},"updatedAt":"2026-11-16T17:24:05.572Z"}],"workVersion":"7"}"""

    @Test fun envelopesRoundTrip() {
        val b = ConnectClient.envelope(0, "{\"a\":1}".encodeToByteArray())
        assertEquals(5 + 7, b.size)
        val (flags, payload) = ConnectClient.readEnvelope(Buffer().write(b))!!
        assertEquals(0, flags)
        assertEquals("{\"a\":1}", payload.decodeToString())
        assertNull(ConnectClient.readEnvelope(Buffer()))
    }

    @Test fun unaryCallSendsTheTokenAndReadsProtojson() = runBlocking {
        server.enqueue(MockResponse().setBody("""{"initialized":true,"ownerId":"o_x","relayUrl":"https://mesh.example","abilities":["her_reply"],"future":{"x":1}}"""))
        val st = api().status()
        assertTrue(st.initialized)
        assertEquals("o_x", st.ownerId)
        assertEquals(listOf("her_reply"), st.abilities)
        val r = server.takeRequest()
        assertEquals("/hermesh.local.v1.LocalService/GetStatus", r.path)
        assertEquals("Bearer tok", r.getHeader("Authorization"))
        assertEquals("1", r.getHeader("Connect-Protocol-Version"))
        assertTrue(r.getHeader("Content-Type")!!.startsWith("application/json"))
        assertEquals("{}", r.body.readUtf8())
    }

    @Test fun unaryErrorIsTheCoresCode() = runBlocking {
        server.enqueue(MockResponse().setResponseCode(400).setBody("""{"code":"invalid_argument","message":"wrong digits"}"""))
        try {
            api().confirmJoin("C", accept = true, match = "12")
            fail("no error")
        } catch (e: ApiError) {
            assertEquals("invalid_argument", e.code)
            assertEquals("wrong digits", e.message)
        }
        val body = server.takeRequest().body.readUtf8()
        val o = ConnectClient.json.parseToJsonElement(body).jsonObject
        assertEquals("12", o["matchCode"]!!.jsonPrimitive.content)
        assertEquals(JsonPrimitive(true), o["accept"])
    }

    @Test fun streamReadsEachMessageUntilTheEnd() = runBlocking {
        val body = Buffer()
            .write(ConnectClient.envelope(0, watchJson.encodeToByteArray()))
            .write(ConnectClient.envelope(0, """{"deletedSessionIds":["s_1"]}""".encodeToByteArray()))
            .write(ConnectClient.envelope(ConnectClient.END_STREAM, "{}".encodeToByteArray()))
        server.enqueue(MockResponse().setHeader("Content-Type", "application/connect+json").setBody(body))
        val got = api().watch().toList()
        assertEquals(2, got.size)
        val w: WatchResponse = got[0]
        assertTrue(w.reset)
        assertEquals("mac", w.machines[0].machineName)
        assertEquals(2, w.machines[0].status!!.runningSessions)
        assertEquals(3, w.machines[0].her!!.unread)
        assertEquals("PAIRING_KIND_CLIENT", w.pairings[0].kind)
        assertEquals("q1", w.sessions[0].progress!!.pendingQuestion!!.questionId)
        assertEquals(listOf("s_1"), got[1].deletedSessionIds)
        val r = server.takeRequest()
        assertEquals("application/connect+json", r.getHeader("Content-Type"))
        val sent = r.body
        val (flags, payload) = ConnectClient.readEnvelope(sent)!!
        assertEquals(0, flags)
        assertEquals("{}", payload.decodeToString())
    }

    @Test fun streamEndsWithTheCoresError() = runBlocking {
        val body = Buffer()
            .write(ConnectClient.envelope(0, """{"seq":"12","ts":"2026-11-16T17:24:05.572Z","toolCall":{"callId":"c1","name":"shell","summary":"go test"}}""".encodeToByteArray()))
            .write(ConnectClient.envelope(ConnectClient.END_STREAM, """{"error":{"code":"resource_exhausted","message":"behind"}}""".encodeToByteArray()))
        server.enqueue(MockResponse().setBody(body))
        val got = mutableListOf<SessionEvent>()
        try {
            api().sessionEvents("s_1", 3).collect { got += it }
            fail("no error")
        } catch (e: ApiError) {
            assertEquals("resource_exhausted", e.code)
        }
        assertEquals(12L, got.single().seq)
        assertEquals("shell", got.single().toolCall!!.name)
        val (_, payload) = ConnectClient.readEnvelope(server.takeRequest().body)!!
        val req = ConnectClient.json.parseToJsonElement(payload.decodeToString()).jsonObject
        assertEquals("3", req["afterSeq"]!!.jsonPrimitive.content) // a uint64: a string in protojson
        assertEquals("EVENT_VIEW_BASIC", req["view"]!!.jsonPrimitive.content)
        assertEquals(JsonPrimitive(true), req["follow"])
        assertEquals(JsonPrimitive(true), req["partial"])
    }

    @Test fun joiningNamesThisPlatform() = runBlocking {
        server.enqueue(MockResponse().setBody("""{"code":"ABCD-EFGH-JKLM","verificationCode":"1234 5678"}"""))
        val r = api().requestJoin("https://mesh.example", "o_x", "Xiaomi 15 Pro")
        assertEquals("1234 5678", r.verificationCode)
        val o = ConnectClient.json.parseToJsonElement(server.takeRequest().body.readUtf8()).jsonObject
        assertEquals("android", o["rootPlatform"]!!.jsonPrimitive.content)
        assertEquals("Xiaomi 15 Pro", o["rootLabel"]!!.jsonPrimitive.content)
        assertNull("the core makes the root key: none is sent", o["rootPublicKey"])
    }
}
