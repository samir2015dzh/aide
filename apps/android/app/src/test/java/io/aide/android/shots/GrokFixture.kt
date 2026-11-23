package io.aide.android.shots

import io.aide.android.ui.GrokOpts
import io.aide.android.ui.GrokSource
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonObject
import java.util.zip.GZIPInputStream

/**
 * The faces' frames for the page pictures (工单 #83): Robolectric has no browser engine to run the face engine in, so
 * these are frames the same engine and page script drew ahead (apps/android/scripts/grok-frames.mjs), one per shape
 * and state, painted by the app's own GrokFace.kt as on the phone.
 */
object GrokFixture : GrokSource {
    private val frames: Map<String, JsonObject> by lazy {
        val text = GZIPInputStream(javaClass.getResourceAsStream("/grok-frames.json.gz")!!).bufferedReader().use { it.readText() }
        Json.parseToJsonElement(text).jsonObject.mapValues { it.value.jsonObject }
    }

    override fun set(id: String, o: GrokOpts): JsonObject? = frames["${o.shape}|${o.state}"] ?: frames["${o.shape}|idle"]
    override fun drop(id: String) {}
    override fun hold(id: String, held: Boolean) {}
    override fun gaze(id: String, x: Float?, y: Float?) {}
    override fun poke(id: String, kind: String) {}
    override suspend fun tick(dt: Double): Map<String, JsonObject> = emptyMap()
}
