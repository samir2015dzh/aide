@file:OptIn(ExperimentalSerializationApi::class)

package io.aide.android.api

import kotlinx.serialization.EncodeDefault
import kotlinx.serialization.ExperimentalSerializationApi
import kotlinx.serialization.KSerializer
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.descriptors.PrimitiveKind
import kotlinx.serialization.descriptors.PrimitiveSerialDescriptor
import kotlinx.serialization.encoding.Decoder
import kotlinx.serialization.encoding.Encoder
import kotlinx.serialization.json.JsonDecoder
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.longOrNull

// The aide.local.v1 messages the app reads and sends, as protojson has
// them: lowerCamelCase names, 64-bit integers as strings, enums by name,
// timestamps as RFC 3339, bytes as base64, defaults left out. Only the
// fields the app uses; the rest are ignored. A request's field whose
// default is not proto's (zero) is sent always (@EncodeDefault).

/** A 64-bit integer, a string in protojson (a number is taken too). */
object Int64 : KSerializer<Long> {
    override val descriptor = PrimitiveSerialDescriptor("Int64", PrimitiveKind.STRING)
    override fun serialize(encoder: Encoder, value: Long) = encoder.encodeString(value.toString())
    override fun deserialize(decoder: Decoder): Long {
        if (decoder is JsonDecoder) {
            val p = decoder.decodeJsonElement() as? JsonPrimitive ?: return 0
            return p.longOrNull ?: p.content.toLongOrNull() ?: 0
        }
        return decoder.decodeString().toLongOrNull() ?: 0
    }
}

@Serializable
data class GetStatusResponse(
    val initialized: Boolean = false,
    val ownerId: String = "",
    val relayUrl: String = "",
    val relayConnected: Boolean = false,
    val version: String = "",
    val hosted: Boolean = false,
    val deviceKeyId: String = "",
    val removed: Boolean = false,
    val signedOut: Boolean = false,
    val account: String = "",
    val mcpUrl: String = "",
    val abilities: List<String> = emptyList(),
    /** The limits sessions' states are worked out with (docs/session-states.md). */
    val stateLimits: StateLimits? = null,
)

@Serializable
data class StateLimits(val lostAfterSeconds: Int = 0, val stallAfterSeconds: Int = 0)

/** A time a machine could not be read for at least the lost limit; to is "" while it lasts. */
@Serializable
data class LostSpan(val from: String = "", val to: String = "")

@Serializable
data class AgentStatus(
    val name: String = "",
    val available: Boolean = false,
    /** The owner hid it (SetAgentHidden): left out of the machine's list, as on the desktop. */
    val hidden: Boolean = false,
    val version: String = "",
    val loggedIn: Boolean = false,
    /** loggedIn is false because the check could not tell (工单 #108): unconfirmed, not signed out. */
    val loginUnconfirmed: Boolean = false,
    val detail: String = "",
    val checking: Boolean = false,
    /** The models it offers here, to pick a contact's from. */
    val models: List<AgentModel> = emptyList(),
    /** Its per-session modes (permission presets) here. */
    val modes: List<AgentMode> = emptyList(),
    /** What its sessions run with when none is chosen: the machine's defaults for it, else its own (工单 #99). */
    val defaults: AgentDefaults? = null,
    val own: AgentDefaults? = null,
    /** The owner paused it here (SetAgentPaused): no new session starts with it until it resumes. */
    val pause: AgentPause? = null,
    /** When it last resumed by itself, once its pause came to its end (RFC 3339). */
    val autoResumedAt: String = "",
    /** Claude plan usage read on that machine (Claude Code only). */
    val usage: ClaudeUsage? = null,
    /** Why it is not available: an AgentProblem name ("AGENT_PROBLEM_NOT_FOUND", …); "" when it is. */
    val problem: String = "",
    /** When the check the rest says came from ended (RFC 3339); "" never checked. */
    val checkedAt: String = "",
    /** Claude only: the Claude desktop app, apart from the command line (loggedIn is the command line's own sign-in). */
    val desktop: ClaudeDesktop? = null,
    /** DSH only: the Aide bridge plugin in DSH Desktop. */
    val dshBridge: DSHBridge? = null,
    /** Its sign-in started from Aide (工单 #51); null: none. */
    val login: AgentLogin? = null,
    /** The kind the owner filed it as (SetAgentCategory, 工单 #93): "" told from what it is, "local", "personal". */
    val category: String = "",
    /** Truthful capability declaration for this provider on this machine (工单 #110 / #112). */
    val capabilities: AgentCapabilities? = null,
    /** Origin of this agent: "builtin", "registry", or "local" (工单 #110). */
    val source: String = "",
    val registryId: String = "",
    val registryVersion: String = "",
    val protocolVersion: Int = 0,
)

