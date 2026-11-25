package io.aide.android.shots

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertIsSelected
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onRoot
import io.aide.android.api.Persona
import io.aide.android.model.BotLook
import io.aide.android.model.withLook
import io.aide.android.ui.ContactEditPage
import io.aide.android.ui.GrokEngine
import io.aide.android.ui.LocalSessions
import androidx.test.core.app.ApplicationProvider
import com.github.takahirom.roborazzi.captureRoboImage
import com.github.takahirom.roborazzi.captureScreenRoboImage
import com.github.takahirom.roborazzi.ExperimentalRoborazziApi
import androidx.compose.material3.SnackbarHostState
import androidx.compose.ui.test.hasScrollAction
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.onFirst
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToNode
import io.aide.android.api.DSHSession
import io.aide.android.api.DSHSessionEntry
import io.aide.android.api.QuestionItem
import io.aide.android.api.QuestionOption
import io.aide.android.ui.DSHSessionPage
import io.aide.android.ui.Onboarding
import io.aide.android.ui.Root
import io.aide.android.ui.SessionPage
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
import io.aide.android.ui.BotSettingsPage
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
                // A session's conversation (工单 #95 pictures): the owner's ask, the agent's answer, a tool call and its result, as a Connect stream.
                if (path.endsWith("StreamSessionEvents")) {
                    val b = okio.Buffer()
                    fun env(flags: Int, json: String) { val bytes = json.encodeToByteArray(); b.writeByte(flags); b.writeInt(bytes.size); b.write(bytes) }
                    events.forEach { env(0, it) }
                    env(2, "{}")
                    return MockResponse().setBody(b).setHeader("Content-Type", "application/connect+json")
                }
                if (path.endsWith("ReadDSHSession")) return MockResponse().setBody("{\"events\":[${events.joinToString(",")}]}").setHeader("Content-Type", "application/json")
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
        // In front of the owner, as the app is while a page shows: a session's page reads its conversation then.
        @Suppress("UNCHECKED_CAST")
        (AppModel::class.java.getDeclaredField("_foreground").apply { isAccessible = true }.get(m) as MutableStateFlow<Boolean>).value = true
        return m
    }

    @OptIn(ExperimentalRoborazziApi::class)
    private fun shot(name: String, waitFor: String? = null, act: (() -> Unit)? = null, content: @Composable () -> Unit) {
        // The faces' frames, drawn ahead by the same engine (工单 #83), and the sessions their states come from, as Root has them.
        GrokEngine.source = GrokFixture
        // A Surface, as the app's Scaffold: the page's colour and the text colour on it.
        rule.setContent {
            CompositionLocalProvider(LocalSessions provides world().sessions.values) {
                AideTheme { Surface(Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) { content() } }
            }
        }
        rule.waitForIdle()
        // What a page reads from the machine after it opens (the conversation): there before the picture.
        waitFor?.let { text -> rule.waitUntil(10_000) { rule.onAllNodesWithText(text, substring = true).fetchSemanticsNodes().isNotEmpty() } }
        // Something pressed first (a sheet, a dialog, a menu opened over the page): the whole screen, every window on it.
        if (act != null) {
            act()
            // A dialog's field can keep Compose from idling: waited for a few seconds at most, then the clock moves on.
            androidx.test.espresso.IdlingPolicies.setMasterPolicyTimeout(5, java.util.concurrent.TimeUnit.SECONDS)
            runCatching { rule.waitForIdle() }
            rule.mainClock.advanceTimeBy(1_000)
            org.robolectric.shadows.ShadowLooper.idleMainLooper()
            captureScreenRoboImage("build/outputs/roborazzi/$name.png")
            return
        }
        rule.onRoot().captureRoboImage("build/outputs/roborazzi/$name.png")
    }

    /** Scrolls the page's list until text is in view. */
    private fun scrollTo(text: String) {
        rule.onAllNodes(hasScrollAction()).onFirst().performScrollToNode(hasText(text))
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
        shot("11-remove-bot") { BotsTab(m) {}; io.aide.android.ui.RemoveBotDialog(m, muse, "") {} }
    }

    // 工单 #95: every page and every sheet, dialog and menu of the app, as pictures to set beside Apple's look.
    @Test fun session() = model().let { m -> shot("12-session", waitFor = "重组") { SessionPage(m, "s2", back = {}) } }
    @Test fun sessionQuestion() = model().let { m -> shot("13-session-question", waitFor = "先合并") { SessionPage(m, "s1", back = {}) } }
    @Test fun sessionSwitch() = model().let { m -> shot("14-session-switch", waitFor = "重组", act = { rule.onNodeWithContentDescription("会话列表").performClick() }) { SessionPage(m, "s2", back = {}) } }
    @Test fun sessionSettings() = model().let { m -> shot("15-session-settings", waitFor = "重组", act = { rule.onNodeWithContentDescription("会话设置").performClick() }) { SessionPage(m, "s2", back = {}) } }
    @Test fun composerMenu() = model().let { m -> shot("16-composer-menu", waitFor = "重组", act = { rule.onNodeWithContentDescription("更多").performClick() }) { SessionPage(m, "s2", back = {}) } }
    @Test fun onboarding() = model().let { m -> shot("17-onboarding") { Onboarding(m, SnackbarHostState()) } }
    @Test fun home() = model().let { m ->
        @Suppress("UNCHECKED_CAST")
        (AppModel::class.java.getDeclaredField("_phase").apply { isAccessible = true }.get(m) as MutableStateFlow<AppModel.Phase>).value = AppModel.Phase.Ready
        shot("18-home-tabs", waitFor = "概览") { Root(m) }
    }
    @Test @Config(qualifiers = "zh-rCN-w392dp-h850dp-port-notnight-xxhdpi") fun homeLight() = model().let { m ->
        @Suppress("UNCHECKED_CAST")
        (AppModel::class.java.getDeclaredField("_phase").apply { isAccessible = true }.get(m) as MutableStateFlow<AppModel.Phase>).value = AppModel.Phase.Ready
        shot("35-home-tabs-light", waitFor = "概览") { Root(m) }
    }
    // A bot's settings, a page of their own (工单 #97): DSH's, its removal asked, its pause menu, in light mode; Muse's (its grant).
    @Test fun botSettings() = model().let { m -> shot("19-bot-dsh-settings") { BotSettingsPage(m, "m:m1/deepseek", edit = {}) {} } }
    @Test fun botRemove() = model().let { m -> shot("20-bot-dsh-remove", act = { scrollTo("从 Aide 里移除…"); rule.onNodeWithText("从 Aide 里移除…").performClick() }) { BotSettingsPage(m, "m:m1/deepseek", edit = {}) {} } }
    @Test fun botPause() = model().let { m -> shot("21-bot-dsh-pause-menu", act = { rule.onNodeWithText("暂停").performClick() }) { BotSettingsPage(m, "m:m1/deepseek", edit = {}) {} } }
    @Test @Config(qualifiers = "zh-rCN-w392dp-h850dp-port-notnight-xxhdpi") fun botSettingsLight() = model().let { m -> shot("34-bot-dsh-settings-light") { BotSettingsPage(m, "m:m1/deepseek", edit = {}) {} } }
    @Test fun personalBotSettings() = model().let { m -> shot("33-bot-muse-settings") { BotSettingsPage(m, "c:c1", edit = {}) {} } }
    @Test fun accountDialog() = model().let { m -> shot("22-settings-password", waitFor = "修改密码…", act = { rule.onNodeWithText("修改密码…").performClick() }) { SettingsTab(m) } }
    @Test fun signOutDialog() = model().let { m -> shot("23-settings-sign-out", act = { rule.onAllNodesWithText("登出…").onFirst().performClick() }) { SettingsTab(m) } }
    @Test fun deleteRoom() = model().let { m -> shot("24-room-delete", waitFor = "可以发版", act = { rule.onNodeWithText("删除").performClick() }) { RoomPage(m, "m1", "room_2", openSession = {}) {} } }
    @Test fun dshSession() = model().let { m ->
        val e = DSHSessionEntry("m1", "workstation", DSHSession(sessionId = "d1", title = "整理发版检查清单", state = "DSH_SESSION_STATE_IDLE", lastActivityAt = ago(5)))
        shot("25-dsh-session", waitFor = "重组") { DSHSessionPage(m, e) {} }
    }
    @Test fun devicesManage() = model().let { m -> shot("26-devices-manage-menu", act = { rule.onAllNodesWithText("管理").onFirst().performClick() }) { DevicesTab(m) {} } }
    @Test fun addContact() = model().let { m -> shot("27-add-contact") { ContactEditPage(m, Persona(machineId = "m1", agent = "deepseek")) {} } }
    @Test @Config(qualifiers = "zh-rCN-w392dp-h850dp-port-notnight-xxhdpi") fun botsLight() = model().let { m -> shot("28-bots-light", waitFor = "发版评审") { BotsTab(m) {} } }
    @Test @Config(qualifiers = "zh-rCN-w392dp-h850dp-port-notnight-xxhdpi") fun settingsLight() = model().let { m -> shot("29-settings-light") { SettingsTab(m) } }
    @Test @Config(qualifiers = "zh-rCN-w392dp-h850dp-port-notnight-xxhdpi") fun sessionLight() = model().let { m -> shot("30-session-light", waitFor = "重组") { SessionPage(m, "s2", back = {}) } }
    @Test @Config(qualifiers = "zh-rCN-w392dp-h850dp-port-notnight-xxhdpi") fun devicesLight() = model().let { m -> shot("36-machines-light") { DevicesTab(m) {} } }

    // The bot pages at full length (工单 #97): a screen tall enough for the whole list, the list's height written down.
    @Test @Config(qualifiers = "zh-rCN-w392dp-h2600dp-port-night-xxhdpi") fun botPageFull() = model().let { m ->
        shot("31-bot-dsh-full") { BotPage(m, "m:m1/deepseek", openSession = {}, edit = {}) {} }
        length("31-bot-dsh-full")
    }
    @Test @Config(qualifiers = "zh-rCN-w392dp-h2600dp-port-night-xxhdpi") fun personalBotPageFull() = model().let { m ->
        shot("32-bot-muse-full", waitFor = "评审过了") { BotPage(m, "c:c1", openSession = {}, edit = {}) {} }
        length("32-bot-muse-full")
    }

    /** How tall the page's list is, in dp, from its first row's top to its last row's bottom: appended to lengths.txt. */
    private fun length(name: String) {
        val list = rule.onAllNodes(hasScrollAction()).onFirst().fetchSemanticsNode()
        fun bottom(n: androidx.compose.ui.semantics.SemanticsNode): Float = (n.children.map { bottom(it) } + n.boundsInRoot.bottom).max()
        val px = list.children.maxOf { bottom(it) } - list.boundsInRoot.top
        val dp = px / androidx.test.core.app.ApplicationProvider.getApplicationContext<android.content.Context>().resources.displayMetrics.density
        java.io.File("build/outputs/roborazzi").apply { mkdirs() }.resolve("lengths.txt").appendText("$name ${dp.toInt()}dp\n")
    }

    // The one avatar editor (工单 #83): DSH's new contact, a heart in magenta picked; no emoji left.
    @Test fun contactAvatar(): Unit = model().let { m ->
        shot("10-contact-avatar") { ContactEditPage(m, Persona(machineId = "m1", agent = "deepseek", name = "前端助手").withLook(BotLook("heart", "magenta"))) {} }
        rule.onNodeWithContentDescription("爱心").assertIsSelected()
        rule.onNodeWithContentDescription("品红").assertIsSelected()
        rule.onAllNodesWithText("🦊").assertCountEquals(0)
    }

    companion object {
        private val now: Instant = Instant.now()

        /** What s2 (修安卓滑动掉帧) said, as its events. */
        private val events = listOf(
            "{\"seq\":\"1\",\"userMessage\":{\"text\":\"机器页上下滑动掉帧，查一下原因\",\"from\":\"owner\"}}",
            "{\"seq\":\"2\",\"agentMessage\":{\"text\":\"先量一下滑动时每帧的重组次数，再看卡片阴影和头像解码。\"}}",
            "{\"seq\":\"3\",\"toolCall\":{\"callId\":\"c1\",\"name\":\"Bash\",\"summary\":\"./gradlew :app:testDebugUnitTest\"}}",
            "{\"seq\":\"4\",\"toolResult\":{\"callId\":\"c1\",\"summary\":\"BUILD SUCCESSFUL\"}}",
            "{\"seq\":\"5\",\"agentMessage\":{\"text\":\"找到了：滑动时脉动动画还在跑，每帧都触发整列重组。改成滑动时暂停。\"}}",
        )
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
                s("s1", "m1", "claude", SessionStatus.WAITING_INPUT, "批 F：机器页名单合并", 3, SessionProgress(pendingQuestion = Question(questionId = "q1", items = listOf(
                    QuestionItem(id = "i1", header = "合并顺序", text = "先合并哪个分支？", options = listOf(QuestionOption("批 F", "机器页名单合并"), QuestionOption("批 G", "名册复核")), allowOther = true),
                )))),
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
