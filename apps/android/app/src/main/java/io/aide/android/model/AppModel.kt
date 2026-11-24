package io.aide.android.model

import android.content.Context
import android.net.ConnectivityManager
import android.net.LinkProperties
import android.net.Network
import android.os.Build
import androidx.lifecycle.DefaultLifecycleObserver
import androidx.lifecycle.LifecycleOwner
import androidx.lifecycle.ProcessLifecycleOwner
import io.aide.android.api.ApiError
import io.aide.android.api.Answer
import io.aide.android.api.Device
import io.aide.android.api.GetLoginResponse
import io.aide.android.api.LocalApi
import io.aide.android.api.Persona
import io.aide.android.api.Session
import io.aide.android.api.SetLoginRequest
import io.aide.android.connection.ConnectionService
import io.aide.android.connection.Keepalive
import io.aide.android.connection.Link
import io.aide.android.connection.link
import io.aide.android.aidecore.Aidecore
import io.aide.android.push.LiveNotices
import io.aide.android.push.NoticeTarget
import io.aide.android.push.Push
import io.aide.android.push.roomNotices
import io.aide.android.push.show
import io.aide.android.ui.t
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.File

/**
 * The state the screens read, and every action: the owner role in this
 * process (the shared Go core), followed through one Watch while the app
 * is in front, or while the connection service keeps it in the background
 * (connection/ConnectionService.kt, on unless the person turned it off).
 * Otherwise in the background the core pauses (no relay), as on the
 * iPhone, and the app syncs when it comes back to the front. What the
 * Watch brings that needs the person is notified (push/LiveNotices.kt).
 */
class AppModel(private val context: Context) {
    enum class Phase { Starting, Failed, Onboarding, Ready }

