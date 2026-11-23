package io.aide.android

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsNotSelected
import androidx.compose.ui.test.assertIsOff
import androidx.compose.ui.test.assertIsOn
import androidx.compose.ui.test.assertIsSelected
import androidx.compose.ui.test.hasScrollAction
import androidx.compose.ui.test.hasSetTextAction
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.isSelectable
import androidx.compose.ui.test.isToggleable
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onFirst
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToNode
import androidx.compose.ui.test.performTextReplacement
import androidx.test.core.app.ApplicationProvider
import io.aide.android.api.AgentPause
import io.aide.android.api.AgentStatus
import io.aide.android.api.Client
import io.aide.android.api.GetStatusResponse
import io.aide.android.api.LocalApi
import io.aide.android.api.Machine
import io.aide.android.api.MachineStatus
import io.aide.android.api.Project
import io.aide.android.api.Session
import io.aide.android.api.SessionProgress
import io.aide.android.api.SessionStatus
import io.aide.android.model.AppModel
import io.aide.android.model.Buckets
import io.aide.android.model.World
import io.aide.android.model.roster
import io.aide.android.ui.AideTheme
import io.aide.android.ui.BotPage
import io.aide.android.ui.BotSettingsPage
import io.aide.android.ui.Root
import io.aide.android.ui.SessionsTab
import io.aide.android.ui.statusLine
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonObject
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
import java.time.Instant
import java.util.Locale

