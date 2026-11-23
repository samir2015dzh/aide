package io.aide.android

import io.aide.android.api.ConnectClient
import io.aide.android.api.LocalApi
import io.aide.android.api.Session
import io.aide.android.api.SessionStatus
import io.aide.android.model.AttachKind
import io.aide.android.model.MAX_ATTACHMENTS
import io.aide.android.model.PendingFile
import io.aide.android.model.attachKind
import io.aide.android.model.attachmentNote
import io.aide.android.model.sessionsByProject
import io.aide.android.model.sizeText
import io.aide.android.ui.Attachments
import io.aide.android.ui.Draft
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import java.util.Base64
import java.util.Locale

/** 批 I, the session page (工单 #90–#92): its sessions by project, and what goes with a message. */
class BatchITest {
    @Before
    fun chinese() = Locale.setDefault(Locale.SIMPLIFIED_CHINESE)

    @Test
    fun imagesAudioAndAnyOtherFile() {
        assertEquals(AttachKind.IMAGE, attachKind(PendingFile("shot.png", "image/png", 1)))
        assertEquals(AttachKind.AUDIO, attachKind(PendingFile("memo.m4a", "", 1)))
        assertEquals(AttachKind.FILE, attachKind(PendingFile("build.tar.gz", "application/gzip", 1)))
        assertTrue(attachmentNote(PendingFile("big.jpg", "image/jpeg", 4L shl 20)).contains("3.5 MB"))
        assertEquals("", attachmentNote(PendingFile("a.txt", "text/plain", 1)))
        assertEquals(listOf("820 B", "12 KB", "3.4 MB"), listOf(sizeText(820), sizeText(12 * 1024), sizeText((3.4 * (1 shl 20)).toLong())))
    }

    @Test
    fun whatDoesNotFitIsSaidByKindAndName() {
        val a = Attachments()
        a.add(List(MAX_ATTACHMENTS - 1) { Draft("$it.txt", "text/plain", byteArrayOf(1)) } + Draft("one.png", "image/png", byteArrayOf(1)) + Draft("two.mp3", "audio/mpeg", byteArrayOf(1)))
        assertEquals(MAX_ATTACHMENTS, a.files.size)
        assertEquals(listOf("音频「two.mp3」没加上：一条消息最多带 20 个附件"), a.refused)
        a.remove(a.files.last())
        assertEquals(emptyList<String>(), a.refused)
    }

    // The files go with the words in one SendMessage, as aide.session.v1.Attachment in protojson: bytes as base64.
    @Test
    fun filesGoWithTheMessage() = runBlocking {
        val server = MockWebServer()
        server.start()
        try {
            server.enqueue(MockResponse().setBody("""{"session":{"sessionId":"s_1"}}"""))
            val a = Attachments()
            a.add(listOf(Draft("notes.txt", "text/plain", "the secret is 4217\n".toByteArray()), Draft("photo-1.jpg", "image/jpeg", byteArrayOf(-1, -40, -1))))
            LocalApi(server.port.toLong(), "tok").sendMessage("s_1", "看看这些", a.wire())
            val r = server.takeRequest()
            assertEquals("/aide.local.v1.LocalService/SendMessage", r.path)
            val o = ConnectClient.json.parseToJsonElement(r.body.readUtf8()).jsonObject
            val files = o["attachments"]!!.jsonArray.map { it.jsonObject }
            assertEquals(listOf("notes.txt", "photo-1.jpg"), files.map { it["name"]!!.jsonPrimitive.content })
            assertEquals("image/jpeg", files[1]["mediaType"]!!.jsonPrimitive.content)
            assertEquals("the secret is 4217\n", String(Base64.getDecoder().decode(files[0]["data"]!!.jsonPrimitive.content)))
        } finally {
            server.shutdown()
        }
    }

    @Test
    fun theSessionsToSwitchToByProject() {
        fun s(id: String, project: String, at: String, agent: String = "claude", machine: String = "m1", status: String = SessionStatus.IDLE) =
            Session(sessionId = id, machineId = machine, agent = agent, projectId = project, status = status, updatedAt = "2026-11-23T10:$at:00Z")
        val open = s("cur", "api", "02", status = SessionStatus.ARCHIVED)
        val groups = sessionsByProject(open, listOf(open, s("w1", "web", "03"), s("w2", "web", "08"), s("a1", "api", "01"), s("gone", "api", "09", status = SessionStatus.ARCHIVED), s("x", "web", "09", agent = "codex"), s("y", "web", "09", machine = "m2")))
        assertEquals(listOf("web" to listOf("w2", "w1"), "api" to listOf("cur", "a1")), groups.map { g -> g.projectId to g.sessions.map { it.sessionId } })
    }
}
