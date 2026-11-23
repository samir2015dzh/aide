package io.aide.android.shots

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onRoot
import androidx.test.core.app.ApplicationProvider
import com.github.takahirom.roborazzi.captureRoboImage
import io.aide.android.api.AgentStatus
import io.aide.android.api.ClaudeDesktop
import io.aide.android.api.DSHBridge
import io.aide.android.api.Device
import io.aide.android.api.GetStatusResponse
import io.aide.android.api.LocalApi
import io.aide.android.api.Machine
import io.aide.android.api.MachineStatus
import io.aide.android.api.Project
import io.aide.android.api.Question
import io.aide.android.api.Session
import io.aide.android.api.SessionProgress
import io.aide.android.api.SessionStatus
import io.aide.android.model.AppModel
import io.aide.android.model.World
import io.aide.android.ui.AideTheme
import io.aide.android.ui.BotsTab
import io.aide.android.ui.BotPage
import io.aide.android.ui.DevicesTab
import io.aide.android.api.Client
import io.aide.android.ui.NeedsYouTab
import io.aide.android.ui.NewRoomPage
import io.aide.android.ui.RoomPage
import io.aide.android.ui.SessionsTab
import io.aide.android.ui.SettingsTab
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
import java.time.Instant
import java.util.Locale

/**
 * Each main page as the owner sees it (工单 #54): a high-density phone in dark mode, in Chinese, with a world like
 * the owner's (two computers, Claude Code and DSH on one, sessions in every state). Run with
 * `./gradlew :app:recordRoborazziDebug -Pshots`; the pictures land in app/build/outputs/roborazzi.
 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], application = android.app.Application::class, qualifiers = "zh-rCN-w392dp-h850dp-port-night-xxhdpi")
class PageShots {
    @get:Rule val rule = createComposeRule()
    private val server = MockWebServer()

    @Before fun setUp() {
        Locale.setDefault(Locale.SIMPLIFIED_CHINESE)
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                // The devices are read again when Settings opens: the same ones the world has.
                val path = request.path.orEmpty()
                // The owner's conversation with Muse (G2-a): its direct room on workstation, two messages.
                val room = "{\"roomId\":\"room_1\",\"name\":\"Muse\",\"hostMachineId\":\"m1\",\"direct\":true,\"members\":[{\"botKey\":\"c:c1\",\"address\":\"c1\",\"name\":\"Muse\"}]}"
                if (path.endsWith("OpenDirectRoom")) return MockResponse().setBody("{\"room\":$room}").setHeader("Content-Type", "application/json")
                // A round table (G2-b/c): DSH on workstation and Muse, DSH handing on to Muse, Muse's agreed result.
                val table = "{\"roomId\":\"room_2\",\"name\":\"发版评审\",\"hostMachineId\":\"m1\",\"mode\":\"ROOM_MODE_ROUNDTABLE\",\"maxRounds\":6,\"round\":1,\"unread\":2,\"lastLine\":\"Muse：[FINAL] 可以发版\"," +
                    "\"members\":[{\"botKey\":\"m:m1/deepseek\",\"name\":\"DSH · workstation\",\"agent\":\"deepseek\",\"projectId\":\"aide\",\"sessionId\":\"s2\"},{\"botKey\":\"c:c1\",\"name\":\"Muse\"}]}"
                if (path.endsWith("ListRoomMessages") && request.body.clone().readUtf8().contains("room_2")) return MockResponse().setBody("{\"room\":$table,\"messages\":[" +
                    "{\"messageId\":\"msg_1\",\"from\":\"owner\",\"text\":\"跑一遍发版前检查\",\"mentions\":[\"m:m1/deepseek\"],\"deliveries\":[{\"botKey\":\"m:m1/deepseek\",\"state\":\"ROOM_DELIVERY_STATE_REPLIED\"}]}," +
                    "{\"messageId\":\"msg_2\",\"from\":\"m:m1/deepseek\",\"fromName\":\"DSH · workstation\",\"text\":\"测试全过，构建产物已签名。@Muse 请确认发版说明\",\"round\":0,\"deliveries\":[{\"botKey\":\"c:c1\",\"state\":\"ROOM_DELIVERY_STATE_REPLIED\"}]}," +
                    "{\"messageId\":\"msg_3\",\"from\":\"c:c1\",\"fromName\":\"Muse\",\"text\":\"[FINAL] 可以发版：测试全过，说明已确认。\",\"final\":true,\"round\":1}]}").setHeader("Content-Type", "application/json")
                if (path.endsWith("ListRoomMessages")) return MockResponse().setBody("{\"room\":$room,\"messages\":[" +
                    "{\"messageId\":\"msg_1\",\"from\":\"owner\",\"text\":\"批 G 的 PR 评审过了吗？\",\"deliveries\":[{\"botKey\":\"c:c1\",\"state\":\"ROOM_DELIVERY_STATE_REPLIED\"}]}," +
                    "{\"messageId\":\"msg_2\",\"from\":\"c:c1\",\"fromName\":\"Muse\",\"text\":\"评审过了：内容通过，等 CI 全绿后成对合并。\",\"inReplyTo\":\"msg_1\"}," +
                    "{\"messageId\":\"msg_3\",\"from\":\"owner\",\"text\":\"好，合了告诉我\",\"deliveries\":[{\"botKey\":\"c:c1\",\"state\":\"ROOM_DELIVERY_STATE_SENT\"}]}]}").setHeader("Content-Type", "application/json")
                val body = if (path.endsWith("ListDevices")) {
                    "{\"devices\":[{\"keyId\":\"k1\",\"label\":\"我的手机\",\"platform\":\"android\",\"thisDevice\":true},{\"keyId\":\"k2\",\"label\":\"workstation\",\"platform\":\"windows\",\"machineId\":\"m1\",\"machineOnline\":true},{\"keyId\":\"k3\",\"label\":\"laptop\",\"platform\":\"linux\",\"machineId\":\"m2\",\"machineOnline\":true}]}"
                } else "{}"
                return MockResponse().setBody(body).setHeader("Content-Type", "application/json")
            }
        }
        server.start()
    }

    @After fun tearDown() = server.shutdown()

    private fun model(): AppModel {
        val m = AppModel(ApplicationProvider.getApplicationContext())
        AppModel::class.java.getDeclaredField("api").apply { isAccessible = true }.set(m, LocalApi(server.port.toLong(), "t"))
        @Suppress("UNCHECKED_CAST")
        (AppModel::class.java.getDeclaredField("_world").apply { isAccessible = true }.get(m) as MutableStateFlow<World>).value = world()
        return m
    }

    private fun shot(name: String, waitFor: String? = null, content: @Composable () -> Unit) {
        // A Surface, as the app's Scaffold: the page's colour and the text colour on it.
        rule.setContent { AideTheme { Surface(Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) { content() } } }
        rule.waitForIdle()
        // What a page reads from the machine after it opens (the conversation): there before the picture.
        waitFor?.let { text -> rule.waitUntil(10_000) { rule.onAllNodesWithText(text, substring = true).fetchSemanticsNodes().isNotEmpty() } }
        rule.onRoot().captureRoboImage("build/outputs/roborazzi/$name.png")
    }

    @Test fun tasks() = model().let { m -> shot("1-tasks") { SessionsTab(m) {} } }
    @Test fun needsYou() = model().let { m -> shot("2-needs-you") { NeedsYouTab(m) {} } }
    @Test fun bots() = model().let { m -> shot("3-bots", waitFor = "发版评审") { BotsTab(m) {} } }
    @Test fun botPage() = model().let { m -> shot("6-bot-dsh") { BotPage(m, "m:m1/deepseek", openSession = {}, edit = {}) {} } }
    @Test fun personalBotPage() = model().let { m -> shot("7-bot-muse", waitFor = "评审过了") { BotPage(m, "c:c1", openSession = {}, edit = {}) {} } }
    @Test fun settings() = model().let { m -> shot("4-settings") { SettingsTab(m) } }
    @Test fun devices() = model().let { m -> shot("5-devices") { DevicesTab(m) {} } }
    @Test fun room() = model().let { m -> shot("8-room", waitFor = "可以发版") { RoomPage(m, "m1", "room_2", openSession = {}) {} } }
    @Test fun newRoom() = model().let { m -> shot("9-new-room") { NewRoomPage(m, opened = {}) {} } }
    // 工单 #82: a long press on Muse in the roster, asking before it is deleted.
    @Test fun removeBot() = model().let { m ->
        val w = world()
        val muse = io.aide.android.model.roster(w.machines, w.clients, w.sessions.values, now).all.first { it.key == "c:c1" }
        shot("10-remove-bot") { BotsTab(m) {}; io.aide.android.ui.RemoveBotDialog(m, muse, "") {} }
    }

    companion object {
        private val now: Instant = Instant.now()
        private fun ago(min: Long) = now.minusSeconds(min * 60).toString()

        fun world(): World {
            val alex = Machine(
                machineId = "m1", machineName = "workstation", online = true, version = "0.2.5",
                status = MachineStatus(
                    platform = "windows/amd64", runningSessions = 2, maxSessions = 4, version = "0.2.5",
                    // The round table it hosts (G2-b/c), as its status brings it on the Watch.
                    rooms = listOf(io.aide.android.api.Room(roomId = "room_2", name = "发版评审", hostMachineId = "m1", mode = "ROOM_MODE_ROUNDTABLE", maxRounds = 6, round = 1, unread = 2, lastLine = "Muse：[FINAL] 可以发版",
                        members = listOf(io.aide.android.api.RoomMember(botKey = "m:m1/deepseek", name = "DSH · workstation"), io.aide.android.api.RoomMember(botKey = "c:c1", name = "Muse")))),
                    agents = listOf(
                        AgentStatus("claude", available = true, loggedIn = true, version = "2.1.330", checkedAt = ago(40), desktop = ClaudeDesktop(installed = true, signedIn = true)),
                        AgentStatus("deepseek", available = true, loggedIn = true, version = "1.4.0", checkedAt = ago(40), models = listOf(io.aide.android.api.AgentModel("deepseek-v4", "DeepSeek V4"), io.aide.android.api.AgentModel("deepseek-v4-flash", "DeepSeek V4 Flash")), dshBridge = DSHBridge("DSH_BRIDGE_STATE_RUNNING", version = "0.3.1", latestVersion = "0.3.1")),
                    ),
                ),
            )
            val big = Machine(
                machineId = "m2", machineName = "laptop", online = true, version = "0.2.5",
                status = MachineStatus(
                    platform = "linux/amd64", runningSessions = 1, maxSessions = 4, version = "0.2.5",
                    agents = listOf(
                        AgentStatus("claude", available = true, loggedIn = false, version = "2.1.330", checkedAt = ago(2)),
                        AgentStatus("codex", available = true, loggedIn = true, version = "0.61.0", checkedAt = ago(2)),
                    ),
                ),
            )
            fun s(id: String, m: String, agent: String, status: String, title: String, min: Long, p: SessionProgress? = null) =
                Session(sessionId = id, machineId = m, projectId = "aide", agent = agent, status = status, createdBy = "owner", title = title, createdAt = ago(min + 30), updatedAt = ago(min), progress = p)
            val sessions = listOf(
                s("s1", "m1", "claude", SessionStatus.WAITING_INPUT, "批 F：机器页名单合并", 3, SessionProgress(pendingQuestion = Question())),
                s("s2", "m1", "deepseek", SessionStatus.RUNNING, "修安卓滑动掉帧", 1, SessionProgress(currentTool = "Bash", lastEventAt = ago(0))),
                s("s3", "m2", "codex", SessionStatus.RUNNING, "整理发版说明", 2, SessionProgress(lastEventAt = ago(0))),
                s("s4", "m1", "claude", SessionStatus.IDLE, "交接仓瘦身", 50),
                s("s5", "m2", "claude", SessionStatus.IDLE, "CI 缓存加速复核", 180),
                s("s6", "m1", "deepseek", SessionStatus.CRASHED, "Go 内存审计第二步", 300),
                s("s7", "m2", "codex", SessionStatus.IDLE, "README 链接核对", 1500),
                s("s8", "m1", "claude", SessionStatus.IDLE, "批 G 名册复核", 20).copy(createdBy = "c1"),
            ).associateBy { it.sessionId }
            return World(
                status = GetStatusResponse(initialized = true, relayUrl = "https://relay.example", relayConnected = true, version = "0.2.5", account = "owner"),
                machines = listOf(alex, big),
                devices = listOf(Device("k1", "我的手机", "android", thisDevice = true), Device("k2", "workstation", "windows", machineId = "m1", machineOnline = true), Device("k3", "laptop", "linux", machineId = "m2", machineOnline = true)),
                projects = listOf(Project("m1", "aide", name = "Aide"), Project("m2", "aide", name = "Aide")),
                clients = listOf(Client(clientId = "c1", grantId = "c1", label = "Muse", online = true), Client(clientId = "c2", grantId = "c2", label = "Grok Bot", lastSeen = ago(90))),
                sessions = sessions,
                watched = true,
            )
        }
    }
}
