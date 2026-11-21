package io.aide.android

import io.aide.android.api.ApiError
import io.aide.android.api.ConnectClient
import io.aide.android.api.LocalApi
import io.aide.android.api.SessionEvent
import io.aide.android.api.SetLoginRequest
import io.aide.android.api.WatchResponse
import kotlinx.coroutines.asCoroutineDispatcher
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

    // What the core sends, from Go's protojson (cmd/aide-mobile/android's core, 2026-11-16).
    private val watchJson = """{"reset":true,"status":{"initialized":true,"ownerId":"o_x","relayConnected":true},"machines":[{"machineId":"m_1","machineName":"mac","online":true,"status":{"platform":"darwin/arm64","agents":[{"name":"claude","available":true,"loggedIn":true}],"runningSessions":2},"her":{"enabled":true,"running":true,"unread":3}}],"devices":[{"keyId":"k_1","label":"Xiaomi 15 Pro","platform":"android","thisDevice":true},{"keyId":"k_2","label":"MacBook","platform":"darwin","machineId":"m_1","machineOnline":true,"legacy":true}],"sessions":[{"sessionId":"s_1","machineId":"m_1","agent":"claude","status":"SESSION_STATUS_WAITING_INPUT","createdBy":"c_1","progress":{"pendingQuestion":{"questionId":"q1","items":[{"id":"i1","text":"Which?","options":[{"label":"A"}]}]}},"updatedAt":"2026-11-16T17:24:05.572Z"}],"workVersion":"7"}"""

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
        assertEquals("/aide.local.v1.LocalService/GetStatus", r.path)
        assertEquals("Bearer tok", r.getHeader("Authorization"))
        assertEquals("1", r.getHeader("Connect-Protocol-Version"))
        assertTrue(r.getHeader("Content-Type")!!.startsWith("application/json"))
        assertEquals("{}", r.body.readUtf8())
    }

    @Test fun unaryErrorIsTheCoresCode() = runBlocking {
        server.enqueue(MockResponse().setResponseCode(403).setBody("""{"code":"permission_denied","message":"wrong account or password"}"""))
        try {
            api().logIn("https://aide.example", "owner", "not it", "Xiaomi 15 Pro")
            fail("no error")
        } catch (e: ApiError) {
            assertEquals("permission_denied", e.code)
            assertEquals("wrong account or password", e.message)
        }
        assertEquals("/aide.local.v1.LocalService/LogIn", server.takeRequest().path)
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
        assertTrue(w.devices[0].thisDevice)
        assertTrue(w.devices[1].legacy && w.devices[1].machineOnline)
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

    // protojson names: LogInRequest and SetupRequest (aide.local.v1, docs/accounts.md).
    @Test fun loggingInNamesThisPlatform() = runBlocking {
        server.enqueue(MockResponse().setBody("""{"ownerId":"o_x"}"""))
        assertEquals("o_x", api().logIn("https://aide.example", "owner", "correct horse", "Xiaomi 15 Pro").ownerId)
        val o = ConnectClient.json.parseToJsonElement(server.takeRequest().body.readUtf8()).jsonObject
        assertEquals(setOf("relayUrl", "account", "password", "label", "platform"), o.keys)
        assertEquals("android", o["platform"]!!.jsonPrimitive.content)
        assertEquals("Xiaomi 15 Pro", o["label"]!!.jsonPrimitive.content)
        assertEquals("correct horse", o["password"]!!.jsonPrimitive.content)
    }

    @Test fun settingUpSendsTheSetupCode() = runBlocking {
        server.enqueue(MockResponse().setBody("""{"ownerId":"o_x"}"""))
        api().setup("http://203.0.113.7:8080", "ABCD-EFGH", "owner", "correct horse", "Xiaomi 15 Pro")
        val r = server.takeRequest()
        assertEquals("/aide.local.v1.LocalService/Setup", r.path)
        val o = ConnectClient.json.parseToJsonElement(r.body.readUtf8()).jsonObject
        assertEquals(setOf("relayUrl", "setupCode", "account", "password", "label", "platform"), o.keys)
        assertEquals("ABCD-EFGH", o["setupCode"]!!.jsonPrimitive.content)
        assertEquals("android", o["platform"]!!.jsonPrimitive.content)
    }

    @Test fun theAccountAndTheDevices() = runBlocking {
        server.enqueue(MockResponse().setBody("""{"account":"owner","server":"aide.example","off":true}"""))
        val l = api().getLogin()
        assertEquals("owner", l.account)
        assertEquals("aide.example", l.server)
        assertTrue(l.off)
        server.takeRequest()

        // A new password, logging in kept off: what is unset is left out.
        server.enqueue(MockResponse().setBody("""{"signedOut":3}"""))
        assertEquals(3, api().setLogin(SetLoginRequest("old one", newPassword = "new password", off = true)).signedOut)
        val set = ConnectClient.json.parseToJsonElement(server.takeRequest().body.readUtf8()).jsonObject
        assertEquals(mapOf("currentPassword" to "old one", "newPassword" to "new password"), set.filterKeys { it != "off" }.mapValues { it.value.jsonPrimitive.content })
        assertEquals(JsonPrimitive(true), set["off"])
        // Turning logging in on: off false, left out as protojson does.
        server.enqueue(MockResponse().setBody("""{}"""))
        assertEquals(0, api().setLogin(SetLoginRequest("pw")).signedOut)
        assertEquals("""{"currentPassword":"pw"}""", server.takeRequest().body.readUtf8())

        server.enqueue(MockResponse().setBody("""{"devices":[{"keyId":"k_2","label":"MacBook","machineId":"m_1"}]}"""))
        assertEquals(listOf("k_2"), api().listDevices().devices.map { it.keyId })
        server.takeRequest()
        server.enqueue(MockResponse().setBody("""{"reissuedMachines":1}"""))
        assertEquals(1, api().removeDevice("k_2", eraseHer = true).reissuedMachines)
        val rm = server.takeRequest()
        assertEquals("/aide.local.v1.LocalService/RemoveDevice", rm.path)
        assertEquals("""{"keyId":"k_2","eraseHer":true}""", rm.body.readUtf8())
    }

    // An answer is read off the caller's thread: the UI calls from the main thread, where reading a large
    // body (a DSH session's page of events) is a socket read Android ends the app for (handover: the
    // Android app quit on opening any DSH session).
    @Test fun aLargeAnswerIsReadOffTheCallersThread() {
        val readOn = java.util.concurrent.atomic.AtomicReference<String>()
        val http = ConnectClient.defaultHttp().newBuilder().eventListener(object : okhttp3.EventListener() {
            override fun responseBodyEnd(call: okhttp3.Call, byteCount: Long) = readOn.set(Thread.currentThread().name)
        }).build()
        val client = ConnectClient("http://127.0.0.1:${server.port}", "tok", "aide.local.v1.LocalService", http)
        val big = "x".repeat(2_000_000)
        server.enqueue(MockResponse().setBody("""{"events":[{"seq":"1","agentMessage":{"text":"$big"}}]}""").throttleBody(256 * 1024, 5, java.util.concurrent.TimeUnit.MILLISECONDS))
        val caller = java.util.concurrent.Executors.newSingleThreadExecutor { Thread(it, "caller-main") }
        try {
            val out = runBlocking(caller.asCoroutineDispatcher()) { client.unary("ReadDSHSession", kotlinx.serialization.json.JsonObject(emptyMap())) }
            assertEquals(1, out.jsonObject["events"]!!.let { it as kotlinx.serialization.json.JsonArray }.size)
            assertTrue("body read on ${readOn.get()}", readOn.get() != null && !readOn.get().startsWith("caller-main"))
        } finally {
            caller.shutdown()
        }
    }

    // What a page catches is ApiError: a body cut short, or an answer that is not JSON or not of the
    // answer's shape, is one too, never an exception that ends the app.
    @Test fun anAnswerCutShortOrUnreadableIsAnApiError() = runBlocking {
        server.enqueue(MockResponse().setBody("{\"events\":[" + "{}, ".repeat(100_000)).setSocketPolicy(okhttp3.mockwebserver.SocketPolicy.DISCONNECT_DURING_RESPONSE_BODY))
        try {
            api().readDSHSession("m_1", "s_1")
            fail("no error")
        } catch (e: ApiError) {
            assertTrue(e.code, e.code == "unavailable" || e.code == "internal")
        }
        server.enqueue(MockResponse().setBody("""{"events":[{"seq":"1","""))
        try {
            api().readDSHSession("m_1", "s_1")
            fail("no error")
        } catch (e: ApiError) {
            assertEquals("internal", e.code)
        }
        server.enqueue(MockResponse().setBody("""{"events":{"not":"a list"}}"""))
        try {
            api().readDSHSession("m_1", "s_1")
            fail("no error")
        } catch (e: ApiError) {
            assertEquals("internal", e.code)
        }
    }
}