/**
 * 批 J, group B: a bot's settings on a page of their own (工单 #97) — its page sets nothing and has a Settings entry, the
 * settings page has every item once, and what is changed there shows on its page — and its tasks the same as the tasks
 * tab's (工单 #102). Through the app's own pages against a local core that keeps what it is told.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class, qualifiers = "zh-rCN-w392dp-h850dp-port-xxhdpi")
class BatchJBotSettingsTest {
    @get:Rule val rule = createComposeRule()
    private val server = MockWebServer()
    /** What the core was asked, by method: the bodies, in order. */
    private val asked = mutableMapOf<String, MutableList<String>>()
    /** The contacts the core keeps. */
    private val contacts = mutableListOf<JsonObject>()
    private lateinit var model: AppModel
    private lateinit var world: MutableStateFlow<World>

    @Before fun setUp() {
        Locale.setDefault(Locale.SIMPLIFIED_CHINESE)
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                val method = request.path.orEmpty().substringAfterLast('/')
                val body = request.body.readUtf8()
                synchronized(asked) { asked.getOrPut(method) { mutableListOf() } += body }
                val out = when (method) {
                    "ListPersonas" -> synchronized(contacts) { "{\"personas\":[${contacts.joinToString(",")}]}" }
                    "SavePersona" -> {
                        val p = Json.parseToJsonElement(body).jsonObject["persona"]!!.jsonObject
                        val kept = JsonObject(p + ("personaId" to JsonPrimitive("p1")))
                        synchronized(contacts) { contacts.removeAll { true }; contacts += kept }
                        "{\"persona\":$kept}"
                    }
                    else -> "{}"
                }
                return MockResponse().setBody(out).setHeader("Content-Type", "application/json")
            }
        }
        server.start()
        model = AppModel(ApplicationProvider.getApplicationContext())
        AppModel::class.java.getDeclaredField("api").apply { isAccessible = true }.set(model, LocalApi(server.port.toLong(), "t"))
        @Suppress("UNCHECKED_CAST")
        world = AppModel::class.java.getDeclaredField("_world").apply { isAccessible = true }.get(model) as MutableStateFlow<World>
        world.value = world()
    }

    @After fun tearDown() = server.shutdown()

    private fun show(content: @Composable () -> Unit) {
        rule.setContent { AideTheme { Surface(Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) { content() } } }
        rule.waitForIdle()
    }

    /** The world as the Watch would bring it once the machine has done what it was asked. */
    private fun deepseek(change: (AgentStatus) -> AgentStatus) {
        val w = world.value
        world.value = w.copy(machines = w.machines.map { m -> m.copy(status = m.status!!.copy(agents = m.status!!.agents.map { if (it.name == "deepseek") change(it) else it })) })
        rule.waitForIdle()
    }

    private fun count(text: String) = rule.onAllNodesWithText(text).fetchSemanticsNodes().size

    private fun scrollTo(text: String) = rule.onAllNodes(hasScrollAction()).onFirst().performScrollToNode(hasText(text))

    private fun waitFor(text: String) = rule.waitUntil(5_000) { count(text) > 0 }

    @Test fun aBotPageSetsNothingAndHasItsSettingsEntry() {
        show { BotPage(model, "m:m1/deepseek", openSession = {}, edit = {}) {} }
        rule.onNodeWithText("设置").assertIsDisplayed()
        rule.onNodeWithText("任务动态").assertIsDisplayed()
        for (gone in listOf("它的设置", "机器", "项目", "类别", "自动判断 · 本机编码 Agent", "名字和头像", "名字与头像", "给它起名字、换头像", "暂停", "从 Aide 里移除…", "接入凭据")) {
            assertEquals("「$gone」is not on the bot page", 0, count(gone))
        }
    }

    @Test fun aPersonalAgentsPageHasNoGrantCard() {
        show { BotPage(model, "c:c1", openSession = {}, edit = {}) {} }
        rule.onNodeWithText("设置").assertIsDisplayed()
        rule.onNodeWithText("任务动态").assertIsDisplayed()
        assertEquals(0, count("接入凭据"))
        assertEquals(0, count("它派出的任务"))
        // Its task: the one it handed out, with its state.
        rule.onNodeWithText("批 G 名册复核").assertIsDisplayed()
    }

    @Test fun theSettingsPageHasEveryItemOnce() {
        show { BotSettingsPage(model, "m:m1/deepseek", edit = {}) {} }
        for (once in listOf("名字和头像", "给它起名字、换头像", "机器", "项目", "类别", "自动判断", "个人 Agent", "本机编码 Agent · workstation", "暂停")) {
            scrollTo(once)
            assertEquals("「$once」once", 1, count(once))
        }
        scrollTo("从 Aide 里移除…")
        assertEquals(1, count("从 Aide 里移除…"))
        assertEquals("the two of before are one", 0, count("名字与头像"))
        // The kind laid out flat (工单 #97 补充三): three radio rows, the one in force picked; no pill with an arrow.
        assertEquals(3, rule.onAllNodes(isSelectable()).fetchSemanticsNodes().size)
        rule.onNode(isSelectable() and hasText("自动判断")).assertIsSelected()
        assertEquals(0, count("自动判断 · 本机编码 Agent"))
        rule.onNode(isToggleable()).assertIsOff()
    }

    @Test fun aGrantsSettingsHaveItsCardOnce() {
        show { BotSettingsPage(model, "c:c1", edit = {}) {} }
        assertEquals(1, count("接入凭据"))
        assertEquals(1, count("删除…"))
    }

    /** Through the app: the roster, DSH's page, its Settings; changed there, seen back on its page and in the roster. */
    @Test fun whatIsChangedInSettingsShowsOnItsPage() {
        world.value = world().copy(clients = emptyList())
        @Suppress("UNCHECKED_CAST")
        (AppModel::class.java.getDeclaredField("_phase").apply { isAccessible = true }.get(model) as MutableStateFlow<AppModel.Phase>).value = AppModel.Phase.Ready
        show { Root(model) }
        rule.onNodeWithText("DSH").performClick()
        rule.waitForIdle()
        rule.onNodeWithText("设置").performClick()
        rule.waitForIdle()

        // Its kind: personal, asked of its machine; once the machine says so, picked here and filed so in the roster.
        rule.onNode(isSelectable() and hasText("个人 Agent")).performClick()
        rule.waitUntil(5_000) { asked["SetAgentCategory"].orEmpty().isNotEmpty() }
        assertTrue(asked["SetAgentCategory"]!!.single().contains("\"category\":\"personal\""))
        deepseek { it.copy(category = "personal") }
        rule.onNode(isSelectable() and hasText("个人 Agent")).assertIsSelected()
        rule.onNode(isSelectable() and hasText("自动判断")).assertIsNotSelected()

        // Paused: the switch; on its page, its state says so.
        rule.onNode(isToggleable()).performClick()
        rule.waitUntil(5_000) { asked["SetAgentPaused"].orEmpty().isNotEmpty() }
        assertTrue(asked["SetAgentPaused"]!!.single().contains("\"paused\":true"))
        deepseek { it.copy(pause = AgentPause(since = Instant.now().toString())) }
        rule.onNode(isToggleable()).assertIsOn()

        // Its name: the one names-and-faces section, its contact page, saved; back here and on its page, the new name.
        rule.onNodeWithText("给它起名字、换头像").performClick()
        rule.waitForIdle()
        rule.onNode(hasSetTextAction()).performTextReplacement("前端助手")
        rule.onAllNodes(hasScrollAction()).onFirst().performScrollToNode(hasText("保存"))
        rule.onNodeWithText("保存").performClick()
        waitFor("再加一个名字和头像")
        assertTrue(count("前端助手") > 0)

        rule.onNodeWithText("返回").performClick()
        rule.waitForIdle()
        assertTrue("its page has its new name", count("前端助手") > 0)
        assertEquals(0, count("DSH"))
        rule.onNodeWithText("已暂停").assertIsDisplayed()
        // Filed personal: in the roster's personal agents.
        rule.onNodeWithText("返回").performClick()
        rule.waitForIdle()
        rule.onNodeWithText("个人 agent").assertIsDisplayed()
        assertEquals(1, count("前端助手"))
    }

    @Test fun removedInSettingsItsPageSaysItIsGone() {
        @Suppress("UNCHECKED_CAST")
        (AppModel::class.java.getDeclaredField("_phase").apply { isAccessible = true }.get(model) as MutableStateFlow<AppModel.Phase>).value = AppModel.Phase.Ready
        show { Root(model) }
        rule.onNodeWithText("DSH").performClick()
        rule.waitForIdle()
        rule.onNodeWithText("设置").performClick()
        rule.waitForIdle()
        scrollTo("从 Aide 里移除…")
        rule.onNodeWithText("从 Aide 里移除…").performClick()
        rule.waitForIdle()
        rule.onNodeWithText("移除").performClick()
        rule.waitUntil(5_000) { asked["SetAgentHidden"].orEmpty().isNotEmpty() }
        assertTrue(asked["SetAgentHidden"]!!.single().contains("\"hidden\":true"))
        deepseek { it.copy(hidden = true) }
        rule.onNodeWithText("这个 bot 已经不在了。").assertIsDisplayed()
        rule.onNodeWithText("返回").performClick()
        rule.waitForIdle()
        rule.onNodeWithText("这个 bot 已经不在了。").assertIsDisplayed()
    }

    /** 工单 #102: a bot's tasks and the tasks tab are one reckoning; the same task reads the same on both. */
    @Test fun aTaskReadsTheSameOnItsBotPageAndTheTasksTab() {
        val w = world.value
        val dsh = roster(w.machines, w.clients, w.sessions.values, Instant.now()).all.first { it.key == "m:m1/deepseek" }
        val mine = w.bucketsOf(dsh)
        val all = w.buckets
        assertEquals(listOf("s2"), mine.inProgress.map { it.sessionId })
        assertTrue(all.inProgress.any { it.sessionId == "s2" })
        // Stopped by a failure: waiting for the owner, on both.
        assertEquals(listOf("s6"), mine.needsYou.map { it.sessionId })
        assertTrue(all.needsYou.any { it.sessionId == "s6" })
        // Its machine lost: the state is not known now, on both alike.
        val lost = w.copy(machines = w.machines.map { it.copy(unreachable = "offline") })
        val lostDsh = lost.bucketsOf(dsh)
        val lostAll = lost.buckets
        for (pick in listOf(Buckets::needsYou, Buckets::unseen, Buckets::inProgress, Buckets::recent)) {
            assertEquals(pick(lostDsh), pick(lostAll).filter { it.agent == "deepseek" })
        }

        val running = statusLine(w.shown(w.sessions.getValue("s2")), w)
        var tab by mutableStateOf(true)
        show { if (tab) SessionsTab(model) {} else BotPage(model, "m:m1/deepseek", openSession = {}, edit = {}) {} }
        rule.onNodeWithText("修安卓滑动掉帧").assertIsDisplayed()
        assertEquals(1, count(running))
        tab = false
        rule.waitForIdle()
        assertEquals(1, count("任务动态"))
        assertEquals(1, count(running))
    }

    private fun world(): World {
        val now = Instant.now()
        fun ago(min: Long) = now.minusSeconds(min * 60).toString()
        val m = Machine(
            machineId = "m1", machineName = "workstation", online = true, version = "0.2.9",
            status = MachineStatus(platform = "windows/amd64", version = "0.2.9", agents = listOf(
                AgentStatus("claude", available = true, loggedIn = true, checkedAt = ago(1)),
                AgentStatus("deepseek", available = true, loggedIn = true, checkedAt = ago(1)),
            )),
        )
        fun s(id: String, agent: String, status: String, title: String, min: Long, p: SessionProgress? = null) =
            Session(sessionId = id, machineId = "m1", projectId = "aide", agent = agent, status = status, createdBy = "owner", title = title, createdAt = ago(min + 30), updatedAt = ago(min), progress = p)
        return World(
            status = GetStatusResponse(initialized = true, relayConnected = true, version = "0.2.9", account = "owner"),
            machines = listOf(m),
            projects = listOf(Project("m1", "aide", name = "Aide")),
            clients = listOf(Client(clientId = "c1", grantId = "c1", label = "Muse", online = true)),
            sessions = listOf(
                s("s2", "deepseek", SessionStatus.RUNNING, "修安卓滑动掉帧", 1, SessionProgress(currentTool = "Bash", lastEventAt = ago(0))),
                s("s6", "deepseek", SessionStatus.CRASHED, "Go 内存审计第二步", 300),
                s("s4", "claude", SessionStatus.IDLE, "交接仓瘦身", 50),
                s("s8", "claude", SessionStatus.IDLE, "批 G 名册复核", 20).copy(createdBy = "c1"),
            ).associateBy { it.sessionId },
            watched = true,
        )
    }
}
