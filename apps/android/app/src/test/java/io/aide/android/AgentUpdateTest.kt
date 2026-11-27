package io.aide.android

import androidx.compose.foundation.layout.Column
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.core.app.ApplicationProvider
import io.aide.android.api.AgentStatus
import io.aide.android.api.AgentUpdate
import io.aide.android.api.LocalApi
import io.aide.android.api.Machine
import io.aide.android.api.MachineStatus
import io.aide.android.model.AppModel
import io.aide.android.ui.AgentUpdateLine
import io.aide.android.ui.AideTheme
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.util.Locale

/**
 * 工单 #178 on the phone, as on the desktop's Connect page: an agent with a newer release out says so against its
 * version and offers Update; pressed, its machine runs the CLI's own updater, and the page says what came of it with
 * its reach and shows what it printed. One whose release can't be told says unknown, with no button.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class, qualifiers = "zh-rCN-w392dp-h850dp-port-xxhdpi")
class AgentUpdateTest {
    @get:Rule val rule = createComposeRule()
    private val server = MockWebServer()
    private val bodies = mutableListOf<String>()
    private lateinit var model: AppModel
    private val m = Machine("m1", "workstation", online = true, status = MachineStatus(platform = "darwin/arm64"))

    @Before fun setUp() {
        Locale.setDefault(Locale.SIMPLIFIED_CHINESE)
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                val method = request.path.orEmpty().substringAfterLast('/')
                if (method == "UpdateMachineAgent") synchronized(bodies) { bodies += request.body.readUtf8() }
                val out = if (method == "UpdateMachineAgent") {
                    """{"succeeded":true,"command":"codex update","output":"Updating Codex via npm…\nUpdated to 0.171.0\n","version":"0.171.0"}"""
                } else "{}"
                return MockResponse().setBody(out).setHeader("Content-Type", "application/json")
            }
        }
        server.start()
        model = AppModel(ApplicationProvider.getApplicationContext())
        AppModel::class.java.getDeclaredField("api").apply { isAccessible = true }.set(model, LocalApi(server.port.toLong(), "t"))
    }

    @After fun tearDown() = server.shutdown()

    private fun count(text: String) = rule.onAllNodesWithText(text).fetchSemanticsNodes().size

    @Test fun aNewerReleaseIsUpdatedWithItsOwnUpdater() {
        val a = AgentStatus("codex", available = true, version = "0.170.0", update = AgentUpdate("AGENT_UPDATE_STATE_AVAILABLE", latest = "0.171.0"))
        rule.setContent { AideTheme { Column { AgentUpdateLine(model, m, a, a.update!!) } } }
        rule.onNodeWithText("有新版 0.171.0（这台机器上是 0.170.0）").assertExists()
        rule.onNodeWithText("更新").performClick()
        rule.waitUntil(5_000) { count("已更新到 0.171.0：新开的会话用新版；正在跑的会话保持旧版，重启后生效。") > 0 }
        rule.onNodeWithText("Updating Codex via npm…\nUpdated to 0.171.0").assertExists()
        val sent = synchronized(bodies) { bodies.toList() }
        assertEquals(1, sent.size)
        assertTrue(sent[0], sent[0].contains("\"machineId\":\"m1\"") && sent[0].contains("\"name\":\"codex\""))
    }

    @Test fun anUnknownReleaseSaysWhyAndOffersNothing() {
        val a = AgentStatus("claude", available = true, version = "2.1.330", update = AgentUpdate("AGENT_UPDATE_STATE_UNKNOWN", detail = "npm registry: 503 Service Unavailable"))
        rule.setContent { AideTheme { Column { AgentUpdateLine(model, m, a, a.update!!) } } }
        rule.onNodeWithText("最新版未知：npm registry: 503 Service Unavailable").assertExists()
        assertEquals(0, count("更新"))
    }
}
