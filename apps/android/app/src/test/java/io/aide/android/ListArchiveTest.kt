package io.aide.android

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.longClick
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTouchInput
import androidx.test.core.app.ApplicationProvider
import io.aide.android.api.GetStatusResponse
import io.aide.android.api.LocalApi
import io.aide.android.api.Machine
import io.aide.android.api.MachineStatus
import io.aide.android.api.Project
import io.aide.android.api.Session
import io.aide.android.api.SessionProgress
import io.aide.android.api.SessionStatus
import io.aide.android.model.AppModel
import io.aide.android.model.World
import io.aide.android.ui.AideTheme
import io.aide.android.ui.PageFeedback
import io.aide.android.ui.SessionsTab
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.time.Instant
import java.util.Locale

/**
 * 工单 #165: a session is archived from the list it is in — a long press, then 归档 — and leaves it at once; the line
 * that says so undoes it for a few seconds. One with a turn running offers it only as a disabled entry that says why.
 * Through the app's own Tasks tab against a local core that keeps what it is told.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class, qualifiers = "zh-rCN-w392dp-h850dp-port-xxhdpi")
class ListArchiveTest {
    @get:Rule val rule = createComposeRule()
    private val server = MockWebServer()
    private val asked = mutableListOf<Pair<String, String>>()
    private lateinit var model: AppModel
    private lateinit var world: MutableStateFlow<World>

    private val now = Instant.now()
    private fun ago(min: Long) = now.minusSeconds(min * 60).toString()
    private fun s(id: String, status: String, title: String, min: Long, p: SessionProgress? = null) =
        Session(sessionId = id, machineId = "m1", projectId = "aide", agent = "claude", status = status, createdBy = "owner", title = title, createdAt = ago(min + 30), updatedAt = ago(min), progress = p)
    private val done = s("s4", SessionStatus.IDLE, "交接仓瘦身", 50)
    private val running = s("s2", SessionStatus.RUNNING, "修安卓滑动掉帧", 1, SessionProgress(currentTool = "Bash", lastEventAt = ago(0)))

    @Before fun setUp() {
        Locale.setDefault(Locale.SIMPLIFIED_CHINESE)
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                val method = request.path.orEmpty().substringAfterLast('/')
                val id = runCatching { Json.parseToJsonElement(request.body.readUtf8()).jsonObject["sessionId"]?.jsonPrimitive?.content }.getOrNull().orEmpty()
                synchronized(asked) { asked += method to id }
                val out = when (method) {
                    "ArchiveSession" -> "{\"session\":${Json.encodeToString(Session.serializer(), done.copy(status = SessionStatus.ARCHIVED))}}"
                    "UnarchiveSession" -> "{\"session\":${Json.encodeToString(Session.serializer(), done)}}"
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
        world.value = World(
            status = GetStatusResponse(initialized = true, relayConnected = true, version = "0.2.11", account = "owner"),
            machines = listOf(Machine(machineId = "m1", machineName = "workstation", online = true, status = MachineStatus(platform = "linux/amd64"))),
            projects = listOf(Project("m1", "aide", name = "Aide")),
            sessions = listOf(done, running).associateBy { it.sessionId },
            watched = true,
        )
        rule.setContent {
            AideTheme {
                Scaffold(Modifier.fillMaxSize(), snackbarHost = { PageFeedback(model) }, containerColor = MaterialTheme.colorScheme.background) { pad ->
                    androidx.compose.foundation.layout.Box(Modifier.padding(pad)) { SessionsTab(model) {} }
                }
            }
        }
        rule.waitForIdle()
    }

    @After fun tearDown() = server.shutdown()

    private fun count(text: String) = rule.onAllNodesWithText(text).fetchSemanticsNodes().size

    @Test fun aLongPressArchivesItAndTheLineUndoesIt() {
        rule.onNodeWithText("交接仓瘦身").performTouchInput { longClick() }
        rule.onNodeWithText("归档").performClick()
        // Gone from the list at once, and said, with an undo.
        rule.waitUntil(5_000) { count("已归档「交接仓瘦身」") > 0 }
        assertEquals(0, count("交接仓瘦身"))
        rule.waitUntil(5_000) { synchronized(asked) { "ArchiveSession" to "s4" in asked } }
        rule.onNodeWithText("撤销").performClick()
        rule.waitUntil(5_000) { synchronized(asked) { "UnarchiveSession" to "s4" in asked } }
        rule.waitUntil(5_000) { count("交接仓瘦身") > 0 }
    }

    @Test fun oneWithATurnRunningSaysWhyNot() {
        rule.onNodeWithText("修安卓滑动掉帧").performTouchInput { longClick() }
        rule.onNodeWithText("归档（进行中或等你回答时不能归档）").assertIsNotEnabled()
        assertEquals(0, synchronized(asked) { asked.count { it.first == "ArchiveSession" } })
    }
}
