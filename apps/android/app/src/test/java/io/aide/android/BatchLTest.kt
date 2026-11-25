package io.aide.android

import io.aide.android.api.AgentMode
import io.aide.android.api.Machine
import io.aide.android.api.MachineStatus
import io.aide.android.api.PostRoomMessageRequest
import io.aide.android.api.Project
import io.aide.android.api.Room
import io.aide.android.api.RoomMember
import io.aide.android.api.RoomMessage
import io.aide.android.model.World
import io.aide.android.model.aboveProject
import io.aide.android.model.modeChoice
import io.aide.android.model.modeLabel
import io.aide.android.model.usesPermissionPresets
import io.aide.android.ui.LIGHT
import io.aide.android.ui.roomLine
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import java.util.Locale

/** 批 L: Antigravity first-class permission presets (#111), light-chat mode (#123), and read-only shadow rooms (#124). */
class BatchLTest {
    private val json = Json { ignoreUnknownKeys = true; encodeDefaults = false }

    @Before
    fun chinese() = Locale.setDefault(Locale.SIMPLIFIED_CHINESE)

    @Test
    fun antigravityUsesThreeTierPermissionPresetsLikeDsh() {
        assertTrue(usesPermissionPresets("antigravity"))
        assertTrue(usesPermissionPresets("deepseek"))
        assertFalse(usesPermissionPresets("claude"))

        val modes = listOf(AgentMode("read-only"), AgentMode("workspace-write"), AgentMode("danger-full-access"))
        assertEquals(listOf("仅可查看", "工作区内修改", "完全权限"), modes.map { modeLabel("antigravity", it) })
        assertTrue(aboveProject("antigravity", "danger-full-access", "workspace-write"))
        assertFalse(aboveProject("antigravity", "read-only", "workspace-write"))

        val proj = Project("m1", "p1", name = "核心库", dshPreset = "read-only")
        val dflt = modeChoice("antigravity", "", proj, modes)
        assertEquals("项目默认：仅可查看", dflt.defaultLabel)
        assertNull(dflt.above)
        val elevated = modeChoice("antigravity", "danger-full-access", proj, modes)
        assertNotNull(elevated.above)
        assertTrue(elevated.above!!.contains("核心库"))
        assertTrue(elevated.above!!.contains("完全权限"))
    }

    @Test
    fun lightChatModeLineAndPayloadRoundTrip() {
        val r = Room(roomId = "r1", name = "Hermes", mode = LIGHT, run = "ROOM_RUN_RUNNING")
        assertEquals("轻聊中 · 等回复", roomLine(r))

        val req = PostRoomMessageRequest("m1", "r1", "在吗", "k1", emptyList(), lightChat = true)
        val encoded = json.encodeToString(req)
        assertTrue(encoded.contains("\"lightChat\":true"))
        val decoded = json.decodeFromString<PostRoomMessageRequest>(encoded)
        assertTrue(decoded.lightChat)
    }

    @Test
    fun shadowRoomAppearsInWorldRoomsAndSerializesMetadata() {
        val shadow = Room(
            roomId = "room_shadow_ab",
            name = "Hermes ↔ Muse",
            hostMachineId = "m1",
            members = listOf(RoomMember("c:c1", name = "Hermes"), RoomMember("c:c2", name = "Muse")),
            direct = false,
            shadow = true,
            updatedAt = "2026-11-25T10:00:00Z",
        )
        val direct = Room(
            roomId = "room_direct_1",
            name = "Hermes",
            hostMachineId = "m1",
            members = listOf(RoomMember("c:c1", name = "Hermes")),
            direct = true,
            updatedAt = "2026-11-25T10:05:00Z",
        )
        val world = World(machines = listOf(Machine("m1", "mac", online = true, status = MachineStatus(rooms = listOf(shadow, direct)))))
        assertEquals(listOf("room_shadow_ab"), world.rooms.map { it.roomId })
        assertTrue(world.rooms.first().shadow)
        assertEquals("Agent 间对话 · 只读", roomLine(shadow))

        val msg = RoomMessage(
            messageId = "msg1",
            roomId = "room_shadow_ab",
            from = "c:c1",
            fromName = "Hermes",
            toName = "Muse",
            subject = "部署确认",
            text = "已就绪",
            inReplyTo = "msg0",
            lightChat = false,
        )
        val roundTrip = json.decodeFromString<RoomMessage>(json.encodeToString(msg))
        assertEquals("Muse", roundTrip.toName)
        assertEquals("部署确认", roundTrip.subject)
        assertEquals("msg0", roundTrip.inReplyTo)
    }
}
