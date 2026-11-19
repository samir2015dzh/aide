package io.hermesh.android.her

import io.hermesh.android.api.ConnectClient
import io.hermesh.android.api.LocalApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.mapNotNull
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.decodeFromJsonElement
import java.util.Base64
import java.util.UUID

/** An error her answered a request with. */
class HerError(message: String) : Exception(message)

/** What a her connection carries: its opening (a result or an error for each setup request), then her's notifications. */
sealed interface HerEvent {
    data class Opened(val connectionId: String, val results: List<Result<JsonElement>>) : HerEvent
    data class Notification(val method: String, val params: JsonElement?) : HerEvent
}

@Serializable private data class HerRequest(val method: String, val params: String = "")
@Serializable private data class OpenRequest(val setup: List<HerRequest>)
@Serializable private data class HerOpenRequest(val machineId: String, val open: OpenRequest)
@Serializable private data class CallRequest(val connectionId: String = "", val request: HerRequest)
@Serializable private data class HerCallRequest(val machineId: String, val call: CallRequest)
@Serializable private data class HerErrorJson(val code: Int = 0, val message: String = "")
@Serializable private data class HerResponse(val result: String = "", val error: HerErrorJson? = null)
@Serializable private data class Opened(val connectionId: String = "", val responses: List<HerResponse> = emptyList())
@Serializable private data class Notification(val method: String = "", val params: String = "")
@Serializable private data class OpenEvent(val opened: Opened? = null, val notification: Notification? = null)
@Serializable private data class CallResponse(val response: HerResponse? = null)
@Serializable private data class HerSendRequest(val machineId: String, val botId: String, val text: String, val id: String, val connectionId: String = "", val sessionId: String = "")
@Serializable data class HerSendResponse(val delivered: Boolean = false)

/** A machine's Her through the core in this app: each call goes end to end through the tunnel, as on the iPhone. */
class HerClient(private val api: LocalApi, private val machineId: String) {
    private fun encode(params: JsonElement): String = Base64.getEncoder().encodeToString(params.toString().encodeToByteArray())

    private fun decode(b64: String): JsonElement? =
        if (b64.isEmpty()) null else ConnectClient.json.parseToJsonElement(Base64.getDecoder().decode(b64).decodeToString())

    private fun result(r: HerResponse?): Result<JsonElement> = when {
        r == null -> Result.failure(HerError("no answer"))
        r.error != null -> Result.failure(HerError(r.error.message.ifEmpty { "her error ${r.error.code}" }))
        else -> Result.success(decode(r.result) ?: JsonObject(emptyMap()))
    }

    /** Opens a connection, sending setup on it first; the flow ends when the connection does, and cancelling it ends the connection. */
    fun open(setup: List<Pair<String, JsonElement>>): Flow<HerEvent> =
        api.stream<HerOpenRequest, JsonElement>("HerOpen", HerOpenRequest(machineId, OpenRequest(setup.map { HerRequest(it.first, encode(it.second)) })))
            .mapNotNull { raw ->
                val e = ConnectClient.json.decodeFromJsonElement<OpenEvent>(raw)
                when {
                    e.opened != null -> HerEvent.Opened(e.opened.connectionId, e.opened.responses.map(::result))
                    e.notification != null -> HerEvent.Notification(e.notification.method, decode(e.notification.params))
                    else -> null
                }
            }

    /** One request, on connectionId's connection or on one of its own. */
    suspend fun call(method: String, params: JsonElement, connectionId: String = ""): JsonElement {
        val raw = api.raw("HerCall", HerCallRequest(machineId, CallRequest(connectionId, HerRequest(method, encode(params)))), timeoutSeconds = 60)
        return result(ConnectClient.json.decodeFromJsonElement<CallResponse>(raw).response).getOrThrow()
    }

    /** A message to an assistant, kept on this phone until it is delivered (docs/her-sending.md). */
    suspend fun send(botId: String, text: String, connectionId: String, sessionId: String): HerSendResponse =
        api.call("HerSend", HerSendRequest(machineId, botId, text, UUID.randomUUID().toString(), connectionId, sessionId), timeoutSeconds = 60)
}
