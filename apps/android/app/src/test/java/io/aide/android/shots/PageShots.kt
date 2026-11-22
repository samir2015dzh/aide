package io.aide.android.shots

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.junit4.createComposeRule
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
import io.aide.android.api.Client
import io.aide.android.ui.NeedsYouTab
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
                val body = if (request.path.orEmpty().endsWith("ListDevices")) {
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

    private fun shot(name: String, content: @Composable () -> Unit) {
        // A Surface, as the app's Scaffold: the page's colour and the text colour on it.
        rule.setContent { AideTheme { Surface(Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) { content() } } }
        rule.waitForIdle()
        rule.onRoot().captureRoboImage("build/outputs/roborazzi/$name.png")
    }

    @Test fun tasks() = model().let { m -> shot("1-tasks") { SessionsTab(m, openDSH = {}) {} } }
    @Test fun needsYou() = model().let { m -> shot("2-needs-you") { NeedsYouTab(m) {} } }
    @Test fun bots() = model().let { m -> shot("3-bots") { BotsTab(m) {} } }
    @Test fun botPage() = model().let { m -> shot("6-bot-dsh") { BotPage(m, "m:m1/deepseek", openSession = {}, edit = {}) {} } }
    @Test fun personalBotPage() = model().let { m -> shot("7-bot-muse") { BotPage(m, "c:c1", openSession = {}, edit = {}) {} } }
    @Test fun settings() = model().let { m -> shot("4-settings") { SettingsTab(m) } }

    companion object {
        private val now: Instant = Instant.now()
        private fun ago(min: Long) = now.minusSeconds(min * 60).toString()

        fun world(): World {
            val alex = Machine(
                machineId = "m1", machineName = "workstation", online = true, version = "0.2.5",
                status = MachineStatus(
                    platform = "windows/amd64", runningSessions = 2, maxSessions = 4, version = "0.2.5",
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