    val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)

    private val _phase = MutableStateFlow(Phase.Starting)
    val phase = _phase.asStateFlow()
    private val _world = MutableStateFlow(World())
    val world = _world.asStateFlow()
    private val _foreground = MutableStateFlow(false)
    val foreground = _foreground.asStateFlow()
    private val _messages = MutableSharedFlow<String>(extraBufferCapacity = 8)
    val messages = _messages.asSharedFlow()
    var startError: String? = null
        private set

    var api: LocalApi? = null
        private set
    private var watching: Job? = null

    val keepalive = Keepalive(context)
    /** The connection service holds the core up in the background. */
    private val _kept = MutableStateFlow(false)
    /** The core follows the relay and the Watch runs: in front, or kept. */
    private val _active = MutableStateFlow(false)
    private val _online = MutableStateFlow(false)
    /** The relay connection as the app shows it. */
    val link = combine(_active, _online, _world) { a, on, w -> link(a, on, w.status.relayConnected) }
        .stateIn(scope, SharingStarted.Eagerly, Link.Disconnected)
    private val live = LiveNotices()
    // Resume and Pause in the order asked: the app's front and the service both ask.
    private val coreCalls = Dispatchers.IO.limitedParallelism(1)

    fun start() {
        scope.launch {
            try {
                val dir = File(context.filesDir, "aide").apply { mkdirs() }
                val info = withContext(Dispatchers.IO) { Aidecore.start(dir.absolutePath) }
                api = LocalApi(info.port, info.token)
            } catch (e: Exception) {
                startError = e.message
                _phase.value = Phase.Failed
                return@launch
            }
            refreshStatus()
            ProcessLifecycleOwner.get().lifecycle.addObserver(object : DefaultLifecycleObserver {
                override fun onStart(owner: LifecycleOwner) = foreground()
                override fun onStop(owner: LifecycleOwner) = background()
            })
            watchNetwork()
            follow() // the connection service may have started first (the system restarted it)
            scope.launch { keepConnected() }
        }
    }

    private fun foreground() {
        _foreground.value = true
        follow()
    }

    private fun background() {
        _foreground.value = false
        follow()
    }

    /** The connection service started (true) or ended (false). */
    fun keep(on: Boolean) {
        _kept.value = on
        follow()
    }

    /** The core runs and the Watch follows it while the app is in front or kept; else both pause. */
    private fun follow() {
        if (api == null) return // not started: start() follows once it is
        val active = _foreground.value || _kept.value
        if (active) {
            scope.launch(coreCalls) { Aidecore.resume() }
            if (watching?.isActive != true) watching = scope.launch { watch() }
        } else if (_active.value) {
            watching?.cancel()
            watching = null
            scope.launch(coreCalls) { Aidecore.pause() }
        }
        _active.value = active
    }

    /**
     * The connection service runs while the owner role is set up and the
     * person keeps it on; it is started from the front (Android 12+ allows
     * it then), and after that the system keeps it or restarts it.
     */
    private suspend fun keepConnected() {
        combine(phase, keepalive.enabled, foreground) { p, on, fg -> Triple(p, on, fg) }.collect { (p, on, fg) ->
            when {
                p == Phase.Ready && on && fg -> ConnectionService.start(context)
                !on || p == Phase.Onboarding || p == Phase.Failed -> ConnectionService.stop(context)
            }
        }
    }

    /** The world while in front or kept: the first message, then each change; again after an error. */
    private suspend fun watch() = coroutineScope {
        val api = api ?: return@coroutineScope
        // Notices that fall due with nothing changing (a question held back for a desktop: LiveNotices).
        launch {
            while (true) {
                delay(10_000)
                if (_world.value.watched) notify(live.look(_world.value))
            }
        }
        while (true) {
            _active.first { it }
            try {
                api.watch().collect { r ->
                    val roomsBefore = _world.value.rooms
                    val wasWatched = _world.value.watched
                    _world.value = _world.value.apply(r)
                    // A room with more the owner has not read tells of it (G2-b/c), as the machines' status brings it;
                    // not what was unread before the first look.
                    if (wasWatched) roomNotices(roomsBefore, _world.value.rooms, roomOpen).forEach { Push.notify(context, show(it)) }
                    if (r.status != null) phaseFor(r.status.initialized, r.status.removed)
                    if (_world.value.watched) notify(live.look(_world.value))
                }
            } catch (e: ApiError) {
                // The core ended the watch (a profile changed, a watcher behind): watch again.
            }
            delay(500)
        }
    }

    suspend fun refreshStatus() {
        val api = api ?: return
        try {
            val st = api.status()
            _world.value = _world.value.copy(status = st)
            phaseFor(st.initialized, st.removed)
        } catch (e: ApiError) {
            if (_phase.value == Phase.Starting) {
                delay(300)
                refreshStatus()
            }
        }
    }

    private fun phaseFor(initialized: Boolean, removed: Boolean) {
        _phase.value = if (initialized && !removed) Phase.Ready else Phase.Onboarding
    }

    /** Notices from the Watch, shown as notifications. */
    private fun notify(c: LiveNotices.Changes) {
        c.withdraw.forEach { Push.withdraw(context, it) }
        c.show.forEach { Push.notify(context, show(it)) }
    }

    /** Wi-Fi to cellular and back: the core connects again at once. */
    private fun watchNetwork() {
        val cm = context.getSystemService(ConnectivityManager::class.java) ?: return
        var current: Network? = null
        _online.value = cm.activeNetwork != null
        cm.registerDefaultNetworkCallback(object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: Network) {
                if (current != null && current != network) Aidecore.networkChanged()
                current = network
                _online.value = true
            }

            override fun onLost(network: Network) {
                if (network == current) {
                    current = null
                    _online.value = false
                }
            }

            override fun onLinkPropertiesChanged(network: Network, lp: LinkProperties) {
                if (network == current) Aidecore.pathChanged()
            }
        })
    }

    private val _opening = MutableStateFlow<NoticeTarget?>(null)
    /** What a notification tapped opens, until the screens took it (opened). */
    val opening = _opening.asStateFlow()

    fun open(target: NoticeTarget) {
        _opening.value = target
    }

    fun opened() {
        _opening.value = null
    }

    /**
     * Where what is said goes (工单 #61, the owner's rule: "a message shows on the screen whose action made it"): the
     * page open on top has its own place to say it (PageFeedback), registered while it shows; with none, the tabs'.
     */
    private val sinks = mutableListOf<(String) -> Unit>()

    fun pushSink(f: (String) -> Unit) {
        sinks += f
    }

    fun popSink(f: (String) -> Unit) {
        sinks.remove(f)
    }

    fun say(text: String) {
        sinks.lastOrNull()?.invoke(text) ?: _messages.tryEmit(text)
    }

    private inline fun <T> report(block: () -> T): T? = try {
        block()
    } catch (e: Exception) {
        say(humanError(e))
        null
    }

    // Logging in and setting up (docs/accounts.md): the server signs this
    // phone in at once; nothing is approved elsewhere, compared or signed here.

    val deviceLabel: String get() = listOf(Build.MANUFACTURER.replaceFirstChar { it.uppercase() }, Build.MODEL).distinct().joinToString(" ")

    /** What this device logs in as, for its face on the owner's devices (工单 #100): a tablet says so. */
    val devicePlatform: String get() = devicePlatform(context.resources.configuration.smallestScreenWidthDp)

    /** Logs in to the owner's server with the account and password. Returns what went wrong, or null. */
    suspend fun logIn(server: String, account: String, password: String): String? = onboard {
        it.logIn(server, account.trim(), password, deviceLabel, devicePlatform)
    }

    /** Sets a new server up with the setup code it printed, keeping this account and password, and logs in to it. */
    suspend fun setUp(server: String, setupCode: String, account: String, password: String): String? = onboard {
        it.setup(server, setupCode.trim(), account.trim(), password, deviceLabel, devicePlatform)
    }

    private suspend fun onboard(call: suspend (LocalApi) -> Unit): String? {
        val api = api ?: return t("Aide’s core is not running.", "Aide 的核心没有运行。")
        return try {
            // Signed out by another device: forget that owner first, so that this phone can log in again.
            if (_world.value.status.removed) api.leave()
            call(api)
            refreshStatus()
            null
        } catch (e: ApiError) {
            loginError(e)
        }
    }

    // The account and the devices (Settings).

    /** The account as the server keeps it; null when it can't be read now (said). */
    suspend fun login(): GetLoginResponse? = report { api!!.getLogin() }

    /**
     * Changes the account's name or password, or turns logging in off or
     * on, with the current password. Returns how many devices and agents a
     * new password signed out, or null when it failed (failed says why).
     */
    suspend fun setLogin(req: SetLoginRequest, failed: (String) -> Unit): Int? {
        val api = api ?: return null
        return try {
            api.setLogin(req).signedOut.also { refreshStatus() }
        } catch (e: ApiError) {
            failed(loginError(e))
            null
        }
    }

    /** Signs another device out; its own machine goes with it, Her's data there erased first if asked. */
    suspend fun removeDevice(d: Device, eraseHer: Boolean): Boolean = report {
        api!!.removeDevice(d.keyId, eraseHer)
        val w = _world.value
        _world.value = w.copy(devices = w.devices.filter { it.keyId != d.keyId })
        true
    } ?: false

    /** The devices again, from the server (the Watch brings their changes too). */
    suspend fun refreshDevices() {
        val api = api ?: return
        runCatching { api.listDevices() }.getOrNull()?.let { _world.value = _world.value.copy(devices = it.devices) }
    }

    // Sessions.

    suspend fun answer(s: Session, answers: List<Answer>, decline: Boolean): Boolean {
        val q = s.progress?.pendingQuestion ?: return false
        return report {
            api!!.answer(s.sessionId, q.questionId, answers, decline)?.let { replace(it) }
            say(if (decline) "Told it to decide by itself" else "Answered; ${agentTitle(s.agent)} is working again")
            true
        } ?: false
    }

    /** The owner opened a session's page: every device hears of it through its machine (docs/session-states.md §7). */
    suspend fun markSeen(s: Session) {
        val api = api ?: return
        runCatching { api.markSeen(s.sessionId) }.getOrNull()?.let { replace(it) }
    }

    suspend fun interrupt(s: Session) {
        report { api!!.interrupt(s.sessionId)?.let { replace(it) } }
    }

    /**
     * Signs Claude Code in on that machine (工单 #51); with code, gives the sign-in under way the code its address's page
     * showed (工单 #118: the address is opened on any device, this phone too).
     */
    suspend fun startAgentLogin(machineId: String, agent: String, cancel: Boolean = false, code: String = "") {
        report { api!!.startAgentLogin(machineId, agent, cancel, code) }
    }

    suspend fun recheck(machineId: String) {
        report { api!!.checkAgents(machineId) }
    }

    /** What waits at the post office for an away host, by room id; empty when it can't be read now. */
    suspend fun roomWaiting(machineId: String): Map<String, Int> = runCatching { api!!.listRoomWaiting(machineId).rooms.associate { it.roomId to it.messages } }.getOrDefault(emptyMap())

    /** The conversation chosen for each bot on this phone (工单 #67 rule 1): a session id, or "new". */
    private val conversations = context.getSharedPreferences("conversations", Context.MODE_PRIVATE)

    fun chosenConversation(botKey: String): String? = conversations.getString(botKey, null)

    fun chooseConversation(botKey: String, v: String) {
        conversations.edit().putString(botKey, v).apply()
    }

    /** The direct room with a personal agent; null when it can't be had, why given to failed, for its own page to say (工单 #61). */
    suspend fun openDirectRoomOr(clientId: String, failed: (String) -> Unit): io.aide.android.api.Room? = try {
        api!!.openDirectRoom(clientId).room
    } catch (e: Exception) {
        failed(humanError(e))
        null
    }

    /** Says something in a room; null when it failed, why given to failed (said in the box it was written in, 工单 #61). */
    suspend fun postRoomMessageOr(room: io.aide.android.api.Room, text: String, key: String, failed: (String) -> Unit, mentions: List<String> = emptyList()): io.aide.android.api.RoomMessage? = try {
        api!!.postRoomMessage(room.hostMachineId, room.roomId, text, key, mentions)
    } catch (e: Exception) {
        failed(humanError(e))
        null
    }

    /** The direct room with a personal agent (ROOMS-DESIGN, G2-a); null, said, when it can't be had. */
    suspend fun openDirectRoom(clientId: String): io.aide.android.api.Room? = report { api!!.openDirectRoom(clientId).room }
    /** A room's messages, quietly: null when they can't be read now. */
    suspend fun roomMessages(room: io.aide.android.api.Room): List<io.aide.android.api.RoomMessage>? = runCatching { api!!.listRoomMessages(room.hostMachineId, room.roomId).messages }.getOrNull()
    suspend fun postRoomMessage(room: io.aide.android.api.Room, text: String, key: String, mentions: List<String> = emptyList()): io.aide.android.api.RoomMessage? = report { api!!.postRoomMessage(room.hostMachineId, room.roomId, text, key, mentions) }

    /** The room whose page is open: what comes in there is read, not told of. */
    var roomOpen: String = ""
    /** A room as its host has it now, with its messages; null when it can't be read now. */
    suspend fun readRoom(machineId: String, roomId: String): io.aide.android.api.ListRoomMessagesResponse? = runCatching { api!!.listRoomMessages(machineId, roomId) }.getOrNull()
    suspend fun createRoom(req: io.aide.android.api.CreateRoomRequest): io.aide.android.api.Room? = report { api!!.createRoom(req) }
    suspend fun updateRoom(machineId: String, patch: io.aide.android.api.RoomPatch): io.aide.android.api.Room? = report { api!!.updateRoom(machineId, patch) }
    suspend fun stopRoom(machineId: String, roomId: String): io.aide.android.api.Room? = report { api!!.stopRoom(machineId, roomId) }
    suspend fun markRoomSeen(machineId: String, roomId: String) {
        runCatching { api!!.markRoomSeen(machineId, roomId) }
    }

    /** Hands installing the DSH bridge plugin to DSH there (工单 #59); the bridge's row follows by itself. */
    suspend fun installDSHBridge(machineId: String): Boolean = report { api!!.installDSHBridge(machineId) } != null

    /** Asks each machine whose findings are old to check its agents again, quietly: nothing is said if one can't. */
    suspend fun checkStaleAgents(now: java.time.Instant) {
        val api = api ?: return
        coroutineScope { world.value.machines.filter { needsCheck(it, now) }.forEach { m -> launch { runCatching { api.checkAgents(m.machineId) } } } }
    }

    /** Files a coding agent of a machine as a kind (工单 #93): "" told from what it is, "local", "personal". */
    suspend fun setAgentCategory(machineId: String, agent: String, category: String): Boolean =
        report { api!!.setAgentCategory(machineId, agent, category) } != null

    /** Hides a coding agent a machine lists (one the owner doesn't trust, as on the desktop), or shows it again. */
    suspend fun setAgentHidden(machineId: String, agent: String, hidden: Boolean) {
        report { api!!.setAgentHidden(machineId, agent, hidden) }
    }

    /**
     * Deletes a bot from the roster (工单 #82), as the desktop does: a coding agent its machine found is hidden there (the
     * devices tab shows it again); an agent with a grant of its own has it revoked, and the server forgets it. True once done.
     */
    suspend fun removeBot(b: Bot): Boolean = report {
        if (b.key.startsWith("m:")) api!!.setAgentHidden(b.machineId, b.agent, true) else api!!.revokeClient(b.client!!.grantId)
        say(if (b.key.startsWith("m:")) t("Removed ${b.name} from the roster", "已把 ${b.name} 移出名册") else t("Deleted ${b.name}", "已删除 ${b.name}"))
    } != null

    /** Pauses a coding agent on a machine, or resumes it; resumeAt (RFC 3339), with paused: when it resumes by itself. */
    suspend fun setAgentPaused(machineId: String, agent: String, paused: Boolean, resumeAt: String? = null) {
        report { api!!.setAgentPaused(machineId, agent, paused, resumeAt) }
    }

    /**
     * Pulled down: the status and the latest page of sessions read again now, merged into what the Watch
     * holds; the Watch keeps them current after.
     */
    suspend fun refresh() {
        refreshStatus()
        report {
            val page = api!!.listSessions()
            val w = _world.value
            _world.value = w.copy(sessions = mergeSessions(w.sessions, page.sessions), next = w.next ?: page.next)
        }
    }

    /** The next older page of sessions. */
    suspend fun loadOlder() {
        val cursor = _world.value.next ?: return
        report {
            val page = api!!.listSessions(cursor)
            val w = _world.value
            _world.value = w.copy(sessions = mergeSessions(w.sessions, page.sessions), next = page.next)
        }
    }

    // Contacts (docs/personas.md), kept on the owner's server.

    private val _personas = MutableStateFlow<List<Persona>?>(null)
    val personas = _personas.asStateFlow()

    private val _personasError = MutableStateFlow<String?>(null)
    /** Why the contacts could not be read, in words (工单 #42); the agents are listed all the same. */
    val personasError = _personasError.asStateFlow()

    /** Contacts are extras: failing to read them is said on the page, never as a raw error that pops up later. */
    suspend fun loadPersonas() {
        try {
            _personas.value = api!!.listPersonas()
            _personasError.value = null
        } catch (e: Exception) {
            _personas.value = emptyList()
            _personasError.value = humanError(e)
        }
    }

    /** Starts a task with an agent on a machine, in a project (工单 #41: no contact needed); returns its id, or null (said). */
    suspend fun startTask(machineId: String, projectId: String, agent: String, text: String, key: String): String? = report {
        val s = api!!.startSession(machineId, projectId, agent, text, key)
        s?.let { replace(it) }
        say(t("${agentTitle(agent)} started a task", "${agentTitle(agent)} 已开始任务"))
        s?.sessionId
    }

    /**
     * A bot's next message (工单 #67): on in its conversation (unarchived first, rule 6), or a new one with the
     * composer's choices. What became of it is the caller's to say, in its box (工单 #61): a failure comes back as
     * the error's words, never said elsewhere.
     */
    suspend fun converse(machineId: String, agent: String, to: Session?, p: ConvParams, text: String, key: String, files: List<io.aide.android.api.AttachmentIn> = emptyList()): Result<Session> = try {
        val api = api!!
        val s = if (to != null) {
            if (to.status == io.aide.android.api.SessionStatus.ARCHIVED) api.archive(to.sessionId, false)
            api.sendMessage(to.sessionId, text, files)
        } else {
            api.startSession(machineId, p.projectId, agent, text, key, p.model, p.effort, p.mode, files)
        }
        s?.let { replace(it) }
        if (s == null) Result.failure(ApiError("internal", "")) else Result.success(s)
    } catch (e: Exception) {
        Result.failure(Exception(humanError(e)))
    }

    /** The owner's next message in a session, from its page (工单 #91: with its files); null when it went, else why. */
    suspend fun sendTo(s: Session, text: String, files: List<io.aide.android.api.AttachmentIn>): String? = try {
        api!!.sendMessage(s.sessionId, text, files)?.let { replace(it) }
        null
    } catch (e: Exception) {
        humanError(e)
    }

    /** Archives a conversation on its machine, or unarchives it; null when it worked, else why. */
    suspend fun archive(s: Session, on: Boolean): String? = try {
        api!!.archive(s.sessionId, on)?.let { replace(it) }
        null
    } catch (e: Exception) {
        humanError(e)
    }

    /** Deletes a room on its host, and with sessions its members' sessions made for it (工单 #73); null, or why not. */
    suspend fun deleteRoom(machineId: String, roomId: String, sessions: List<String>): String? = try {
        api!!.deleteRoom(machineId, roomId)
        val kept = sessions.filter { id -> runCatching { api!!.deleteSession(id) }.isFailure }
        if (kept.isEmpty()) null else t("The room is gone; ${kept.size} of its sessions could not be deleted.", "房间已删除；有 ${kept.size} 个成员会话没删掉。")
    } catch (e: Exception) {
        humanError(e)
    }

    /** Keeps a contact; returns it as kept, or null with why given to failed, for its page to say there (工单 #61). */
    suspend fun savePersonaOr(p: Persona, failed: (String) -> Unit): Persona? = try {
        api!!.savePersona(p)?.also { loadPersonas() }
    } catch (e: Exception) {
        failed(humanError(e))
        null
    }

    /** Keeps a contact; returns it as kept, or null (said). */
    suspend fun savePersona(p: Persona): Persona? = report { api!!.savePersona(p) }?.also { loadPersonas() }

    /** Forgets a contact; its sessions stay. */
    suspend fun deletePersona(p: Persona): Boolean = report {
        api!!.deletePersona(p.personaId)
        _personas.value = _personas.value?.filter { it.personaId != p.personaId }
        true
    } ?: false

    /** Writes to a contact; returns the session it went to, or null (said). */
    suspend fun talk(p: Persona, text: String, key: String): String? = report {
        val r = api!!.talkToPersona(p.personaId, text, key)
        r.session?.let { replace(it) }
        r.persona?.let { kept -> _personas.value = _personas.value?.map { if (it.personaId == kept.personaId) kept else it } }
        say(talkedText(p.name, r))
        r.session?.sessionId
    }

    private fun replace(s: Session) {
        val w = _world.value
        _world.value = w.copy(sessions = mergeSessions(w.sessions, listOf(s)))
    }
}

