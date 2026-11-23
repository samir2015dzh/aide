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
import io.aide.android.api.AgentLogin
import io.aide.android.api.AgentStatus
import io.aide.android.api.ClaudeDesktop
import io.aide.android.api.GetStatusResponse
import io.aide.android.api.LocalApi
import io.aide.android.api.Machine
import io.aide.android.api.MachineStatus
import io.aide.android.api.Project
import io.aide.android.api.Session
import io.aide.android.api.SessionStatus
import io.aide.android.model.AppModel
import io.aide.android.model.World
import io.aide.android.ui.AideTheme
import io.aide.android.ui.BotPage
import io.aide.android.ui.DevicesTab
import io.aide.android.ui.GrokEngine
import io.aide.android.ui.LocalSessions
import io.aide.android.ui.SessionPage
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
 * 工单 #108 on the phone: Claude Code's sign-in as its command line has it, the Claude desktop app signed in beside it —
 * not signed in, unconfirmed, signing in, signed in — with the entry that signs it in from Aide on its bot's page, on the
 * Devices tab and under a session that failed not signed in. `./gradlew :app:recordRoborazziDebug -Pshots`.
 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], application = android.app.Application::class, qualifiers = "zh-rCN-w392dp-h850dp-port-xxhdpi")
class LoginShots {
    @get:Rule val rule = createComposeRule()
    private val server = MockWebServer()
    private val now = Instant.now()

    @Before fun setUp() {
        Locale.setDefault(Locale.SIMPLIFIED_CHINESE)
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                if (request.path.orEmpty().endsWith("StreamSessionEvents")) {
                    val b = okio.Buffer()
                    fun env(flags: Int, json: String) { val bytes = json.encodeToByteArray(); b.writeByte(flags); b.writeInt(bytes.size); b.write(bytes) }
                    env(0, "{\"seq\":\"1\",\"userMessage\":{\"text\":\"把首页标题改一下\",\"from\":\"owner\"}}")
                    env(0, "{\"seq\":\"2\",\"turnCompleted\":{\"isError\":true,\"result\":\"Not logged in · Please run /login\",\"failure\":{\"kind\":\"FAILURE_KIND_AUTH\",\"message\":\"Not logged in · Please run /login\"}}}")
                    env(2, "{}")
                    return MockResponse().setBody(b).setHeader("Content-Type", "application/connect+json")
                }
                return MockResponse().setBody("{}").setHeader("Content-Type", "application/json")
            }
        }
        server.start()
    }

    @After fun tearDown() = server.shutdown()

    private fun claude(loggedIn: Boolean, unconfirmed: Boolean = false, login: AgentLogin? = null) = AgentStatus(
        "claude", available = true, version = "2.1.330", loggedIn = loggedIn, loginUnconfirmed = unconfirmed, checkedAt = now.minusSeconds(60).toString(),
        desktop = ClaudeDesktop(installed = true, signedIn = true), login = login,
    )

    private fun world(a: AgentStatus) = World(
        status = GetStatusResponse(initialized = true, relayConnected = true, account = "owner"),
        machines = listOf(Machine("m1", "workstation", online = true, status = MachineStatus(platform = "windows/amd64", agents = listOf(a)))),
        projects = listOf(Project("m1", "site", name = "site")),
        sessions = mapOf("s1" to Session(sessionId = "s1", machineId = "m1", projectId = "site", agent = "claude", status = SessionStatus.IDLE, createdBy = "owner", title = "改首页标题", createdAt = now.toString(), updatedAt = now.toString())),
        watched = true,
    )

    private fun shot(name: String, a: AgentStatus, waitFor: String? = null, content: @Composable (AppModel) -> Unit) {
        val m = AppModel(ApplicationProvider.getApplicationContext())
        AppModel::class.java.getDeclaredField("api").apply { isAccessible = true }.set(m, LocalApi(server.port.toLong(), "t"))
        val w = world(a)
        @Suppress("UNCHECKED_CAST")
        (AppModel::class.java.getDeclaredField("_world").apply { isAccessible = true }.get(m) as MutableStateFlow<World>).value = w
        @Suppress("UNCHECKED_CAST")
        (AppModel::class.java.getDeclaredField("_foreground").apply { isAccessible = true }.get(m) as MutableStateFlow<Boolean>).value = true
        GrokEngine.source = GrokFixture
        rule.setContent {
            CompositionLocalProvider(LocalSessions provides w.sessions.values) {
                AideTheme { Surface(Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) { content(m) } }
            }
        }
        rule.waitForIdle()
        waitFor?.let { text -> rule.waitUntil(10_000) { rule.onAllNodesWithText(text, substring = true).fetchSemanticsNodes().isNotEmpty() } }
        rule.onRoot().captureRoboImage("build/outputs/roborazzi/$name.png")
    }

    @Test fun signedOut() = shot("batchj-login-1-signed-out", claude(false)) { BotPage(it, "m:m1/claude", openSession = {}, edit = {}) {} }
    @Test fun unconfirmed() = shot("batchj-login-2-unconfirmed", claude(false, unconfirmed = true)) { BotPage(it, "m:m1/claude", openSession = {}, edit = {}) {} }
    @Test fun waiting() = shot("batchj-login-3-waiting", claude(false, login = AgentLogin("AGENT_LOGIN_STATE_WAITING"))) { BotPage(it, "m:m1/claude", openSession = {}, edit = {}) {} }
    @Test fun signedIn() = shot("batchj-login-4-signed-in", claude(true, login = AgentLogin("AGENT_LOGIN_STATE_SUCCEEDED"))) { BotPage(it, "m:m1/claude", openSession = {}, edit = {}) {} }
    @Test fun devices() = shot("batchj-login-5-devices", claude(false)) { DevicesTab(it) {} }
    @Test fun failedSession() = shot("batchj-login-6-session-failed", claude(false), waitFor = "Not logged in") { SessionPage(it, "s1", back = {}) }
}
