package io.aide.android.shots

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.core.app.ApplicationProvider
import com.github.takahirom.roborazzi.ExperimentalRoborazziApi
import com.github.takahirom.roborazzi.captureScreenRoboImage
import io.aide.android.api.AgentDefaults
import io.aide.android.api.AgentModel
import io.aide.android.api.AgentStatus
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
import io.aide.android.ui.GrokEngine
import io.aide.android.ui.LocalSessions
import kotlinx.coroutines.flow.MutableStateFlow
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.util.Locale

/**
 * 批 J, the bot page's model choice (工单 #98, #99), as the owner opens it on the phone: Claude Code's list names each
 * model's specific one, the default too; DSH has the same choice, saying before its first session here that its models
 * show after it, and listing its own once a session named them. Run with `-Pshots` (PageShots' way).
 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], application = android.app.Application::class, qualifiers = "zh-rCN-w392dp-h850dp-port-night-xxhdpi")
class ModelShots {
    @get:Rule val rule = createComposeRule()

    @Before fun chinese() = Locale.setDefault(Locale.SIMPLIFIED_CHINESE)

    private val claude = AgentStatus(
        "claude", available = true, loggedIn = true, version = "2.1.330",
        models = listOf(
            AgentModel("sonnet", "Sonnet", resolved = "claude-sonnet-5-5"), AgentModel("fable", "Fable", resolved = "claude-fable-5-2"),
            AgentModel("opus", "Opus", resolved = "claude-opus-5-5"), AgentModel("haiku", "Haiku", resolved = "claude-haiku-5-5"),
        ),
        own = AgentDefaults(model = "claude-sonnet-5-5"),
    )
    private val dshModels = listOf(AgentModel("deepseek/v4-pro", "DEEPSEEK/V4-PRO (Models)"), AgentModel("deepseek/v4-flash", "DEEPSEEK/V4-FLASH (Models)"))

    private fun world(dsh: AgentStatus, sessions: List<Session> = emptyList()) = World(
        status = GetStatusResponse(initialized = true, relayConnected = true, version = "0.2.9", account = "owner"),
        machines = listOf(Machine(machineId = "m1", machineName = "workstation", online = true, version = "0.2.9", status = MachineStatus(platform = "linux/amd64", maxSessions = 4, version = "0.2.9", agents = listOf(claude, dsh)))),
        projects = listOf(Project("m1", "aide", name = "Aide")),
        sessions = sessions.associateBy { it.sessionId },
        watched = true,
    )

    @OptIn(ExperimentalRoborazziApi::class)
    private fun open(w: World, bot: String, chip: String, name: String) {
        GrokEngine.source = GrokFixture
        val m = AppModel(ApplicationProvider.getApplicationContext())
        AppModel::class.java.getDeclaredField("api").apply { isAccessible = true }.set(m, LocalApi(1, "t"))
        @Suppress("UNCHECKED_CAST")
        (AppModel::class.java.getDeclaredField("_world").apply { isAccessible = true }.get(m) as MutableStateFlow<World>).value = w
        rule.setContent {
            CompositionLocalProvider(LocalSessions provides w.sessions.values) {
                AideTheme { Surface(Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) { BotPage(m, bot, openSession = {}, edit = {}) {} } }
            }
        }
        rule.waitForIdle()
        rule.onNodeWithText(chip).performClick()
        rule.waitForIdle()
        rule.mainClock.advanceTimeBy(1_000)
        org.robolectric.shadows.ShadowLooper.idleMainLooper()
        captureScreenRoboImage("build/outputs/roborazzi/$name.png")
    }

    private fun shown(text: String) = assertEquals(text, 1, rule.onAllNodesWithText(text).fetchSemanticsNodes().size.coerceAtMost(1))

    @Test fun claudeModels() {
        open(world(AgentStatus("deepseek", available = true, loggedIn = true)), "m:m1/claude", "默认 · Sonnet 5.5", "31-bot-claude-models")
        listOf("默认 · Sonnet 5.5 · claude-sonnet-5-5", "Sonnet 5.5 · claude-sonnet-5-5", "Fable 5.2 · claude-fable-5-2", "Opus 5.5 · claude-opus-5-5", "Haiku 5.5 · claude-haiku-5-5").forEach(::shown)
        assertEquals(0, rule.onAllNodesWithText("Opus").fetchSemanticsNodes().size) // never the family alone
    }

    @Test fun dshBeforeItsFirstSession() {
        open(world(AgentStatus("deepseek", available = true, loggedIn = true)), "m:m1/deepseek", "默认模型", "32-bot-dsh-models-before")
        shown("DSH 只在会话里告诉可用的模型：先用默认模型发出第一条，之后这里列出它在这台机器上的实际模型")
    }

    @Test fun dshModels() {
        val ran = Session(sessionId = "s1", machineId = "m1", projectId = "aide", agent = "deepseek", status = SessionStatus.IDLE, createdBy = "owner", title = "看看配置", model = "deepseek/v4-pro", createdAt = "2026-11-23T05:00:00Z", updatedAt = "2026-11-23T05:01:00Z")
        open(world(AgentStatus("deepseek", available = true, loggedIn = true, models = dshModels, own = AgentDefaults(model = "deepseek/v4-pro")), listOf(ran)), "m:m1/deepseek", "DEEPSEEK/V4-PRO (Models)", "33-bot-dsh-models")
        listOf("默认 · DEEPSEEK/V4-PRO (Models) · deepseek/v4-pro", "DEEPSEEK/V4-PRO (Models) · deepseek/v4-pro", "DEEPSEEK/V4-FLASH (Models) · deepseek/v4-flash").forEach(::shown)
    }
}