/**
 * An error as the person reads it (工单 #42): never a raw status like "404 Not Found". The owner's server without
 * contacts yet (an older relay) says so; a call the other side doesn't know says an update is due; else what it said.
 */
fun humanError(e: Exception): String {
    val code = (e as? ApiError)?.code ?: ""
    val msg = (e as? ApiError)?.message ?: e.message ?: e.toString()
    // What the core gave a reason key for (工单 #60), in the person's words.
    (e as? ApiError)?.let { a -> reasonText(a.reason, a.args, msg)?.let { return it } }
    return when {
        code == "unimplemented" && msg.startsWith("contacts_unavailable") ->
            t("Contacts need a newer server: your relay doesn't keep them yet. Work can still go to the agents listed here.", "联系人功能待中继升级：你的服务器（中继）还不支持保存联系人。下面列出的 Agent 照样可以直接派活。")
        code == "unimplemented" || Regex("""^(HTTP )?\d{3}( [A-Za-z ]+)?$""").matches(msg.trim()) ->
            t("The other side's Aide is older and doesn't know this yet: update Aide there (the server or the computer).", "对方的 Aide 版本较旧，还不支持这个操作：请更新那边（服务器或电脑）上的 Aide。")
        msg.isEmpty() -> code
        else -> msg
    }
}

