package io.aide.android

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.core.app.ApplicationProvider
import io.aide.android.api.AgentLogin
import io.aide.android.api.AgentStatus
import io.aide.android.api.ClaudeDesktop
import io.aide.android.api.ErrorEvent
import io.aide.android.api.Failure
import io.aide.android.api.GetStatusResponse
import io.aide.android.api.LocalApi
import io.aide.android.api.Machine
import io.aide.android.api.MachineStatus
import io.aide.android.api.SessionEvent
import io.aide.android.api.TurnCompleted
import io.aide.android.model.AppModel
import io.aide.android.model.ConnState
import io.aide.android.model.Item
import io.aide.android.model.Transcript
import io.aide.android.model.Usable
import io.aide.android.model.World
import io.aide.android.model.codingBotState
import io.aide.android.model.machineConnections
import io.aide.android.ui.AideTheme
import io.aide.android.ui.BotPage
import kotlinx.coroutines.flow.MutableStateFlow
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.time.Instant
import java.util.Locale

/**
 * 工单 #108 on the phone, as on the desktop: Claude Code's sign-in is its command line's own — signed in, not signed in,
 * or unconfirmed, the Claude desktop app's sign-in standing in for none — and signing it in is one press on its bot's
 * page (its machine runs `claude auth login`), which follows the machine's report to signed in and back.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class, qualifiers = "zh-rCN-w392dp-h850dp-port-xxhdpi")
class BatchJLoginTest {
    @get:Rule val rule = createComposeRule()
    private val server = MockWebServer()
    private val asked = mutableListOf<String>()
    private lateinit var model: AppModel
    private lateinit var world: MutableStateFlow<World>
    private val now = Instant.now()

    @Before fun setUp() {
        Locale.setDefault(Locale.SIMPLIFIED_CHINESE)
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                if (request.path.orEmpty().endsWith("/StartMachineAgentLogin")) synchronized(asked) { asked += request.body.readUtf8() }
                return MockResponse().setBody("{}").setHeader("Content-Type", "application/json")
            }
        }
        server.start()
        model = AppModel(ApplicationProvider.getApplicationContext())
        AppModel::class.java.getDeclaredField("api").apply { isAccessible = true }.set(model, LocalApi(server.port.toLong(), "t"))
        @Suppress("UNCHECKED_CAST")
        world = AppModel::class.java.getDeclaredField("_world").apply { isAccessible = true }.get(model) as MutableStateFlow<World>
        world.value = World(status = GetStatusResponse(initialized = true, relayConnected = true, account = "owner"), machines = listOf(machine(claude(loggedIn = false))), watched = true)
    }

    @After fun tearDown() = server.shutdown()

    private fun claude(loggedIn: Boolean, unconfirmed: Boolean = false, login: AgentLogin? = null) = AgentStatus(
        "claude", available = true, version = "2.1.330", loggedIn = loggedIn, loginUnconfirmed = unconfirmed, checkedAt = now.minusSeconds(60).toString(),
        desktop = ClaudeDesktop(installed = true, signedIn = true), login = login,
    )

    private fun machine(a: AgentStatus) = Machine("m1", "workstation", online = true, status = MachineStatus(agents = listOf(a)))

    private fun count(text: String) = rule.onAllNodesWithText(text).fetchSemanticsNodes().size

    @Test fun threeStatesTheDesktopAppsSignInStandsInForNone() {
        fun cli(a: AgentStatus) = machineConnections(machine(a), now).rows.single { it.id == "claude-cli" }
        assertEquals(ConnState.OK to "已登录 · 2.1.330", cli(claude(loggedIn = true)).let { it.state to it.text })
        val out = cli(claude(loggedIn = false))
        assertEquals(ConnState.MISSING to "未登录", out.state to out.text)
        assertTrue(out.login)
        val vague = cli(claude(loggedIn = false, unconfirmed = true))
        assertEquals(ConnState.UNKNOWN to "未确认", vague.state to vague.text)
        assertTrue(vague.login && vague.lacks!!.contains("不拿 Claude Desktop 的登录代替它"))
        val bot = codingBotState(machine(claude(loggedIn = false, unconfirmed = true)), "claude", now)!!
        assertEquals(Triple("未确认", Usable.UNVERIFIED, true), Triple(bot.word, bot.usable, bot.login))
        assertFalse(codingBotState(machine(claude(loggedIn = true)), "claude", now)!!.login)
    }

    @Test fun aFailureNotSignedInIsToldApart() {
        val t = Transcript()
        t.add(SessionEvent(seq = 1, turnCompleted = TurnCompleted(isError = true, failure = Failure(kind = "FAILURE_KIND_AUTH", message = "Not logged in · Please run /login"))))
        t.add(SessionEvent(seq = 2, error = ErrorEvent(message = "Not logged in · Please run /login", failure = Failure(kind = "FAILURE_KIND_AUTH"))))
        t.add(SessionEvent(seq = 3, turnCompleted = TurnCompleted(isError = true, failure = Failure(kind = "FAILURE_KIND_NETWORK", message = "reset"))))
        assertEquals(listOf(true, true, false), t.items().map { (it as? Item.TurnEnd)?.auth ?: (it as Item.Error).auth })
    }

    @Test fun signedInFromItsBotPageAndBack() {
        rule.setContent { AideTheme { Surface(Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) { BotPage(model, "m:m1/claude", openSession = {}, edit = {}) {} } } }
        rule.waitForIdle()
        rule.onNodeWithText("未登录").assertIsDisplayed()
        assertEquals("not sent to the Devices tab or a terminal", 0, count("去设备页处理 ›"))
        rule.onNodeWithText("在 workstation 上登录").performClick()
        rule.waitUntil(5_000) { synchronized(asked) { asked.isNotEmpty() } }
        assertTrue(asked.single().contains("\"agent\":\"claude\""))

        // The machine reports it waiting for the browser, then signed in: the page follows, the entry goes.
        world.value = world.value.copy(machines = listOf(machine(claude(loggedIn = false, login = AgentLogin("AGENT_LOGIN_STATE_WAITING")))))
        rule.waitForIdle()
        rule.onNodeWithText("取消登录").assertIsDisplayed()
        world.value = world.value.copy(machines = listOf(machine(claude(loggedIn = true, login = AgentLogin("AGENT_LOGIN_STATE_SUCCEEDED")))))
        rule.waitForIdle()
        rule.onNodeWithText("已登录").assertIsDisplayed()
        assertEquals(0, count("在 workstation 上登录"))

        // Its sign-in gone: not signed in again, the entry back.
        world.value = world.value.copy(machines = listOf(machine(claude(loggedIn = false))))
        rule.waitForIdle()
        rule.onNodeWithText("未登录").assertIsDisplayed()
        rule.onNodeWithText("在 workstation 上登录").assertIsDisplayed()
    }
}