@Serializable
data class AgentCapabilities(
    val selectModel: Boolean = false,
    val switchModelInSession: Boolean = false,
    val selectEffort: Boolean = false,
    val selectMode: Boolean = false,
    val loginInAide: Boolean = false,
    val usage: Boolean = false,
    val fork: Boolean = false,
    val rollback: Boolean = false,
    val steer: Boolean = false,
    val resume: Boolean = false,
    val images: Boolean = false,
    val audio: Boolean = false,
)
/**
 * aide.machine.v1.AgentLogin; state is an AgentLoginState name. url: the address its sign-in printed, to open on any
 * device; codeSent: the code the page showed went to it (工单 #118).
 */
@Serializable
data class AgentLogin(val state: String = "", val detail: String = "", val startedAt: String = "", val endedAt: String = "", val url: String = "", val codeSent: Boolean = false)

@Serializable data class StartAgentLoginRequest(val machineId: String, val agent: String, val cancel: Boolean = false, val code: String = "")
@Serializable data class StartAgentLoginResponse(val login: AgentLogin? = null)

@Serializable
data class ClaudeDesktop(val installed: Boolean = false, val signedIn: Boolean = false)

/** aide.machine.v1.AgentPause; resumeAt empty: only the owner resumes it. Times are RFC 3339. */
@Serializable
data class AgentPause(val since: String = "", val resumeAt: String = "")

/** fetchedAt (RFC 3339): when it was read; stale: the last reading, kept after a refresh that failed (工单 #86). */
@Serializable
data class ClaudeUsage(val windows: List<UsageWindow> = emptyList(), val fetchedAt: String = "", val stale: Boolean = false, val unavailable: String = "")

@Serializable
data class UsageWindow(val key: String = "", val utilization: Double = 0.0, val resetsAt: String = "")

/**
 * resolved: the specific model an alias id runs on that machine (Claude Code's "opus" → "claude-opus-5-5", 工单 #99);
 * source: where its agent offers it from (DSH's provider lines, 工单 #98 补充).
 */
@Serializable
data class AgentModel(
    val id: String = "",
    val name: String = "",
    val efforts: List<String> = emptyList(),
    val resolved: String = "",
    val source: String = "",
    val contextWindow: Int = 0,
    val maxOutputTokens: Int = 0,
    val capabilities: List<String> = emptyList(),
    val inputCostPerMillion: Double = 0.0,
    val outputCostPerMillion: Double = 0.0,
)

@Serializable
data class ModelCatalogSync(
    val source: String = "",
    val modelCount: Int = 0,
    val syncedAt: String = "",
    val lastAttemptAt: String = "",
    val lastError: String = "",
)

/** aide.identity.v1.AgentDefaults, as far as the phone reads it. */
@Serializable
data class AgentDefaults(val model: String = "", val effort: String = "", val mode: String = "")

@Serializable
data class AgentMode(val id: String = "", val name: String = "")

/** aide.machine.v1.HerStatus: what a machine says of Her, which Aide no longer has. */
@Serializable
data class HerStatus(
    /** Her is on there: said by a machine whose Aide still has her. */
    val enabled: Boolean = false,
    /** Why it can't run Her: "removed" from every machine since Aide dropped her. */
    val unsupported: String = "",
    /** Her was on there once and her data is still there. */
    val data: Boolean = false,
)

@Serializable
data class MachineStatus(
    val name: String = "",
    val platform: String = "",
    val agents: List<AgentStatus> = emptyList(),
    val runningSessions: Int = 0,
    val maxSessions: Int = 0,
    val paused: Boolean = false,
    val version: String = "",
    val her: HerStatus? = null,
    /** The rooms of several bots it hosts (G2-b/c), followed on the Watch. */
    val rooms: List<Room> = emptyList(),
    /** What its coding agents said for rooms an away host keeps, waiting at the post office (工单 #74). */
    val roomOutbox: List<RoomOutbox> = emptyList(),
    /** Open model catalog sync status (工单 #114). */
    val modelCatalog: ModelCatalogSync? = null,
)

