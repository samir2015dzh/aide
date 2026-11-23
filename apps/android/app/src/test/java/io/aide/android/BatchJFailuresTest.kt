package io.aide.android

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.performClick
import androidx.test.core.app.ApplicationProvider
import com.github.takahirom.roborazzi.captureRoboImage
import io.aide.android.api.Failure
import io.aide.android.api.GetStatusResponse
import io.aide.android.api.LocalApi
import io.aide.android.api.Machine
import io.aide.android.api.Session
import io.aide.android.api.SessionEvent
import io.aide.android.api.SessionProgress
import io.aide.android.api.SessionStatus
import io.aide.android.api.TurnCompleted
import io.aide.android.model.AppModel
import io.aide.android.model.Item
import io.aide.android.model.Transcript
import io.aide.android.model.World
import io.aide.android.model.failureAdvice
import io.aide.android.model.failureHeadline
import io.aide.android.ui.AideTheme
import io.aide.android.ui.SessionPage
import kotlinx.coroutines.flow.MutableStateFlow
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
import org.robolectric.annotation.GraphicsMode
import java.util.Locale

/**
 * 工单 #105, as the desktop's batchj-failures spec: a failed turn says what happened in words the owner can act on —
 * which agent, what kind of problem, what to do — and keeps the agent's raw message folded under 详情. What the
 * machine could not tell apart says so, and that the original is in 详情. Through the session page, against a local
 * core that streams the session's events as the machine sorted them (internal/provider/failure.go).
 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], application = android.app.Application::class, qualifiers = "zh-rCN-w392dp-h850dp-port-xxhdpi")
open class BatchJFailuresTest {
    @get:Rule val rule = createComposeRule()
    private val server = MockWebServer()
    private var events: List<String> = emptyList()

    @Before fun setUp() {
        Locale.setDefault(Locale.SIMPLIFIED_CHINESE)
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                if (request.path.orEmpty().endsWith("StreamSessionEvents")) {
                    val b = okio.Buffer()
                    fun env(flags: Int, json: String) { val bytes = json.encodeToByteArray(); b.writeByte(flags); b.writeInt(bytes.size); b.write(bytes) }
                    events.forEach { env(0, it) }
                    env(2, "{}")
                    return MockResponse().setBody(b).setHeader("Content-Type", "application/connect+json")
                }
                return MockResponse().setBody("{}").setHeader("Content-Type", "application/json")
            }
        }
        server.start()
    }

    @After fun tearDown() = server.shutdown()

    private fun json(s: String) = "\"" + s.replace("\\", "\\\\").replace("\"", "\\\"") + "\""

    /** The session page of a DSH session whose turn failed with failure (the machine's JSON of it). */
    private fun failedPage(failure: String) {
        events = listOf(
            "{\"seq\":\"1\",\"userMessage\":{\"text\":\"把登录页的报错文案改一下\",\"from\":\"owner\"}}",
            "{\"seq\":\"2\",\"turnCompleted\":{\"turn\":1,\"isError\":true,\"failure\":$failure}}",
        )
        val m = AppModel(ApplicationProvider.getApplicationContext())
        AppModel::class.java.getDeclaredField("api").apply { isAccessible = true }.set(m, LocalApi(server.port.toLong(), "t"))
        val s = Session(sessionId = "s1", machineId = "m1", projectId = "aide", agent = "deepseek", status = SessionStatus.IDLE, createdBy = "owner", title = "改登录页文案",
            progress = SessionProgress(lastFailure = Failure(kind = "FAILURE_KIND_MISSING_CREDENTIALS")))
        @Suppress("UNCHECKED_CAST")
        (AppModel::class.java.getDeclaredField("_world").apply { isAccessible = true }.get(m) as MutableStateFlow<World>).value =
            World(status = GetStatusResponse(initialized = true, relayConnected = true), machines = listOf(Machine(machineId = "m1", machineName = "workstation", online = true)), sessions = mapOf("s1" to s), watched = true)
        @Suppress("UNCHECKED_CAST")
        (AppModel::class.java.getDeclaredField("_foreground").apply { isAccessible = true }.get(m) as MutableStateFlow<Boolean>).value = true
        rule.setContent { AideTheme { Surface(Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) { SessionPage(m, "s1", back = {}) } } }
        rule.waitUntil(10_000) { rule.onAllNodesWithText("把登录页的报错文案改一下").fetchSemanticsNodes().isNotEmpty() }
    }

    private fun shown(text: String) = assertEquals(text, 1, rule.onAllNodesWithText(text, substring = true).fetchSemanticsNodes().size.coerceAtMost(1))
    private fun hidden(text: String) = assertEquals(text, 0, rule.onAllNodesWithText(text, substring = true).fetchSemanticsNodes().size)

    /** A picture of the page as it is, when pictures are recorded (-Pshots, shots/FailureShots); nothing otherwise. */
    private fun shot(name: String) = rule.onRoot().captureRoboImage("build/outputs/roborazzi/$name.png")

    @Test fun aMissingKeyIsSaidInWordsAndTheRawMessageOnlyUnderDetails() {
        failedPage("{\"kind\":\"FAILURE_KIND_MISSING_CREDENTIALS\",\"provider\":\"DeepSeek\",\"message\":${json("$NO_KEY (code -32603)")}}")
        shown("DSH 还没配 DeepSeek 的密钥")
        shown("在 DSH 的模型设置里填好 DeepSeek 的密钥，再发一条消息就能接着做。")
        for (raw in listOf("DEEPSEEK_API_KEY", "deepseek-official", "-32603", "Internal error")) hidden(raw)
        shot("batchj-105-android-nokey")
        rule.onNodeWithText("详情").performClick()
        rule.waitForIdle()
        shown("$NO_KEY (code -32603)")
        shot("batchj-105-android-nokey-details")
    }

    @Test fun oneThatCantBeToldApartSaysSoAndThatTheOriginalIsUnderDetails() {
        failedPage("{\"kind\":\"FAILURE_KIND_UNKNOWN\",\"message\":${json("$ODD (code -32603)")}}")
        shown("DSH 出了个内部错误")
        shown("详情里有原文。")
        hidden("tool registry")
        shot("batchj-105-android-unknown")
        rule.onNodeWithText("详情").performClick()
        rule.waitForIdle()
        shown("$ODD (code -32603)")
        shot("batchj-105-android-unknown-details")
    }

    // The words, as the desktop's @aide/core says them; the provider only when the machine found one.
    @Test fun theWordsOfEachKind() {
        assertEquals("DSH 还没配模型服务的密钥", failureHeadline("FAILURE_KIND_MISSING_CREDENTIALS", "DSH"))
        assertEquals("DSH 连不上 DeepSeek", failureHeadline("FAILURE_KIND_NETWORK", "DSH", "DeepSeek"))
        assertEquals("DSH 被 DeepSeek 限流了", failureHeadline("FAILURE_KIND_RATE_LIMIT", "DSH", "DeepSeek"))
        assertEquals("本轮执行已超时", failureHeadline("FAILURE_KIND_TIMEOUT", "DSH"))
        assertEquals("DSH 意外退出了", failureHeadline("FAILURE_KIND_AGENT_CRASHED", "DSH"))
        assertEquals("DSH 出了个内部错误", failureHeadline("FAILURE_KIND_SOMETHING_NEWER", "DSH"))
        assertEquals("详情里有原文。", failureAdvice("FAILURE_KIND_SOMETHING_NEWER", "DSH"))
        assertEquals("再次发送消息即可恢复任务。", failureAdvice("FAILURE_KIND_TIMEOUT", "DSH"))
        Locale.setDefault(Locale.ENGLISH)
        assertEquals("DSH has no DeepSeek API key yet", failureHeadline("FAILURE_KIND_MISSING_CREDENTIALS", "DSH", "DeepSeek"))
        Locale.setDefault(Locale.SIMPLIFIED_CHINESE)
        // The transcript keeps what the machine made of it beside the raw message.
        val t = Transcript()
        t.add(SessionEvent(seq = 2, turnCompleted = TurnCompleted(isError = true, failure = Failure(kind = "FAILURE_KIND_MISSING_CREDENTIALS", message = NO_KEY, provider = "DeepSeek"))))
        assertEquals(Item.TurnEnd("e2", true, NO_KEY, "FAILURE_KIND_MISSING_CREDENTIALS", "DeepSeek"), t.items().last())
    }

    companion object {
        const val NO_KEY = "Internal error: turn failed: llm-deepseek: no API key for provider route \"deepseek-official\"; store DEEPSEEK_API_KEY through the credentials service (the web Models page writes it), or export DEEPSEEK_API_KEY in the launching environment"
        const val ODD = "Internal error: turn failed: tool registry corrupted"
    }
}
