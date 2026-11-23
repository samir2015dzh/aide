package io.aide.android.ui

import android.animation.ValueAnimator
import android.annotation.SuppressLint
import android.content.Context
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.MutableState
import androidx.compose.runtime.State
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Matrix
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.PathFillType
import androidx.compose.ui.graphics.PathOperation
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.withTransform
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.vector.PathParser
import androidx.compose.ui.input.pointer.PointerEventPass
import androidx.compose.ui.input.pointer.PointerEventType
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.ProcessLifecycleOwner
import io.aide.android.api.Session
import io.aide.android.model.BotLook
import io.aide.android.model.BotMood
import io.aide.android.model.botColors
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.MainScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.float
import kotlinx.serialization.json.floatOrNull
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import kotlin.coroutines.resume
import kotlin.math.PI

// A bot's face on Android (工单 #83): the Grok Bot engine ported from BotArk (packages/ui/src/grok), the same file the
// desktop draws with, run in ONE hidden page for every face on screen (a page per face would be far too heavy for a
// list). Each frame the page hands back what to draw (assets/grok/host.js), and Compose paints it here in the bot's
// ink. A list's faces hold their frame while it scrolls (LocalScrolling, 工单 #55); a bot's page and the editor draw
// at 60 frames a second, lists at 30; nothing is drawn while the app is in the background; the system's "remove
// animations" holds every face still, as prefers-reduced-motion does on the desktop.

/** One shape of a frame, in the frame's units, drawn with its matrix at its opacity. */
sealed class GrokOp(val m: Matrix, val o: Float) {
    class Fill(val path: Path, val grey: Color?, m: Matrix, o: Float) : GrokOp(m, o)
    class Line(val path: Path, val width: Float, m: Matrix, o: Float) : GrokOp(m, o)
    class Dot(val x: Float, val y: Float, val r: Float, val width: Float?, val dash: Float?, m: Matrix, o: Float) : GrokOp(m, o)
}

/** A frame: its viewBox's side, and its shapes in order. */
class GrokFrame(val side: Float, val ops: List<GrokOp>)

private fun pathOf(d: String): Path = PathParser().parsePathString(d).toPath()

/** A frame as host.js flattens it. */
fun grokFrame(j: JsonObject): GrokFrame {
    val ops = j["ops"]!!.jsonArray.map { e ->
        val o = e.jsonObject
        val v = o["m"]!!.jsonArray.map { it.jsonPrimitive.float }
        val m = Matrix().apply {
            values[Matrix.ScaleX] = v[0]; values[Matrix.SkewY] = v[1]; values[Matrix.SkewX] = v[2]
            values[Matrix.ScaleY] = v[3]; values[Matrix.TranslateX] = v[4]; values[Matrix.TranslateY] = v[5]
        }
        val alpha = o["o"]?.jsonPrimitive?.floatOrNull ?: 1f
        val d = o["d"]?.jsonPrimitive?.contentOrNull.orEmpty()
        when (o["k"]!!.jsonPrimitive.content) {
            "p" -> {
                var p = pathOf(d)
                if (o["e"]?.jsonPrimitive?.booleanOrNull == true) p.fillType = PathFillType.EvenOdd
                // Its mask's holes (the eyes), cut out of it.
                o["h"]?.jsonPrimitive?.contentOrNull?.takeIf { it.isNotEmpty() }?.let { h -> p = Path().apply { op(p, pathOf(h), PathOperation.Difference) } }
                val grey = o["f"]?.jsonPrimitive?.contentOrNull?.removePrefix("#")?.toLongOrNull(16)?.let { Color(0xFF000000 or it) }
                GrokOp.Fill(p, grey, m, alpha)
            }
            "s" -> GrokOp.Line(pathOf(d), o["w"]!!.jsonPrimitive.float, m, alpha)
            else -> GrokOp.Dot(
                o["x"]!!.jsonPrimitive.float, o["y"]!!.jsonPrimitive.float, o["r"]!!.jsonPrimitive.float,
                o["w"]?.jsonPrimitive?.floatOrNull, o["a"]?.jsonArray?.firstOrNull()?.jsonPrimitive?.floatOrNull, m, alpha,
            )
        }
    }
    return GrokFrame(j["v"]!!.jsonPrimitive.float, ops)
}

/** How much bigger than its box a face's canvas is (the engine's computeCanvasScale(0.92)): it may reach past it. */
private const val CANVAS_SCALE = 0.92f / 0.6f