/** What logging in, setting up or changing the account failed with, as the person reads it. */
fun loginError(e: ApiError): String = when (e.code) {
    "permission_denied" -> when {
        e.message.contains("setup code") -> t("Wrong setup code.", "设置码不对。")
        e.message.contains("current password") -> t("The current password is wrong.", "当前密码不对。")
        else -> t("Wrong account or password.", "账号或密码不对。")
    }
    "resource_exhausted" -> t("Too many tries. Wait a few minutes and try again.", "尝试次数太多，请过几分钟再试。")
    "already_exists" -> if (e.message.contains("set up already")) {
        t("This server is set up already: log in with its account instead.", "这台服务器已经设置过了，请改用它的账号登录。")
    } else {
        e.message
    }
    "failed_precondition" -> when {
        e.message.contains("not set up") -> t("This server is not set up yet: set it up with the setup code it printed.", "这台服务器还没有设置，请用它显示的设置码来设置。")
        e.message.contains("turned off") -> t("Logging in is turned off for this account. Turn it on from a device that is logged in.", "这个账号的登录已关闭，请在已登录的设备上重新打开。")
        else -> e.message
    }
    else -> e.message.ifEmpty { e.code }
}

/**
 * An error's reason key (工单 #60, internal/reason: localv1.ErrorReason on the error) in the person's language, as the
 * desktop's common:errors; null for one with no key, or a key this app has no words for (said as the core worded it).
 */