@Serializable data class RoomWaiting(val roomId: String = "", val messages: Int = 0, val since: String = "")
@Serializable data class ListRoomWaitingRequest(val machineId: String)
@Serializable data class ListRoomWaitingResponse(val rooms: List<RoomWaiting> = emptyList())
@Serializable data class RoomOutbox(val roomId: String = "", val hostMachineId: String = "", val replies: Int = 0, val since: String = "")

@Serializable
data class Machine(
    val machineId: String = "",
    val machineName: String = "",
    val online: Boolean = false,
    val lastSeen: String = "",
    val status: MachineStatus? = null,
    val local: Boolean = false,
    val deviceKeyId: String = "",
    val version: String = "",
    val unreachable: String = "",
    val syncing: Boolean = false,
    /** When this phone last knew it was there, once it no longer is. */
    val heardAt: String = "",
    val unreachableSince: String = "",
    val lostSpans: List<LostSpan> = emptyList(),
)

@Serializable
data class GrantSpec(
    val label: String = "",
    val machineIds: List<String> = emptyList(),
    val projectIds: List<String> = emptyList(),
    val agents: List<String> = emptyList(),
    val taskVisibility: String = "",
    val ttlDays: Int = 0,
    val maxConcurrentSessions: Int = 0,
)

@Serializable
data class Client(
    val clientId: String = "",
    val grantId: String = "",
    val label: String = "",
    val fingerprint: String = "",
    val spec: GrantSpec? = null,
    val expiresAt: String = "",
    val online: Boolean = false,
    /** Online by its last call, no connection now (an MCP connection, 工单 #94): lastSeen is when. */
    val recent: Boolean = false,
    val lastSeen: String = "",
    val revoked: Boolean = false,
    val custodian: String = "",
    val herMachineId: String = "",
    /** The kind the owner set (工单 #52): "personal", "local", or "" to tell from its name. */
    val category: String = "",
    val categoryMachineId: String = "",
    /** The icon the owner picked: "bot:<shape>:<colour>" its character's look (工单 #83); else its seed's. */
    val icon: String = "",
)

@Serializable
data class Device(
    val keyId: String = "",
    val label: String = "",
    val platform: String = "",
    val thisDevice: Boolean = false,
    val machineId: String = "",
    val machineOnline: Boolean = false,
    /** Set up before accounts: a root key of its own in the root cert. */
    val legacy: Boolean = false,
)

@Serializable
data class Project(
    val machineId: String = "",
    val projectId: String = "",
    val description: String = "",
    val path: String = "",
    /** The coding agents that may work in it; empty: all. */
    val agents: List<String> = emptyList(),
    /** What the owner calls it (工单 #50); empty: its id. */
    val name: String = "",
    /** The DSH permission preset its DSH sessions start with when none is chosen (工单 #89); empty: DSH's own. */
    val dshPreset: String = "",
)

/** A project as the owner knows it: the name they gave it, else its id. */
fun Project.title(): String = name.trim().ifEmpty { projectId }

@Serializable
data class ProjectRef(val machineId: String = "", val projectId: String = "")

@Serializable
data class Failure(val kind: String = "", val message: String = "", val retryable: Boolean = false, val hint: String = "", val provider: String = "")

@Serializable
data class QuestionOption(val label: String = "", val description: String = "")

@Serializable
data class QuestionItem(
    val id: String = "",
    val header: String = "",
    val text: String = "",
    val options: List<QuestionOption> = emptyList(),
    val multiSelect: Boolean = false,
    val allowOther: Boolean = false,
    val secret: Boolean = false,
    val optional: Boolean = false,
)

@Serializable
data class Question(
    val questionId: String = "",
    val items: List<QuestionItem> = emptyList(),
    val askedAt: String = "",
    val expiresAt: String = "",
)

@Serializable
data class Answer(val itemId: String = "", val selected: List<String> = emptyList(), val text: String = "")

