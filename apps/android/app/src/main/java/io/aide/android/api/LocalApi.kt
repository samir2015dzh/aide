package io.aide.android.api

import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.encodeToJsonElement

/**
 * LocalService (hermesh.local.v1) of the core in this app's process: the
 * only API the app uses, as on the iPhone. Calls that the person signs wait
 * as long as they take (timeout 0).
 */
class LocalApi(port: Long, token: String) {
    val client = ConnectClient("http://127.0.0.1:$port", token, "hermesh.local.v1.LocalService")

    suspend inline fun <reified Req, reified Res> call(method: String, req: Req, timeoutSeconds: Long = 30): Res {
        val out = client.unary(method, ConnectClient.json.encodeToJsonElement(req), timeoutSeconds)
        return ConnectClient.json.decodeFromJsonElement(out)
    }

    inline fun <reified Req, reified Res> stream(method: String, req: Req): Flow<Res> =
        client.stream(method, ConnectClient.json.encodeToJsonElement(req)).map { ConnectClient.json.decodeFromJsonElement<Res>(it) }

    /** A call to which only the answer's raw JSON matters (Her's). */
    suspend inline fun <reified Req> raw(method: String, req: Req, timeoutSeconds: Long = 30): JsonElement =
        client.unary(method, ConnectClient.json.encodeToJsonElement(req), timeoutSeconds)

    suspend fun status(): GetStatusResponse = call("GetStatus", Empty())
    fun watch(): Flow<WatchResponse> = stream("Watch", Empty())

    suspend fun setup(relayUrl: String, claimCode: String, label: String): Unit =
        call<SetupRequest, JsonElement>("Setup", SetupRequest(relayUrl, claimCode, label), timeoutSeconds = 0).let { }

    suspend fun requestJoin(relayUrl: String, ownerId: String, label: String): RequestJoinResponse =
        call("RequestJoin", RequestJoinRequest(relayUrl, ownerId, label), timeoutSeconds = 0)

    suspend fun passwordLogin(relayUrl: String, account: String, password: String, label: String): PasswordLoginResponse =
        call("PasswordLogin", PasswordLoginRequest(relayUrl, account, password, label), timeoutSeconds = 0)

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

    suspend fun waitJoin(code: String): WaitJoinResponse = call("WaitJoin", CodeRequest(code), timeoutSeconds = 60)

    suspend fun confirmJoin(code: String, accept: Boolean, match: String = "") {
        call<ConfirmJoinRequest, JsonElement>("ConfirmJoin", ConfirmJoinRequest(code, accept, match))
    }

    suspend fun lookupPairing(code: String): PairingDetails? =
        call<CodeRequest, LookupPairingResponse>("LookupPairing", CodeRequest(code)).details

    suspend fun approveMachine(code: String, name: String): ApproveMachinePairingResponse =
        call("ApproveMachinePairing", ApproveMachinePairingRequest(code, name), timeoutSeconds = 0)

    suspend fun approveClient(code: String, spec: GrantSpec) {
        call<ApproveClientPairingRequest, JsonElement>("ApproveClientPairing", ApproveClientPairingRequest(code, spec), timeoutSeconds = 0)
    }

    suspend fun approveDevice(code: String): ApproveDevicePairingResponse =
        call("ApproveDevicePairing", CodeRequest(code), timeoutSeconds = 0)

    suspend fun listSessions(before: SessionCursor? = null): ListSessionsResponse =
        call("ListSessions", ListSessionsRequest(before = before))

    fun sessionEvents(sessionId: String, afterSeq: Long): Flow<SessionEvent> =
        stream("StreamSessionEvents", StreamSessionEventsRequest(sessionId, afterSeq))

    suspend fun answer(sessionId: String, questionId: String, answers: List<Answer>, decline: Boolean): Session? =
        call<AnswerQuestionRequest, AnswerQuestionResponse>("AnswerQuestion", AnswerQuestionRequest(sessionId, questionId, answers, decline)).session

    suspend fun interrupt(sessionId: String): Session? =
        call<SessionIdRequest, SessionResponse>("InterruptSession", SessionIdRequest(sessionId)).session

    suspend fun checkAgents(machineId: String) {
        call<MachineIdRequest, JsonElement>("CheckAgents", MachineIdRequest(machineId), timeoutSeconds = 60)
    }
}
