package io.hermesh.android.api

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.channels.trySendBlocking
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.Call
import okhttp3.Callback
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import okio.Buffer
import okio.BufferedSource
import java.io.IOException
import java.util.concurrent.TimeUnit
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException

/** An error the core answered with (Connect's code, e.g. "invalid_argument"), or "unavailable" when it could not be reached. */
class ApiError(val code: String, override val message: String) : Exception(message)

/**
 * A Connect client (the Connect protocol, JSON codec) for one service the
 * core serves on 127.0.0.1: unary calls and server streams, as connect-go
 * answers them. The iPhone app speaks the same protocol with protobuf.
 */
class ConnectClient(
    private val baseUrl: String,
    private val token: String,
    private val service: String,
    private val http: OkHttpClient = defaultHttp(),
) {
    companion object {
        val json = Json {
            ignoreUnknownKeys = true
            explicitNulls = false
            encodeDefaults = false
            coerceInputValues = true
        }
        private val JSON = "application/json".toMediaType()
        private val STREAM_JSON = "application/connect+json".toMediaType()

        fun defaultHttp(): OkHttpClient = OkHttpClient.Builder()
            .connectTimeout(5, TimeUnit.SECONDS)
            .readTimeout(0, TimeUnit.SECONDS) // streams and calls that wait for the person
            .retryOnConnectionFailure(true)
            .build()

        /** One Connect envelope: a flags byte, the length (big-endian), the message. */
        fun envelope(flags: Int, payload: ByteArray): ByteArray {
            val b = Buffer()
            b.writeByte(flags)
            b.writeInt(payload.size)
            b.write(payload)
            return b.readByteArray()
        }

        /** The next envelope from source: (flags, payload), or null at its end. */
        fun readEnvelope(source: BufferedSource): Pair<Int, ByteArray>? {
            if (source.exhausted()) return null
            val flags = source.readByte().toInt() and 0xff
            val size = source.readInt()
            if (size < 0) throw IOException("bad envelope length $size")
            return flags to source.readByteArray(size.toLong())
        }

        /** The error an end-of-stream message carries, if any. */
        fun endStreamError(payload: ByteArray): ApiError? {
            if (payload.isEmpty()) return null
            val o = json.parseToJsonElement(payload.decodeToString()).jsonObject
            val e = o["error"] as? JsonObject ?: return null
            return errorOf(e)
        }

        fun errorOf(e: JsonObject): ApiError = ApiError(
            e["code"]?.jsonPrimitive?.contentOrNull ?: "unknown",
            e["message"]?.jsonPrimitive?.contentOrNull ?: "",
        )

        const val END_STREAM = 0x02
    }

    private fun request(method: String, body: ByteArray, streaming: Boolean): Request =
        Request.Builder()
            .url("$baseUrl/$service/$method")
            .header("Authorization", "Bearer $token")
            .header("Connect-Protocol-Version", "1")
            .post(body.toRequestBody(if (streaming) STREAM_JSON else JSON))
            .build()

    /** A unary call; timeoutSeconds 0 waits as long as it takes (the person signing, a long poll). */
    suspend fun unary(method: String, req: JsonElement, timeoutSeconds: Long = 30): JsonElement {
        val call = http.newCall(request(method, req.toString().encodeToByteArray(), streaming = false))
        if (timeoutSeconds > 0) call.timeout().timeout(timeoutSeconds, TimeUnit.SECONDS)
        val res = call.await()
        res.use {
            val text = it.body?.string().orEmpty()
            if (!it.isSuccessful) {
                val e = runCatching { json.parseToJsonElement(text).jsonObject }.getOrNull()
                throw if (e != null) errorOf(e) else ApiError("unknown", "HTTP ${it.code}")
            }
            return json.parseToJsonElement(text.ifEmpty { "{}" })
        }
    }

    /** A server stream: each message until the core ends it; an error ends the flow with ApiError. */
    fun stream(method: String, req: JsonElement): Flow<JsonElement> = callbackFlow {
        val call = http.newCall(request(method, envelope(0, req.toString().encodeToByteArray()), streaming = true))
        val reader = Thread {
            try {
                call.execute().use { res ->
                    if (!res.isSuccessful) {
                        val text = res.body?.string().orEmpty()
                        val e = runCatching { json.parseToJsonElement(text).jsonObject }.getOrNull()
                        throw if (e != null) errorOf(e) else ApiError("unknown", "HTTP ${res.code}")
                    }
                    val source = res.body!!.source()
                    while (true) {
                        val (flags, payload) = readEnvelope(source) ?: break
                        if (flags and END_STREAM != 0) {
                            endStreamError(payload)?.let { throw it }
                            break
                        }
                        trySendBlocking(json.parseToJsonElement(payload.decodeToString()))
                    }
                }
                close()
            } catch (e: ApiError) {
                close(e)
            } catch (e: IOException) {
                close(if (call.isCanceled()) null else ApiError("unavailable", e.message ?: "the core is not reachable"))
            }
        }
        reader.isDaemon = true
        reader.start()
        awaitClose { call.cancel() }
    }.flowOn(Dispatchers.IO)
}

private suspend fun Call.await(): Response = suspendCancellableCoroutine { cont ->
    enqueue(object : Callback {
        override fun onResponse(call: Call, response: Response) = cont.resume(response)
        override fun onFailure(call: Call, e: IOException) {
            if (!cont.isCancelled) cont.resumeWithException(ApiError("unavailable", e.message ?: "the core is not reachable"))
        }
    })
    cont.invokeOnCancellation { cancel() }
}