@Serializable
data class SessionProgress(
    val phase: String = "",
    val lastMessageExcerpt: String = "",
    val currentTool: String = "",
    val lastFailure: Failure? = null,
    val pendingQuestion: Question? = null,
    val turnStartedAt: String = "",
    /** When its last event was recorded. */
    val lastEventAt: String = "",
    /** Turns finished. */
    val turnCount: Int = 0,
)

/** What the owner has seen of a session, kept by its machine (MarkSessionSeen). */
@Serializable
data class SessionSeen(
    val turns: Int = 0,
    /** The pending question a desktop told the owner of, and whether the owner was at a desktop while it waited. */
    val questionId: String = "",
    val questionPresent: Boolean = false,
)

object SessionStatus {
    const val QUEUED = "SESSION_STATUS_QUEUED"
    const val PREPARING = "SESSION_STATUS_PREPARING"
    const val RUNNING = "SESSION_STATUS_RUNNING"
    const val IDLE = "SESSION_STATUS_IDLE"
    const val ARCHIVED = "SESSION_STATUS_ARCHIVED"
    const val CRASHED = "SESSION_STATUS_CRASHED"
    const val WAITING_INPUT = "SESSION_STATUS_WAITING_INPUT"
    /** Not known now: its machine can't be read (World.shown). */
    const val UNKNOWN = "SESSION_STATUS_UNSPECIFIED"
}

@Serializable
data class Session(
    val sessionId: String = "",
    val machineId: String = "",
    val projectId: String = "",
    val agent: String = "",
    val status: String = "",
    val createdBy: String = "",
    val progress: SessionProgress? = null,
    val createdAt: String = "",
    val updatedAt: String = "",
    val error: String = "",
    val branch: String = "",
    val result: String = "",
    val title: String = "",
    val createdAs: String = "",
    val model: String = "",
    val effort: String = "",
    /** Unset from an older machine, which keeps nothing seen. */
    val seen: SessionSeen? = null,
    /** Archived, its worktree kept: it had changes not in the last turn's snapshot (Session.workspace_kept). */
    val workspaceKept: Boolean = false,
)

@Serializable
data class SessionCursor(val updatedAt: String = "", val sessionId: String = "")

@Serializable
data class WatchResponse(
    val reset: Boolean = false,
    val status: GetStatusResponse? = null,
    val machines: List<Machine> = emptyList(),
    val removedMachineIds: List<String> = emptyList(),
    val clients: List<Client> = emptyList(),
    val removedGrantIds: List<String> = emptyList(),
    val devices: List<Device> = emptyList(),
    val removedDeviceKeyIds: List<String> = emptyList(),
    val projects: List<Project> = emptyList(),
    val removedProjects: List<ProjectRef> = emptyList(),
    val sessions: List<Session> = emptyList(),
    val deletedSessionIds: List<String> = emptyList(),
    val next: SessionCursor? = null,
    /** Moves when this device's counts of work changed: ask QueryWork again (工单 #142). */
    @Serializable(with = Int64::class) val workVersion: Long = 0,
)

// QueryWork (docs/work-stats.md §7): the work over a range by day, grouped by the dimensions asked (工单 #142).

@Serializable
data class QueryWorkRequest(
    val timeZone: String = "",
    /** YYYY-MM-DD in timeZone, and how many days from it. */
    val firstDay: String = "",
    val days: Int = 0,
    /** WORK_BUCKET_HOUR or WORK_BUCKET_DAY. */
    val bucket: String = "",
    /** WORK_DIMENSION_AGENT, WORK_DIMENSION_MACHINE, …; none: everything together. */
    val groupBy: List<String> = emptyList(),
)

/** The dimensions asked for; the others are empty. */
@Serializable
data class WorkGroup(val client: String = "", val agent: String = "", val machineId: String = "", val projectId: String = "")

@Serializable
data class SessionCount(val bucket: Int = 0, val group: WorkGroup? = null, val sessions: Int = 0, val done: Int = 0)

@Serializable
data class TurnCount(
    val bucket: Int = 0,
    val group: WorkGroup? = null,
    val turns: Int = 0,
    @Serializable(with = Int64::class) val inputTokens: Long = 0,
    @Serializable(with = Int64::class) val outputTokens: Long = 0,
)

@Serializable
data class QueryWorkResponse(val sessions: List<SessionCount> = emptyList(), val turns: List<TurnCount> = emptyList())

