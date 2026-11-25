package io.aide.android.shots

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.hasScrollAction
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithContentDescription
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onFirst
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.performScrollToNode
import androidx.test.core.app.ApplicationProvider
import com.github.takahirom.roborazzi.captureRoboImage
import io.aide.android.BuildConfig
import io.aide.android.api.Device
import io.aide.android.api.LocalApi
import io.aide.android.model.AppModel
import io.aide.android.model.World
import io.aide.android.ui.AideTheme
import io.aide.android.ui.BotPage
import io.aide.android.ui.GrokEngine
import io.aide.android.ui.LocalSessions
import io.aide.android.ui.SessionPage
import io.aide.android.ui.SettingsTab
import kotlinx.coroutines.flow.MutableStateFlow
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
import org.robolectric.annotation.GraphicsMode
import java.util.Locale

/**
 * 批 J · D (工单 #101 #103), as the owner sees them: a sender named by its connection's name now, the app's version in
 * Settings, among devices of every kind (#100's faces wait for the design draft #106). Pictures in
 * app/build/outputs/roborazzi (-Pshots).
 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], application = android.app.Application::class, qualifiers = "zh-rCN-w392dp-h850dp-port-night-xxhdpi")
class BatchJDShots {
    @get:Rule val rule = createComposeRule()
    private val server = MockWebServer()

    // Every kind a device or machine reports: a phone, a tablet, the three computers, an iPhone, one saying nothing known.
    private val devices = listOf(
        Device("k1", "我的手机", "android", thisDevice = true),
        Device("k4", "平板", "android/tablet"),
        Device("k2", "workstation", "windows", machineId = "m1", machineOnline = true),
        Device("k5", "MacBook", "macos"),
        Device("k3", "laptop", "linux", machineId = "m2", machineOnline = true),
        Device("k6", "iPhone", "ios"),
        Device("k7", "旧设备", ""),
    )

    @Before fun setUp() {
        Locale.setDefault(Locale.SIMPLIFIED_CHINESE)
        val room = "{\"roomId\":\"room_1\",\"name\":\"Muse\",\"hostMachineId\":\"m1\",\"direct\":true,\"members\":[{\"botKey\":\"c:c1\",\"address\":\"c1\",\"name\":\"Muse\"}]}"
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                val path = request.path.orEmpty()
                val json = { body: String -> MockResponse().setBody(body).setHeader("Content-Type", "application/json") }
                if (path.endsWith("OpenDirectRoom")) return json("{\"room\":$room}")
                if (path.endsWith("ListRoomMessages")) return json("{\"room\":$room,\"messages\":[" +
                    "{\"messageId\":\"msg_1\",\"from\":\"owner\",\"text\":\"批 J 的 PR 评审过了吗？\",\"deliveries\":[{\"botKey\":\"c:c1\",\"state\":\"ROOM_DELIVERY_STATE_REPLIED\"}]}," +
                    "{\"messageId\":\"msg_2\",\"from\":\"c:c1\",\"fromName\":\"Muse\",\"text\":\"评审过了：等 CI 全绿后合并。\",\"inReplyTo\":\"msg_1\"}," +
                    "{\"messageId\":\"msg_3\",\"from\":\"owner\",\"text\":\"好，合了告诉我\",\"deliveries\":[{\"botKey\":\"c:c1\",\"state\":\"ROOM_DELIVERY_STATE_SENT\"}]}]}")
                // A session one of Muse's messages came into while its connection was still "Muse (127.0.0.1)" (before 工单 #85).
                if (path.endsWith("StreamSessionEvents")) {
                    val b = okio.Buffer()
                    fun env(flags: Int, j: String) { val bytes = j.encodeToByteArray(); b.writeByte(flags); b.writeInt(bytes.size); b.write(bytes) }
                    env(0, "{\"seq\":\"1\",\"userMessage\":{\"text\":\"机器页上下滑动掉帧，查一下原因\",\"from\":\"owner\"}}")
                    env(0, "{\"seq\":\"2\",\"agentMessage\":{\"text\":\"先量一下滑动时每帧的重组次数。\"}}")
                    env(0, "{\"seq\":\"3\",\"userMessage\":{\"text\":\"顺便看看头像解码\",\"from\":\"c1\",\"fromName\":\"Muse (127.0.0.1)\"}}")
                    env(2, "{}")
                    return MockResponse().setBody(b).setHeader("Content-Type", "application/connect+json")
                }
                if (path.endsWith("ListDevices")) return json("{\"devices\":[" + devices.joinToString(",") {
                    "{\"keyId\":\"${it.keyId}\",\"label\":\"${it.label}\",\"platform\":\"${it.platform}\",\"thisDevice\":${it.thisDevice},\"machineId\":\"${it.machineId}\",\"machineOnline\":${it.machineOnline}}"
                } + "]}")
                return json("{}")
            }
        }
        server.start()
    }

    @After fun tearDown() = server.shutdown()

    private fun world(): World = PageShots.world().copy(devices = devices)

    private fun model(): AppModel {
        val m = AppModel(ApplicationProvider.getApplicationContext())
        AppModel::class.java.getDeclaredField("api").apply { isAccessible = true }.set(m, LocalApi(server.port.toLong(), "t"))
        @Suppress("UNCHECKED_CAST")
        (AppModel::class.java.getDeclaredField("_world").apply { isAccessible = true }.get(m) as MutableStateFlow<World>).value = world()
        @Suppress("UNCHECKED_CAST")
        (AppModel::class.java.getDeclaredField("_foreground").apply { isAccessible = true }.get(m) as MutableStateFlow<Boolean>).value = true
        return m
    }

    private fun show(waitFor: String? = null, content: @Composable () -> Unit) {
        GrokEngine.source = GrokFixture
        rule.setContent {
            CompositionLocalProvider(LocalSessions provides world().sessions.values) {
                AideTheme { Surface(Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) { content() } }
            }
        }
        rule.waitForIdle()
        waitFor?.let { text -> rule.waitUntil(10_000) { rule.onAllNodesWithText(text, substring = true).fetchSemanticsNodes().isNotEmpty() } }
    }

    private fun snap(name: String) = rule.onRoot().captureRoboImage("build/outputs/roborazzi/$name.png")

    // 工单 #100 / #103 / #106: Fluent DeviceFace for account & devices + app version in Settings.
    @Test fun versionAndFaces() = model().let { m ->
        show(waitFor = "我的手机") { SettingsTab(m) }
        assertTrue(rule.onAllNodesWithContentDescription("face:person").fetchSemanticsNodes().isNotEmpty())
        assertTrue(rule.onAllNodesWithContentDescription("face:android").fetchSemanticsNodes().isNotEmpty())
        assertTrue(rule.onAllNodesWithContentDescription("face:tablet").fetchSemanticsNodes().isNotEmpty())
        assertTrue(rule.onAllNodesWithContentDescription("face:windows").fetchSemanticsNodes().isNotEmpty())
        rule.onAllNodes(hasScrollAction()).onFirst().performScrollToNode(hasText(BuildConfig.VERSION_NAME, substring = true))
        assertTrue(rule.onAllNodesWithText(BuildConfig.VERSION_NAME, substring = true).fetchSemanticsNodes().isNotEmpty())
        snap("batchj-d-settings-version")
    }

    // 工单 #101 / #106 (after #85): Fluent blue bubble with white text + sender named by connection's name now.
    @Test fun senderByItsNameNow() = model().let { m ->
        show(waitFor = "顺便看看头像解码") { SessionPage(m, "s2", back = {}) }
        assertTrue(rule.onAllNodesWithText("Muse").fetchSemanticsNodes().isNotEmpty())
        rule.onAllNodesWithText("127.0.0.1", substring = true).assertCountEquals(0)
        snap("batchj-d-session-sender")
    }

    @Test @Config(qualifiers = "zh-rCN-w392dp-h850dp-port-notnight-xxhdpi") fun senderLight() = model().let { m ->
        show(waitFor = "顺便看看头像解码") { SessionPage(m, "s2", back = {}) }
        snap("batchj-d-session-sender-light")
    }
}