fun DrawScope.drawGrok(f: GrokFrame, ink: Color) {
    val canvas = size.width * CANVAS_SCALE
    val s = canvas / f.side
    val off = (size.width - canvas) / 2
    withTransform({ translate(off, off); scale(s, s, Offset.Zero) }) {
        for (op in f.ops) withTransform({ transform(op.m) }) {
            when (op) {
                is GrokOp.Fill -> drawPath(op.path, op.grey ?: ink, alpha = op.o)
                is GrokOp.Line -> drawPath(op.path, ink, alpha = op.o, style = Stroke(op.width, cap = StrokeCap.Round, join = StrokeJoin.Round))
                is GrokOp.Dot -> when {
                    op.width == null -> drawCircle(ink, op.r, Offset(op.x, op.y), alpha = op.o)
                    op.dash == null -> drawCircle(ink, op.r, Offset(op.x, op.y), alpha = op.o, style = Stroke(op.width))
                    else -> drawArc(
                        ink, 0f, 360f * op.dash / (2 * PI.toFloat() * op.r), false, Offset(op.x - op.r, op.y - op.r), Size(2 * op.r, 2 * op.r),
                        alpha = op.o, style = Stroke(op.width),
                    )
                }
            }
        }
    }
}

/** What a face is: its shape, seed and state, its size (dp), and how it moves. */
data class GrokOpts(val shape: String, val seed: String, val state: String, val size: Int, val playing: Boolean, val follow: Boolean, val still: Boolean) {
    fun json(): JsonObject = buildJsonObject {
        put("shape", shape); put("seed", seed); put("state", state); put("size", size)
        put("playing", playing); put("follow", follow); put("still", still)
    }
}

/** Where frames come from: the engine's page, or (tests) frames made before by the same engine. */
interface GrokSource {
    /** Its face's first frame at once, when it has one (frames made before); else it comes with a tick. */
    fun set(id: String, o: GrokOpts): JsonObject?
    fun drop(id: String)
    fun hold(id: String, held: Boolean)
    fun gaze(id: String, x: Float?, y: Float?)
    fun poke(id: String, kind: String)
    /** Each face that moved, by dt seconds: its frame. */
    suspend fun tick(dt: Double): Map<String, JsonObject>
}

/** The engine as a page's script: its module's exports as the global G (assets/grok/host.js reads them so). */
fun engineScript(module: String): String {
    val at = module.lastIndexOf("export {")
    val names = module.substring(at + "export {".length, module.indexOf('}', at)).lines()
        .map { it.trim().trimEnd(',') }.filter { it.isNotEmpty() && !it.startsWith("//") }
        .joinToString(",") { val (local, name) = it.split(" as "); "$name:$local" }
    return module.substring(0, at) + "var G={$names};"
}

/** The one hidden page the engine runs in. */
@SuppressLint("SetJavaScriptEnabled")
private class PageSource(context: Context) : GrokSource {
    private val queue = mutableListOf<String>()
    private var loaded = false
    private val page = WebView(context).apply {
        settings.javaScriptEnabled = true
        webViewClient = object : WebViewClient() {
            override fun onPageFinished(view: WebView, url: String?) {
                if (loaded) return
                val read = { name: String -> context.assets.open("grok/$name").bufferedReader().use { it.readText() } }
                view.evaluateJavascript(engineScript(read("grokMotionEngine.js")), null)
                view.evaluateJavascript(read("host.js"), null)
                loaded = true
                queue.forEach { view.evaluateJavascript(it, null) }
                queue.clear()
            }
        }
        loadDataWithBaseURL(null, "<!doctype html><title>faces</title>", "text/html", "utf-8", null)
    }

    private fun run(js: String) {
        if (loaded) page.evaluateJavascript(js, null) else queue += js
    }

    private fun q(s: String) = JsonPrimitive(s).toString()

    override fun set(id: String, o: GrokOpts): JsonObject? = run("aide.set(${q(id)},${o.json()})").let { null }
    override fun drop(id: String) = run("aide.drop(${q(id)})")
    override fun hold(id: String, held: Boolean) = run("aide.hold(${q(id)},$held)")
    override fun gaze(id: String, x: Float?, y: Float?) = run("aide.gaze(${q(id)},${x ?: "null"},${y ?: "null"})")
    override fun poke(id: String, kind: String) = run("aide.poke(${q(id)},${q(kind)})")

    override suspend fun tick(dt: Double): Map<String, JsonObject> {
        if (!loaded || !ProcessLifecycleOwner.get().lifecycle.currentState.isAtLeast(Lifecycle.State.STARTED)) return emptyMap()
        return suspendCancellableCoroutine { c ->
            page.evaluateJavascript("aide.tick($dt)") { r ->
                c.resume(runCatching { Json.parseToJsonElement(r).jsonObject.mapValues { it.value.jsonObject } }.getOrDefault(emptyMap()))
            }
        }
    }
}