// Session events (aide.session.v1.SessionEvent): one of the kinds is set.

@Serializable data class StatusChanged(val status: String = "", val reason: String = "")
@Serializable data class TurnStarted(val asker: String = "")
@Serializable data class AttachmentInfo(
    val name: String = "",
    val mediaType: String = "",
    @Serializable(with = Int64::class) val size: Long = 0,
    val path: String = "",
    val sha256: String = "",
    val image: Boolean = false,
)
/** clientMessageIds: the sends it records (SendMessageRequest.clientMessageId), for the outbox (工单 #115). */
@Serializable data class UserMessage(val text: String = "", val attachments: List<AttachmentInfo> = emptyList(), val from: String = "", val fromName: String = "", val clientMessageIds: List<String> = emptyList())
@Serializable data class AgentMessage(val text: String = "")
@Serializable data class AgentDraft(val text: String = "", val whole: Boolean = false)
/** title: the line it shows by when summary is its raw arguments (a session read from DSH's or Claude Desktop's records). */
@Serializable data class ToolCall(val callId: String = "", val kind: String = "", val name: String = "", val summary: String = "", val decision: String = "", val reason: String = "", val title: String = "")
@Serializable data class ToolResult(val callId: String = "", val summary: String = "", val isError: Boolean = false, val truncated: Boolean = false, val preview: Boolean = false)
@Serializable data class TurnCompleted(val result: String = "", val isError: Boolean = false, val commit: String = "", val failure: Failure? = null)
@Serializable data class ErrorEvent(val message: String = "", val failure: Failure? = null)
@Serializable data class QuestionAsked(val question: Question? = null)
@Serializable data class QuestionResolved(val questionId: String = "", val resolution: String = "", val by: String = "", val answers: List<Answer> = emptyList(), val clientMessageIds: List<String> = emptyList())
@Serializable data class MessageReceived(val messageId: String = "", val from: String = "", val fromName: String = "", val text: String = "")
@Serializable data class RolledBack(val turn: Int = 0, val lastTurn: Int = 0, val by: String = "")
@Serializable data class MessageSent(val messageId: String = "", val to: String = "", val toName: String = "", val text: String = "")
/** A managed workspace's request as it stands after one change (docs/workspace-requests.md); from: "agent" or a client id. */
@Serializable data class WorkspaceRequest(
    val requestId: String = "", val kind: String = "", val status: String = "", val from: String = "", val fromName: String = "",
    val item: String = "", val note: String = "",
)

@Serializable
data class SessionEvent(
    @Serializable(with = Int64::class) val seq: Long = 0,
    val ts: String = "",
    val turnId: String = "",
    val status: StatusChanged? = null,
    val turnStarted: TurnStarted? = null,
    val userMessage: UserMessage? = null,
    val agentMessage: AgentMessage? = null,
    val toolCall: ToolCall? = null,
    val toolResult: ToolResult? = null,
    val turnCompleted: TurnCompleted? = null,
    val error: ErrorEvent? = null,
    val questionAsked: QuestionAsked? = null,
    val questionResolved: QuestionResolved? = null,
    val agentDraft: AgentDraft? = null,
    val messageReceived: MessageReceived? = null,
    val messageSent: MessageSent? = null,
    val workspaceRequest: WorkspaceRequest? = null,
    val rolledBack: RolledBack? = null,
)

// Requests and the answers the app reads.

