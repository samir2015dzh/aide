package io.aide.android.api

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.withContext
import kotlinx.serialization.SerializationException
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.encodeToJsonElement

/**
 * LocalService (aide.local.v1) of the core in this app's process: the
 * only API the app uses, as on the iPhone.
 */
class LocalApi(port: Long, token: String) {
    val client = ConnectClient("http://127.0.0.1:$port", token, "aide.local.v1.LocalService")

    /** The answer decoded off the caller's (the UI's) thread; one that does not fit Res is an ApiError, as callers expect. */
    suspend inline fun <reified Req, reified Res> call(method: String, req: Req, timeoutSeconds: Long = 30): Res {
        val out = client.unary(method, ConnectClient.json.encodeToJsonElement(req), timeoutSeconds)
        return withContext(Dispatchers.Default) { decode<Res>(method, out) }
    }

    inline fun <reified Req, reified Res> stream(method: String, req: Req): Flow<Res> =
        client.stream(method, ConnectClient.json.encodeToJsonElement(req)).map { decode<Res>(method, it) }

    inline fun <reified Res> decode(method: String, e: JsonElement): Res = try {
        ConnectClient.json.decodeFromJsonElement(e)
    } catch (x: SerializationException) {
        throw ApiError("internal", "unreadable answer to $method: ${x.message}")
    } catch (x: IllegalArgumentException) { // what a JSON element of the wrong kind throws
        throw ApiError("internal", "unreadable answer to $method: ${x.message}")
    }

    /** A call to which only the answer's raw JSON matters (Her's). */
    suspend inline fun <reified Req> raw(method: String, req: Req, timeoutSeconds: Long = 30): JsonElement =
        client.unary(method, ConnectClient.json.encodeToJsonElement(req), timeoutSeconds)

    suspend fun status(): GetStatusResponse = call("GetStatus", Empty())
    fun watch(): Flow<WatchResponse> = stream("Watch", Empty())

    // The account (docs/accounts.md). The server checks a password with
    // Argon2id, which takes a moment: these wait longer than the rest.

    /** Sets a new server up with the setup code it printed, and logs this phone in to it. */
    suspend fun setup(relayUrl: String, setupCode: String, account: String, password: String, label: String): SetupResponse =
        call("Setup", SetupRequest(relayUrl, setupCode, account, password, label), timeoutSeconds = 60)

    /** Logs this phone in to the owner's server: it is signed in at once. */
    suspend fun logIn(relayUrl: String, account: String, password: String, label: String): LogInResponse =
        call("LogIn", LogInRequest(relayUrl, account, password, label), timeoutSeconds = 60)

    suspend fun getLogin(): GetLoginResponse = call("GetLogin", Empty())

    suspend fun setLogin(req: SetLoginRequest): SetLoginResponse = call("SetLogin", req, timeoutSeconds = 60)

    suspend fun listDevices(): ListDevicesResponse = call("ListDevices", Empty())

    /** Signs another device out: it can no longer connect, and its own machine is removed. */
    suspend fun removeDevice(keyId: String, eraseHer: Boolean): RemoveDeviceResponse =
        call("RemoveDevice", RemoveDeviceRequest(keyId, eraseHer), timeoutSeconds = 60)

    /** Forgets the owner on this phone once it was signed out, so that it can log in again. */
    suspend fun leave() {
        call<Empty, JsonElement>("Leave", Empty())
    }

    suspend fun listDSHSessions(): ListDSHSessionsResponse = call("ListDSHSessions", Empty(), timeoutSeconds = 60)

    suspend fun readDSHSession(machineId: String, sessionId: String, beforeSeq: Long = 0): ReadDSHSessionResponse =
        call("ReadDSHSession", ReadDSHSessionRequest(machineId, sessionId, beforeSeq), timeoutSeconds = 60)

    /** Taking over a DSH session (docs/dsh-takeover.md). */
    suspend fun setDSHTakeover(machineId: String, sessionId: String, on: Boolean): SetDSHTakeoverResponse =
        call("SetDSHTakeover", SetDSHTakeoverRequest(machineId, sessionId, on), timeoutSeconds = 30)

    /** DSH has 25 s to take it, on the machine: more here, for the way there. */
    suspend fun sendDSHMessage(machineId: String, sessionId: String, text: String, requestId: String): SendDSHMessageResponse =
        call("SendDSHMessage", SendDSHMessageRequest(machineId, sessionId, text, requestId), timeoutSeconds = 45)

    suspend fun interruptDSHSession(machineId: String, sessionId: String): InterruptDSHSessionResponse =
        call("InterruptDSHSession", InterruptDSHSessionRequest(machineId, sessionId), timeoutSeconds = 30)

    suspend fun listSessions(before: SessionCursor? = null): ListSessionsResponse =
        call("ListSessions", ListSessionsRequest(before = before))

    fun sessionEvents(sessionId: String, afterSeq: Long): Flow<SessionEvent> =
        stream("StreamSessionEvents", StreamSessionEventsRequest(sessionId, afterSeq))

    suspend fun answer(sessionId: String, questionId: String, answers: List<Answer>, decline: Boolean): Session? =
        call<AnswerQuestionRequest, AnswerQuestionResponse>("AnswerQuestion", AnswerQuestionRequest(sessionId, questionId, answers, decline)).session

    /** The owner opened the session's page (docs/session-states.md §7). */
    suspend fun markSeen(sessionId: String): Session? =
        call<MarkSessionSeenRequest, SessionResponse>("MarkSessionSeen", MarkSessionSeenRequest(sessionId, opened = true)).session

    suspend fun interrupt(sessionId: String): Session? =
        call<SessionIdRequest, SessionResponse>("InterruptSession", SessionIdRequest(sessionId)).session

    // Contacts (docs/personas.md), kept on the owner's server for every device.

    suspend fun listPersonas(): List<Persona> = call<Empty, ListPersonasResponse>("ListPersonas", Empty()).personas

    suspend fun savePersona(p: Persona): Persona? = call<SavePersonaRequest, SavePersonaResponse>("SavePersona", SavePersonaRequest(p)).persona

    suspend fun deletePersona(id: String) {
        call<DeletePersonaRequest, JsonElement>("DeletePersona", DeletePersonaRequest(id))
    }

    /** Its session goes on, or one starts on its machine (which may take a while to answer). */
    suspend fun talkToPersona(id: String, text: String, key: String): TalkToPersonaResponse =
        call("TalkToPersona", TalkToPersonaRequest(id, text, key), timeoutSeconds = 60)

    suspend fun checkAgents(machineId: String) {
        call<MachineIdRequest, JsonElement>("CheckAgents", MachineIdRequest(machineId), timeoutSeconds = 60)
    }
}
