package io.aide.android.shots

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.performTouchInput
import androidx.test.core.app.ApplicationProvider
import io.aide.android.api.LocalApi
import io.aide.android.api.Session
import io.aide.android.api.SessionStatus
import io.aide.android.model.AppModel
import io.aide.android.model.World
import io.aide.android.ui.AideTheme
import io.aide.android.ui.MachinesTab
import io.aide.android.ui.SessionsTab
import kotlinx.coroutines.flow.MutableStateFlow
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.io.File
import java.util.Locale

/**
 * What one frame of scrolling costs on the JVM (工单 #55): the machines and tasks pages, a finger dragged up a frame at
 * a time, each frame's composition, layout and drawing timed. Not a phone's frame rate — a like-for-like measure of the
 * app's own work per frame, before and after a change; the phone's is scripts/frame-stats.sh. Writes
 * build/outputs/roborazzi/scroll-cost.txt.
 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], application = android.app.Application::class, qualifiers = "zh-rCN-w392dp-h850dp-port-night-xxhdpi")
class ScrollCost {
    @get:Rule val rule = createComposeRule()

    private fun model(): AppModel {
        val server = MockWebServer().apply { dispatcher = object : okhttp3.mockwebserver.Dispatcher() { override fun dispatch(request: okhttp3.mockwebserver.RecordedRequest) = MockResponse().setBody("{}") } }
        server.start()
        val m = AppModel(ApplicationProvider.getApplicationContext())
        AppModel::class.java.getDeclaredField("api").apply { isAccessible = true }.set(m, LocalApi(server.port.toLong(), "t"))
        val w = PageShots.world()
        // A long world: six machines, sixty sessions, so the pages scroll a while.
        val machines = (0 until 3).flatMap { i -> w.machines.map { it.copy(machineId = "${it.machineId}-$i", machineName = "${it.machineName}-$i") } }
        val sessions = (0 until 60).map { i ->
            val s = w.sessions.values.toList()[i % w.sessions.size]
            s.copy(sessionId = "s$i", machineId = machines[i % machines.size].machineId, status = if (i % 5 == 0) SessionStatus.RUNNING else s.status)
        }.associateBy(Session::sessionId)
        @Suppress("UNCHECKED_CAST")
        (AppModel::class.java.getDeclaredField("_world").apply { isAccessible = true }.get(m) as MutableStateFlow<World>).value = w.copy(machines = machines, sessions = sessions)
        return m
    }

    /** Frame times (ms) over a drag of the page from its top. */
    private fun drag(): List<Double> {
        rule.mainClock.autoAdvance = false
        val out = mutableListOf<Double>()
        rule.onRoot().performTouchInput { down(center) }
        repeat(90) {
            rule.onRoot().performTouchInput { moveBy(androidx.compose.ui.geometry.Offset(0f, -60f)) }
            val t = System.nanoTime()
            rule.mainClock.advanceTimeByFrame()
            rule.onRoot().captureToImageOrNull()
            out += (System.nanoTime() - t) / 1e6
        }
        rule.onRoot().performTouchInput { up() }
        return out
    }

    private fun measure(name: String, page: @Composable (AppModel) -> Unit) {
        Locale.setDefault(Locale.SIMPLIFIED_CHINESE)
        val m = model()
        rule.setContent { AideTheme { Surface(Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) { page(m) } } }
        rule.waitForIdle()
        drag() // warm up: the JIT, the fonts
        val f = drag().drop(5).sorted()
        val line = "%s: frames %d, median %.2f ms, p90 %.2f ms, max %.2f ms".format(name, f.size, f[f.size / 2], f[f.size * 9 / 10], f.last())
        File("build/outputs/roborazzi").mkdirs()
        File("build/outputs/roborazzi/scroll-cost.txt").appendText(line + "\n")
        println(line)
    }

    @Test fun machines() = measure("machines") { MachinesTab(it) }
    @Test fun tasks() = measure("tasks") { SessionsTab(it, openDSH = {}) {} }
}

private fun androidx.compose.ui.test.SemanticsNodeInteraction.captureToImageOrNull() = runCatching { captureToImage() }.getOrNull()