@Serializable data class Empty(val unused: String? = null)
/** Setting a new server up with the setup code it printed, and the account and password it will keep (docs/accounts.md). */
@Serializable data class SetupRequest(
    val relayUrl: String,
    val setupCode: String,
    val account: String,
    val password: String,
    val label: String,
    @EncodeDefault val platform: String = "android",
)
@Serializable data class SetupResponse(val ownerId: String = "")
/** Logging in to the owner's server with the account and password: the server signs this phone in at once. */
@Serializable data class LogInRequest(
    val relayUrl: String,
    val account: String,
    val password: String,
    val label: String,
    @EncodeDefault val platform: String = "android",
)
@Serializable data class LogInResponse(val ownerId: String = "")
/** The account, as the server keeps it; off: logging in is turned off; convert: an old password login waits to be converted. */
@Serializable data class GetLoginResponse(val account: String = "", val server: String = "", val convert: Boolean = false, val off: Boolean = false)
/** A new name or password, or logging in off or on, with the current password; account and newPassword empty: kept. */
@Serializable data class SetLoginRequest(val currentPassword: String, val account: String = "", val newPassword: String = "", val off: Boolean = false)
/** How many devices and agents a new password signed out. */
@Serializable data class SetLoginResponse(val signedOut: Int = 0)
@Serializable data class ListDevicesResponse(val devices: List<Device> = emptyList())
@Serializable data class RemoveDeviceRequest(val keyId: String, val eraseHer: Boolean = false)
@Serializable data class RemoveDeviceResponse(val reissuedMachines: Int = 0, val reissuedGrants: Int = 0)
@Serializable data class SessionIdRequest(val sessionId: String)
@Serializable data class SessionResponse(val session: Session? = null)
@Serializable data class StreamSessionEventsRequest(
    val sessionId: String,
    @Serializable(with = Int64::class) val afterSeq: Long = 0,
    @EncodeDefault val follow: Boolean = true,
    @EncodeDefault val view: String = "EVENT_VIEW_BASIC",
    @EncodeDefault val partial: Boolean = true,
)
@Serializable data class AnswerQuestionRequest(val sessionId: String, val questionId: String, val answers: List<Answer> = emptyList(), val decline: Boolean = false)
@Serializable data class MarkSessionSeenRequest(val sessionId: String, val opened: Boolean = false, val questionId: String = "", val present: Boolean = false)
@Serializable data class AnswerQuestionResponse(val session: Session? = null)
@Serializable data class MachineIdRequest(val machineId: String)

/** resumeAt (RFC 3339), with paused: when it resumes by itself; null: when the owner resumes it. */
@Serializable data class SetAgentHiddenRequest(val machineId: String, val agent: String, val hidden: Boolean)
@Serializable data class SetAgentCategoryRequest(val machineId: String, val agent: String, val category: String)

@Serializable data class SetAgentPausedRequest(val machineId: String, val agent: String, val paused: Boolean, val resumeAt: String? = null)

/** An agent's grant to revoke (工单 #82: deleting it from the roster). */
@Serializable data class RevokeClientRequest(val grantId: String)

/** DeepSeek Harness's own sessions on the owner's machines (docs: aide.machine.v1.DSHSession); takeover: the owner took it over in Aide. */
@Serializable data class DSHSession(
    val sessionId: String = "",
    val title: String = "",
    val cwd: String = "",
    val state: String = "",
    val createdAt: String = "",
    val lastActivityAt: String = "",
    val turnOpen: Boolean = false,
    val archived: Boolean = false,
    val agentPreset: String = "",
    val error: String = "",
    val takeover: Boolean = false,
)
/** The Aide bridge plugin in DSH Desktop (aide.machine.v1.DSHBridge). */
@Serializable data class DSHBridge(
    val state: String = "",
    val version: String = "",
    val detail: String = "",
    /** The package Aide prepared, when there is one of the version it ships (工单 #46). */
    val packagePath: String = "",
    /** The version this machine's Aide ships: a running plugin of an older one is out of date. */
    val latestVersion: String = "",
    /** The last install handed to DSH (工单 #59). */
    val install: DSHBridgeInstall? = null,
)

