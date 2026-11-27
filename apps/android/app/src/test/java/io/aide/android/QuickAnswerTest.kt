package io.aide.android

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.core.app.ApplicationProvider
import io.aide.android.api.GetStatusResponse
import io.aide.android.api.LocalApi
import io.aide.android.api.Machine
import io.aide.android.api.MachineStatus
import io.aide.android.api.Project
import io.aide.android.api.Question
import io.aide.android.api.QuestionItem
import io.aide.android.api.QuestionOption
import io.aide.android.api.Session
import io.aide.android.api.SessionProgress
import io.aide.android.api.SessionStatus
import io.aide.android.model.AppModel
import io.aide.android.model.World
import io.aide.android.ui.AideTheme
import io.aide.android.ui.PageFeedback
import io.aide.android.ui.SessionsTab
import io.aide.android.ui.quickChoiceItem
import kotlinx.coroutines.flow.MutableStateFlow
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.time.Instant
import java.util.Locale
import java.util.concurrent.TimeUnit

/**
 * 工单 #177: the Tasks tab's one-tap answers (工单 #173). Tapped twice, or one option then another, while the first
 * answer is on its way, they send one answer. A question with more items than one, optional ones too, gets none:
 * it is answered on its page, where each item shows.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class, qualifiers = "zh-rCN-w392dp-h850dp-port-xxhdpi")
class QuickAnswerTest {
    @get:Rule val rule = createComposeRule()
    private val server = MockWebServer()
    private val answers = mutableListOf<String>()
    private lateinit var model: AppModel
    private lateinit var world: MutableStateFlow<World>

    private val now = Instant.now()
    private fun ago(min: Long) = now.minusSeconds(min * 60).toString()
    private fun waiting(id: String, title: String, q: Question) = Session(
        sessionId = id, machineId = "m1", projectId = "aide", agent = "claude", status = SessionStatus.WAITING_INPUT, createdBy = "owner",
        title = title, createdAt = ago(30), updatedAt = ago(1), progress = SessionProgress(pendingQuestion = q),
    )
    private val choice = QuestionItem(id = "go", text = "要合并吗", options = listOf(QuestionOption("合并"), QuestionOption("先不")))
    private val one = waiting("s1", "发版前检查", Question(questionId = "q1", items = listOf(choice)))
    private val more = waiting("s2", "迁移数据库", Question(questionId = "q2", items = listOf(
        QuestionItem(id = "how", text = "怎么迁移", options = listOf(QuestionOption("在线迁移"), QuestionOption("停机迁移"))),
        QuestionItem(id = "note", text = "补充说明", optional = true),
    )))

    @Before fun setUp() {
        Locale.setDefault(Locale.SIMPLIFIED_CHINESE)
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                val method = request.path.orEmpty().substringAfterLast('/')
                if (method == "AnswerQuestion") synchronized(answers) { answers += request.body.readUtf8() }
                // Slow, as over a phone's network: the second tap comes while the first answer is on its way.
                return MockResponse().setBody("{}").setHeader("Content-Type", "application/json").setBodyDelay(600, TimeUnit.MILLISECONDS)
            }
        }
        server.start()
        model = AppModel(ApplicationProvider.getApplicationContext())
        AppModel::class.java.getDeclaredField("api").apply { isAccessible = true }.set(model, LocalApi(server.port.toLong(), "t"))
        @Suppress("UNCHECKED_CAST")
        world = AppModel::class.java.getDeclaredField("_world").apply { isAccessible = true }.get(model) as MutableStateFlow<World>
        world.value = World(
            status = GetStatusResponse(initialized = true, relayConnected = true, version = "0.2.14", account = "owner"),
            machines = listOf(Machine(machineId = "m1", machineName = "workstation", online = true, status = MachineStatus(platform = "linux/amd64"))),
            projects = listOf(Project("m1", "aide", name = "Aide")),
            sessions = listOf(one, more).associateBy { it.sessionId },
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
    private fun sent() = synchronized(answers) { answers.toList() }

    @Test fun tappedAgainWhileItGoesItSendsOneAnswer() {
        rule.onNodeWithText("合并").performClick()
        rule.onNodeWithText("合并").performClick()
        rule.onNodeWithText("先不").performClick()
        rule.waitUntil(5_000) { sent().isNotEmpty() }
        Thread.sleep(1_500) // every answer that would go has gone, the first come back
        rule.waitForIdle()
        assertEquals(1, sent().size)
        assert("\"合并\"" in sent()[0]) { sent()[0] }
        // Back (the machine did not take it here: the session still waits), the chips answer again.
        rule.onNodeWithText("先不").performClick()
        rule.waitUntil(5_000) { sent().size == 2 }
    }

    @Test fun aQuestionOfMoreItemsIsAnsweredOnItsPage() {
        assertEquals(1, count("迁移数据库"))
        assertEquals(0, count("在线迁移"))
        assertNull(quickChoiceItem(more.progress!!.pendingQuestion!!))
        assertNotNull(quickChoiceItem(one.progress!!.pendingQuestion!!))
    }
}
