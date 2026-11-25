package io.aide.android.ui

import android.content.Context
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.scale
import androidx.compose.ui.graphics.drawscope.translate
import androidx.compose.ui.graphics.vector.PathParser
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlin.math.floor

// The account's and the devices' faces (工单 #100 / #106 / #109), drawn from packages/ui/src/faces.json, the file the
// desktop draws (copied into the app's assets at build time): a 1.5dp non-scaling white line drawing on its Fluent
// non-status colour, a circle for the person, a rounded tile (floor(size * 0.3)) for a device.

@Serializable
internal data class FaceBook(val stroke: Float, val glyph: Float, val corner: Float, val faces: Map<String, FaceDrawing>)

@Serializable
internal data class FaceDrawing(val color: String, val round: Boolean = false, val paths: List<String>)

internal object Faces {
    private val json = Json { ignoreUnknownKeys = true }
    private var book: FaceBook? = null
    private val paths = HashMap<String, List<Path>>()

    fun book(context: Context): FaceBook = book ?: json
        .decodeFromString(FaceBook.serializer(), context.assets.open("faces/faces.json").bufferedReader().use { it.readText() })
        .also { book = it }

    fun paths(kind: String, f: FaceDrawing): List<Path> = paths.getOrPut(kind) { f.paths.map { PathParser().parsePathString(it).toPath() } }
}

/** A face (kind: "person", or deviceKind's), with its state's dot on the corner when online is given. */
@Composable
fun DeviceFace(kind: String, size: Int, online: Boolean? = null) {
    val book = Faces.book(LocalContext.current)
    val (key, f) = (book.faces[kind]?.let { kind to it }) ?: ("device" to book.faces.getValue("device"))
    val color = Color(android.graphics.Color.parseColor(f.color))
    val glyph = Faces.paths(key, f)
    val dark = isSystemInDarkTheme()
    val dotBorder = aide.card
    val dotColor = if (online == true) aide.green else aide.disabled
    Box(Modifier.size(size.dp).semantics { contentDescription = "face:$key" }) {
        Canvas(Modifier.size(size.dp)) {
            val side = this.size.minDimension
            val corner = if (f.round) side / 2 else floor(size * book.corner).dp.toPx()
            drawRoundRect(color, cornerRadius = CornerRadius(corner, corner))
            if (dark && key == "iphone") {
                val rim = 1.dp.toPx()
                drawRoundRect(
                    Color.White.copy(alpha = 0.08f),
                    topLeft = Offset(rim / 2f, rim / 2f),
                    size = Size(side - rim, side - rim),
                    cornerRadius = CornerRadius((corner - rim / 2f).coerceAtLeast(0f), (corner - rim / 2f).coerceAtLeast(0f)),
                    style = Stroke(width = rim),
                )
            }
            val k = side * book.glyph / 24f
            val inset = side * (1 - book.glyph) / 2
            translate(inset, inset) {
                scale(k, k, pivot = Offset.Zero) {
                    // 1.5dp on the screen, as the desktop's non-scaling stroke: the path is scaled, the line is not.
                    val line = Stroke(width = book.stroke.dp.toPx() / k, cap = StrokeCap.Round, join = StrokeJoin.Round)
                    glyph.forEach { drawPath(it, Color.White, style = line) }
                }
            }
        }
        if (online != null) Box(
            Modifier.align(Alignment.BottomEnd).offset(x = 2.dp, y = 2.dp).size(12.dp)
                .background(dotBorder, CircleShape).padding(2.dp).background(dotColor, CircleShape),
        )
    }
}
