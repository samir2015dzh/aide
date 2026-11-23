package io.aide.android

import io.aide.android.api.AgentStatus
import io.aide.android.api.Client
import io.aide.android.api.LocalApi
import io.aide.android.api.Machine
import io.aide.android.api.MachineStatus
import io.aide.android.model.roster
import kotlinx.coroutines.runBlocking
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.assertEquals
import org.junit.Test
import java.time.Instant

/** 工单 #82: deleting a bot from the roster, as the desktop does. */
class RemoveBotTest {
    private val now = Instant.parse("2026-11-23T10:00:00Z")

    /** A personal agent's grant is revoked through the core's RevokeClient, by its grant id. */
    @Test fun aGrantIsRevokedByItsId() = runBlocking {
        val server = MockWebServer()
        server.start()
        try {
            server.enqueue(MockResponse().setBody("{}").setHeader("Content-Type", "application/json"))
            LocalApi(server.port.toLong(), "t").revokeClient("g_muse")
            val r = server.takeRequest()
            assertEquals("/aide.local.v1.LocalService/RevokeClient", r.path)
            assertEquals("""{"grantId":"g_muse"}""", r.body.readUtf8())
        } finally {
            server.shutdown()
        }
    }

    /** A coding agent removed from the roster is hidden on its machine; its own grant stays off the roster with it. */
    @Test fun aHiddenCodingAgentStaysOffWithItsGrant() {
        val m = Machine("m1", "work-pc", online = true, status = MachineStatus(agents = listOf(
            AgentStatus("claude", available = true, loggedIn = true, checkedAt = "2026-11-23T09:59:00Z"),
            AgentStatus("deepseek", available = true, loggedIn = true, checkedAt = "2026-11-23T09:59:00Z", hidden = true),
        )))
        val r = roster(listOf(m), listOf(Client(clientId = "c1", grantId = "g1", label = "DSH on work-pc")), emptyList(), now)
        assertEquals(listOf("Claude Code"), r.all.map { it.name })
        assertEquals(listOf("DSH on work-pc"), r.aside.map { it.client.label })
    }
}