fun reasonText(reason: String, args: Map<String, String>, raw: String): String? {
    val a = { k: String -> args[k].orEmpty() }
    return when (reason) {
        "not_execution_machine" -> t("This device cannot be an execution machine.", "这台设备不能作为执行机器。")
        "not_set_up" -> t("Aide is not set up yet.", "Aide 还没有设置好。")
        "signed_out" -> t("This account is signed out on this device: sign in again.", "这台设备上的账号已经登出：请重新登录。")
        "no_relay" -> t("No server (relay) is set up yet.", "还没有配置服务器（中继）。")
        "machine_older" -> t("Aide on that machine is older and doesn’t know this yet: update it there, then try again.", "那台机器上的 Aide 版本较旧，还不认识这个操作：先在那台机器上更新 Aide，再试一次。")
        "no_host_online" -> t("No machine of yours is online to keep the conversation: start Aide on one.", "没有在线的机器能负责这场对话：先在一台机器上打开 Aide。")
        "no_room_host_online" -> t("No machine of yours is online to keep the room: start Aide on one.", "没有在线的机器能负责这个房间：先在一台机器上打开 Aide。")
        "cannot_sign_charter" -> t("This device can’t give this room its permissions: it is not one of your devices.", "这台设备不能给这个房间发许可：它不是你的设备。")
        "her_access_whole" -> t("Her’s access is whole and doesn’t expire: turn Her off to end it.", "Her 的授权是完整且不过期的：要结束它，请关掉 Her。")
        "name_length" -> t("A name is 1 to 64 bytes.", "名字要在 1 到 64 个字节之间。")
        "icon_needs_picture" -> t("The “picture” icon needs a picture.", "选了“图片”做图标，需要先选一张图片。")
        "bad_session_id" -> t("That session id is not right.", "会话编号不对。")
        "session_no_machine" -> t("No machine keeps this session.", "找不到保存这个会话的机器。")
        "claude_not_set_up" -> t("Claude Code is not set up on this machine.", "这台机器上还没有设置 Claude Code。")
        "claude_cli_missing" -> t("Claude Code’s command line is not installed on this machine.", "这台机器上没有安装 Claude Code 命令行。")
        "machine_paused" -> t("You paused this machine: resume it to hand it work.", "这台机器已被你暂停：恢复后才能派活。")
        "agent_paused" -> t("You paused ${a("agent")} on this machine: resume it to start new work with it.", "${a("agent")} 在这台机器上已被你暂停：恢复后才能给它派新任务。")
        "agent_paused_until" -> t("You paused ${a("agent")} on this machine until ${a("until")}.", "${a("agent")} 在这台机器上已被你暂停，到 ${a("until")} 自动恢复。")
        "agent_unavailable" -> t("${a("agent")} can’t be used on this machine now: $raw", "${a("agent")} 在这台机器上现在用不了：$raw")
        "project_missing" -> t("There is no project “${a("project")}” on this machine.", "这台机器上没有项目「${a("project")}」。")
        "project_gone" -> t("The project “${a("project")}” is gone.", "项目「${a("project")}」已经不在了。")
        "session_archived" -> t("This conversation is archived (read-only): unarchive it to go on.", "这条对话已归档（只读）：先取消归档再续聊。")
        "turn_in_progress" -> t("It is still busy with the last turn: wait, or interrupt it first.", "它还在忙上一轮：等它做完，或先打断它。")
        "room_no_grant" -> t("This agent’s grant hasn’t reached the machine keeping this room yet: try again in a moment.", "这个 agent 的授权还没到负责这个房间的机器：等一会儿再试。")
        "room_needs_name" -> t("A room needs a name.", "房间要有个名字。")
        "room_members_count" -> t("A room has 1 to ${a("max")} members.", "一个房间要有 1 到 ${a("max")} 个成员。")
        "room_not_in_charter" -> t("${a("agent")} (project ${a("project")}) is not on this room’s member list.", "${a("agent")}（项目 ${a("project")}）不在这个房间的成员名单里。")
        "room_not_found" -> t("The machine keeping this room no longer has it.", "负责这个房间的机器上找不到它了。")
        "nothing_to_say" -> t("Nothing written yet.", "还没写内容。")
        else -> null
    }
}