/** Every face on screen, and the one loop that moves them. */
object GrokEngine {
    /** Set by tests to frames made before; else the engine's page, made with the first face. */
    var source: GrokSource? = null
    private val frames = mutableMapOf<String, MutableState<GrokFrame?>>()
    private val fast = mutableSetOf<String>()
    private var loop: Job? = null
    private var next = 0

    fun newId(): String = "f${next++}"

    fun frameOf(id: String): State<GrokFrame?> = frames.getOrPut(id) { mutableStateOf(null) }

    fun set(context: Context, id: String, o: GrokOpts) {
        val s = source ?: PageSource(context.applicationContext).also { source = it }
        val state = frames.getOrPut(id) { mutableStateOf(null) }
        s.set(id, o)?.let { state.value = grokFrame(it) }
        if (o.size >= 48) fast += id else fast -= id
        if (loop?.isActive != true) loop = MainScope().launch { run() }
    }

    fun drop(id: String) {
        source?.drop(id)
        frames.remove(id)
        fast -= id
    }

    fun hold(id: String, held: Boolean) = source?.hold(id, held)
    fun gaze(id: String, x: Float?, y: Float?) = source?.gaze(id, x, y)
    fun poke(id: String, kind: String) = source?.poke(id, kind)

    private suspend fun run() {
        var last = System.nanoTime()
        while (frames.isNotEmpty()) {
            val start = System.nanoTime()
            val dt = ((start - last) / 1e9).coerceIn(0.0, 0.1)
            last = start
            val out = source?.tick(dt).orEmpty()
            if (out.isNotEmpty()) {
                val parsed = withContext(Dispatchers.Default) { out.mapValues { runCatching { grokFrame(it.value) }.getOrNull() } }
                parsed.forEach { (id, f) -> if (f != null) frames[id]?.value = f }
            }
            val frameMs = if (fast.isEmpty()) 33L else 16L
            delay((frameMs - (System.nanoTime() - start) / 1_000_000).coerceAtLeast(4))
        }
    }
}

/** The sessions the faces read their bots' states from (Root provides the world's). */
val LocalSessions = compositionLocalOf<Collection<Session>> { emptyList() }

/**
 * A face: the character in its look and state (offline: resting, faded). A press pokes it (a list's row still opens);
 * follow: it looks where a finger goes over it (the editor's stage). poke: a count; each new one spins and bounces it.
 */
@Composable
fun GrokMark(seed: String, look: BotLook, mood: BotMood, size: Int, modifier: Modifier = Modifier, still: Boolean = false, follow: Boolean = false, poke: Int = 0) {
    val context = LocalContext.current
    val id = remember { GrokEngine.newId() }
    val reduce = remember { !ValueAnimator.areAnimatorsEnabled() }
    val scrolling = LocalScrolling.current
    val opts = GrokOpts(look.shape, seed, mood.state, size, playing = !mood.offline, follow = follow, still = still || reduce)
    LaunchedEffect(opts) { GrokEngine.set(context, id, opts) }
    LaunchedEffect(scrolling) { GrokEngine.hold(id, scrolling) }
    LaunchedEffect(poke) {
        if (poke > 0) {
            GrokEngine.poke(id, "spin")
            GrokEngine.poke(id, "bounce")
        }
    }
    DisposableEffect(id) { onDispose { GrokEngine.drop(id) } }
    val frame by GrokEngine.frameOf(id)
    val dark = isSystemInDarkTheme()
    val ink = Color(botColors.first { it.id == look.color }.let { if (dark) it.dark else it.light })
    Canvas(
        modifier.size(size.dp).graphicsLayer { alpha = if (mood.offline) 0.4f else 1f }.pointerInput(id, follow) {
            val gazeAt = { p: Offset ->
                val k = { v: Float -> (v / (this.size.width * 0.5f)).coerceIn(-1f, 1f) }
                GrokEngine.gaze(id, k(p.x - this.size.width / 2f), k(-(p.y - this.size.height / 2f)))
            }
            awaitPointerEventScope {
                while (true) {
                    val e = awaitPointerEvent(PointerEventPass.Initial)
                    val p = e.changes.firstOrNull() ?: continue
                    when (e.type) {
                        PointerEventType.Press -> {
                            GrokEngine.poke(id, "bounce")
                            if (follow) gazeAt(p.position)
                        }
                        PointerEventType.Move -> if (follow) gazeAt(p.position)
                        PointerEventType.Release -> if (follow) GrokEngine.gaze(id, null, null)
                    }
                }
            }
        },
    ) { frame?.let { drawGrok(it, ink) } }
}
