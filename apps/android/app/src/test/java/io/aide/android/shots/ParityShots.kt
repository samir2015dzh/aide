package io.aide.android.shots

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onRoot
import androidx.test.core.app.ApplicationProvider
import com.github.takahirom.roborazzi.captureRoboImage
import io.aide.android.api.AgentStatus
import io.aide.android.api.DSHBridge
import io.aide.android.api.LocalApi
import io.aide.android.api.Machine
import io.aide.android.api.MachineStatus
import io.aide.android.model.AppModel
import io.aide.android.model.World
import io.aide.android.ui.AideTheme
import io.aide.android.ui.BotPage
import io.aide.android.ui.BotsTab
import io.aide.android.ui.DevicesTab
import io.aide.android.ui.GrokEngine
import io.aide.android.ui.LocalSessions
import io.aide.android.ui.RoomPage
import java.time.LocalDate
import java.time.ZoneId
import java.util.Locale
import kotlinx.coroutines.flow.MutableStateFlow
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.junit.After
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/**
 * 工单 #145 第 5 条的安卓页面（-Pshots）：Muse 对话里没送到任何人的消息（失败态气泡 + 重发）与时间分隔行（P02/P28），
 * 房间时间线同样；名册与设备页上「未确认」的 DSH 与 ACP agent 同一个黄点、「已登录」同一个蓝点（P25）。
 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], application = android.app.Application::class, qualifiers = "zh-rCN-w392dp-h850dp-port-night-xxhdpi")
class ParityShots {
    @get:Rule val rule = createComposeRule()
    private val server = MockWebServer()

    /** A time today or days back at a clock time, in this computer's zone, as the timeline reads it. */
    private fun at(daysBack: Long, hour: Int, minute: Int) = LocalDate.now().minusDays(daysBack).atTime(hour, minute).atZone(ZoneId.systemDefault()).toInstant().toString()

    @Before fun setUp() {
        Locale.setDefault(Locale.SIMPLIFIED_CHINESE)
        val failed = "{\"botKey\":\"c:c1\",\"state\":\"ROOM_DELIVERY_STATE_FAILED\",\"reason\":\"Muse 现在没连着，没送到\"}"
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                val path = request.path.orEmpty()
                val direct = "{\"roomId\":\"room_1\",\"name\":\"Muse\",\"hostMachineId\":\"m1\",\"direct\":true,\"members\":[{\"botKey\":\"c:c1\",\"address\":\"c1\",\"name\":\"Muse\"}]}"
                val table = "{\"roomId\":\"room_2\",\"name\":\"发版评审\",\"hostMachineId\":\"m1\",\"mode\":\"ROOM_MODE_MANUAL\",\"maxRounds\":6," +
                    "\"members\":[{\"botKey\":\"m:m1/deepseek\",\"name\":\"DSH\",\"agent\":\"deepseek\",\"projectId\":\"aide\"},{\"botKey\":\"m:m2/gemini\",\"name\":\"Gemini CLI\",\"agent\":\"gemini\",\"projectId\":\"aide\"},{\"botKey\":\"c:c1\",\"name\":\"Muse\"}]}"
                val json = when {
                    path.endsWith("OpenDirectRoom") -> "{\"room\":$direct}"
                    path.endsWith("ListRoomMessages") && request.body.clone().readUtf8().contains("room_2") -> "{\"room\":$table,\"messages\":[" +
                        "{\"messageId\":\"r1\",\"from\":\"owner\",\"text\":\"跑一遍发版前检查\",\"at\":\"${at(1, 20, 0)}\",\"mentions\":[\"m:m1/deepseek\"],\"deliveries\":[{\"botKey\":\"m:m1/deepseek\",\"state\":\"ROOM_DELIVERY_STATE_REPLIED\"}]}," +
                        "{\"messageId\":\"r2\",\"from\":\"m:m1/deepseek\",\"fromName\":\"DSH\",\"text\":\"测试全过，构建产物已签名。\",\"at\":\"${at(1, 20, 6)}\"}," +
                        "{\"messageId\":\"r3\",\"from\":\"owner\",\"text\":\"@Muse 写一下发版说明\",\"at\":\"${at(0, 0, 5)}\",\"mentions\":[\"c:c1\"],\"deliveries\":[$failed]}]}"
                    path.endsWith("ListRoomMessages") -> "{\"room\":$direct,\"messages\":[" +
                        "{\"messageId\":\"m1\",\"from\":\"owner\",\"text\":\"批 G 的 PR 评审过了吗？\",\"at\":\"${at(1, 20, 0)}\",\"deliveries\":[{\"botKey\":\"c:c1\",\"state\":\"ROOM_DELIVERY_STATE_REPLIED\"}]}," +
                        "{\"messageId\":\"m2\",\"from\":\"c:c1\",\"fromName\":\"Muse\",\"text\":\"评审过了：内容通过，等 CI 全绿后成对合并。\",\"at\":\"${at(1, 20, 2)}\",\"inReplyTo\":\"m1\"}," +
                        "{\"messageId\":\"m3\",\"from\":\"owner\",\"text\":\"好，合了告诉我\",\"at\":\"${at(0, 0, 5)}\",\"deliveries\":[$failed]}]}"
                    else -> "{}"
                }
                return MockResponse().setBody(json).setHeader("Content-Type", "application/json")
            }
        }
        server.start()
    }

    @After fun tearDown() = server.shutdown()

    /** The owner's world, with the two kinds of "未确认" (DSH's, an ACP agent's) and the two kinds of "已登录" side by side. */
    private fun world(): World {
        val w = PageShots.world()
        val fresh = java.time.Instant.now().minusSeconds(60).toString()
        val m1 = Machine(
            machineId = "m1", machineName = "workstation", online = true, version = "0.2.5",
            status = MachineStatus(
                platform = "windows/amd64", version = "0.2.5", rooms = w.machines[0].status!!.rooms,
                agents = listOf(
                    AgentStatus("claude", available = true, loggedIn = true, version = "2.1.330", checkedAt = fresh),
                    AgentStatus("deepseek", available = true, loginUnconfirmed = true, version = "1.4.0", checkedAt = fresh, dshBridge = DSHBridge("DSH_BRIDGE_STATE_RUNNING", version = "0.3.1", latestVersion = "0.3.1")),
                ),
            ),
        )
        val m2 = Machine(
            machineId = "m2", machineName = "laptop", online = true, version = "0.2.5",
            status = MachineStatus(
                platform = "linux/amd64", version = "0.2.5",
                agents = listOf(
                    AgentStatus("gemini", available = true, loginUnconfirmed = true, version = "0.9.0", checkedAt = fresh),
                    AgentStatus("codex", available = true, version = "0.61.0", checkedAt = fresh),
                ),
            ),
        )
        return w.copy(machines = listOf(m1, m2))
    }

    private fun model(): AppModel {
        val m = AppModel(ApplicationProvider.getApplicationContext())
        AppModel::class.java.getDeclaredField("api").apply { isAccessible = true }.set(m, LocalApi(server.port.toLong(), "t"))
        @Suppress("UNCHECKED_CAST")
        (AppModel::class.java.getDeclaredField("_world").apply { isAccessible = true }.get(m) as MutableStateFlow<World>).value = world()
        @Suppress("UNCHECKED_CAST")
        (AppModel::class.java.getDeclaredField("_foreground").apply { isAccessible = true }.get(m) as MutableStateFlow<Boolean>).value = true
        return m
    }

    private fun shot(name: String, waitFor: String? = null, content: @Composable () -> Unit) {
        GrokEngine.source = GrokFixture
        rule.setContent {
            CompositionLocalProvider(LocalSessions provides world().sessions.values) {
                AideTheme { Surface(Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) { content() } }
            }
        }
        rule.waitForIdle()
        waitFor?.let { text -> rule.waitUntil(10_000) { rule.onAllNodesWithText(text, substring = true).fetchSemanticsNodes().isNotEmpty() } }
        rule.onRoot().captureRoboImage("build/outputs/roborazzi/$name.png")
    }

    @Test fun museFailed() = model().let { m -> shot("145-p02-p28-muse", waitFor = "重发") { BotPage(m, "c:c1", openSession = {}, edit = {}) {} } }
    @Test @Config(qualifiers = "zh-rCN-w392dp-h850dp-port-notnight-xxhdpi") fun museFailedLight() = model().let { m -> shot("145-p02-p28-muse-light", waitFor = "重发") { BotPage(m, "c:c1", openSession = {}, edit = {}) {} } }
    @Test fun roomFailed() = model().let { m -> shot("145-p02-p28-room", waitFor = "重发") { RoomPage(m, "m1", "room_2", openSession = {}) {} } }
    @Test fun rosterDots() = model().let { m -> shot("145-p25-roster") { BotsTab(m) {} } }
    @Test @Config(qualifiers = "zh-rCN-w392dp-h850dp-port-notnight-xxhdpi") fun rosterDotsLight() = model().let { m -> shot("145-p25-roster-light") { BotsTab(m) {} } }
    // P33: Claude Code on workstation, its conversations counted as its list shows them.
    @Test @Config(qualifiers = "zh-rCN-w392dp-h1400dp-port-night-xxhdpi") fun claudeCount() = model().let { m -> shot("145-p33-claude", waitFor = "当前展示") { BotPage(m, "m:m1/claude", openSession = {}, edit = {}) {} } }
    @Test fun devicesDots() = model().let { m -> shot("145-p25-devices") { DevicesTab(m) {} } }
}