/** How the install handed to DSH went (工单 #59): DSH_BRIDGE_INSTALL_STATE_RUNNING, _DONE, _RESTART, _FAILED. */
@Serializable data class DSHBridgeInstall(val state: String = "", val sessionId: String = "", val reason: String = "", val startedAt: String = "", val reportedAt: String = "")
@Serializable data class InstallDSHBridgeResponse(val sessionId: String = "", val packagePath: String = "")
/** dshHome: the DSH home that machine reads DSH's sessions from. */
@Serializable data class DSHBridgeEntry(val machineId: String = "", val bridge: DSHBridge = DSHBridge(), val dshHome: String = "")
@Serializable data class SetDSHTakeoverRequest(val machineId: String, val sessionId: String, val on: Boolean)
@Serializable data class SetDSHTakeoverResponse(val session: DSHSession = DSHSession())
@Serializable data class SendDSHMessageRequest(val machineId: String, val sessionId: String, val text: String, val requestId: String)
@Serializable data class SendDSHMessageResponse(val requestId: String = "")
@Serializable data class InterruptDSHSessionRequest(val machineId: String, val sessionId: String)
@Serializable data class InterruptDSHSessionResponse(val interrupted: Boolean = false)
/** lost: its machine can't be reached, and the session is as last read (readAt): its state last known, nothing can be sent. */
@Serializable data class DSHSessionEntry(val machineId: String = "", val machineName: String = "", val session: DSHSession = DSHSession(), val lost: Boolean = false, val readAt: String = "")
@Serializable data class DSHUnreachable(val machineId: String = "", val machineName: String = "")
@Serializable data class ListDSHSessionsResponse(val sessions: List<DSHSessionEntry> = emptyList(), val unreachable: List<DSHUnreachable> = emptyList(), val bridges: List<DSHBridgeEntry> = emptyList())
@Serializable data class ReadDSHSessionRequest(
    val machineId: String,
    val sessionId: String,
    @Serializable(with = Int64::class) val beforeSeq: Long = 0,
    @EncodeDefault val limit: Int = 200,
    @EncodeDefault val view: String = "EVENT_VIEW_FULL",
)
@Serializable data class ReadDSHSessionResponse(val session: DSHSession = DSHSession(), val events: List<SessionEvent> = emptyList(), val hasMoreBefore: Boolean = false)
/** A Claude Code session the Claude desktop app has on a machine, not started by Aide (aide.machine.v1.ClaudeSession). */
@Serializable data class ClaudeSession(
    val sessionId: String = "",
    val title: String = "",
    val cwd: String = "",
    val repo: String = "",
    val createdAt: String = "",
    val lastActivityAt: String = "",
    val archived: Boolean = false,
    val model: String = "",
    val status: String = "",
    val error: String = "",
)
@Serializable data class ClaudeSessionEntry(val machineId: String = "", val machineName: String = "", val session: ClaudeSession = ClaudeSession(), val lost: Boolean = false, val readAt: String = "")
@Serializable data class ClaudeIndexEntry(val machineId: String = "", val indexes: List<String> = emptyList())
@Serializable data class ListClaudeSessionsResponse(val sessions: List<ClaudeSessionEntry> = emptyList(), val unreachable: List<DSHUnreachable> = emptyList(), val indexes: List<ClaudeIndexEntry> = emptyList())
@Serializable data class ReadClaudeSessionRequest(
    val machineId: String,
    val sessionId: String,
    @Serializable(with = Int64::class) val beforeSeq: Long = 0,
    @EncodeDefault val limit: Int = 200,
    @EncodeDefault val view: String = "EVENT_VIEW_FULL",
)
@Serializable data class ReadClaudeSessionResponse(val session: ClaudeSession = ClaudeSession(), val events: List<SessionEvent> = emptyList(), val hasMoreBefore: Boolean = false)
@Serializable data class ListSessionsRequest(@EncodeDefault val limit: Int = 50, val before: SessionCursor? = null, @EncodeDefault val view: String = "SESSION_VIEW_BASIC")
@Serializable data class ListSessionsResponse(val sessions: List<Session> = emptyList(), val next: SessionCursor? = null)


@Serializable data class DiffStat(val filesChanged: Int = 0, val insertions: Int = 0, val deletions: Int = 0)

/** aide.session.v1.Notice: what a machine sealed to this phone's notice key. */
@Serializable
data class Notice(
    val kind: String = "",
    val sessionId: String = "",
    val title: String = "",
    val grant: String = "",
    val agent: String = "",
    val project: String = "",
    val machine: String = "",
    val question: Question? = null,
    val failure: String = "",
    val failureMessage: String = "",
    val turn: Int = 0,
    val diffStat: DiffStat? = null,
    val roomId: String = "",
    val roomLine: String = "",
)

/** aide.session.v1.Attachment: a file sent with a message (工单 #91); data is its bytes, base64 as JSON carries bytes. */
@Serializable data class AttachmentIn(val name: String, val mediaType: String = "", val data: String)
@Serializable data class StartSessionBody(val idempotencyKey: String, val projectId: String, val agent: String, val prompt: String, val model: String = "", val effort: String = "", val mode: String = "", val attachments: List<AttachmentIn> = emptyList())
@Serializable data class SendMessageRequest(
    val sessionId: String,
    val text: String,
    val attachments: List<AttachmentIn> = emptyList(),
    val clientMessageId: String = "",
    @Serializable(with = Int64::class) val clientSeq: Long = 0,
)
@Serializable data class StartSessionRequest(val machineId: String, val session: StartSessionBody)

// Contacts (docs/personas.md): aide.identity.v1.Persona and LocalService's calls for them.

@Serializable
data class Persona(
    val personaId: String = "",
    val name: String = "",
    val icon: String = "",
    val color: String = "",
    val machineId: String = "",
    val projectId: String = "",
    val agent: String = "",
    val model: String = "",
    val effort: String = "",
    val mode: String = "",
    /** The session it talks in now; "" before its first message or once its presets changed. */
    val sessionId: String = "",
)

@Serializable data class ListPersonasResponse(val personas: List<Persona> = emptyList())
@Serializable data class SavePersonaRequest(val persona: Persona)
@Serializable data class SavePersonaResponse(val persona: Persona? = null)
@Serializable data class DeletePersonaRequest(val personaId: String)
@Serializable data class TalkToPersonaRequest(val personaId: String, val text: String, val idempotencyKey: String = "")

@Serializable
data class TalkToPersonaResponse(
    val session: Session? = null,
    /** A new session was started; else the message went to the one it talks in. */
    val started: Boolean = false,
    /** Why it was started: first, archived, gone, changed. */
    val reason: String = "",
    val persona: Persona? = null,
)

// Rooms (ROOMS-DESIGN-2026-11-22, G2-a): the owner's conversation with a personal agent, kept on a host machine.
@Serializable data class RoomMember(val botKey: String = "", val address: String = "", val name: String = "", val agent: String = "", val projectId: String = "", val sessionId: String = "", val machineId: String = "")
// G2-b/c: rooms of several bots, run by hand (only those the owner names are woken) or as a round table.
@Serializable data class Room(
    val roomId: String = "", val name: String = "", val hostMachineId: String = "", val members: List<RoomMember> = emptyList(), val direct: Boolean = false,
    val mode: String = "", val maxRounds: Int = 0, val round: Int = 0, val run: String = "", val unread: Int = 0, val lastLine: String = "", val seenMessageId: String = "",
    val updatedAt: String = "",
    /** What the host last told the owner of it (工单 #69, #70), and when. */
    val notice: String = "", val noticeAt: String = "",
    /** Read-only shadow conversation aggregating 1:1 agent mailbox traffic (工单 #124). */
    val shadow: Boolean = false,
)
@Serializable data class RoomDelivery(val botKey: String = "", val state: String = "", val reason: String = "", val copyId: String = "", val reminded: Boolean = false)
@Serializable data class RoomMessage(
    val messageId: String = "", val roomId: String = "", val from: String = "", val fromName: String = "", val text: String = "", val at: String = "",
    val deliveries: List<RoomDelivery> = emptyList(), val inReplyTo: String = "", val mentions: List<String> = emptyList(), val round: Int = 0,
    @SerialName("final") val isFinal: Boolean = false, val finalSha256: String = "", val late: Boolean = false, val asks: List<String> = emptyList(), val system: Boolean = false,
    val toName: String = "", val subject: String = "", val lightChat: Boolean = false,
)
@Serializable data class OpenDirectRoomRequest(val clientId: String)
@Serializable data class OpenDirectRoomResponse(val room: Room? = null)
@Serializable data class ListRoomMessagesRequest(val machineId: String, val roomId: String, val limit: Int = 200)
@Serializable data class ListRoomMessagesResponse(val room: Room? = null, val messages: List<RoomMessage> = emptyList())
@Serializable data class PostRoomMessageRequest(val machineId: String, val roomId: String, val text: String, val idempotencyKey: String, val mentions: List<String> = emptyList(), val lightChat: Boolean = false)
@Serializable data class CreateRoomMember(val botKey: String, val projectId: String = "", val name: String = "")
@Serializable data class CreateRoomRequest(val name: String, val members: List<CreateRoomMember>, val mode: String, val maxRounds: Int)
@Serializable data class RoomPatch(val roomId: String, val name: String? = null, val mode: String? = null, val maxRounds: Int? = null)
@Serializable data class UpdateRoomRequest(val machineId: String, val room: RoomPatch)
@Serializable data class RoomRef(val machineId: String, val roomId: String)
