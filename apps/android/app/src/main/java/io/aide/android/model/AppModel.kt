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
                    _world.value = _world.value.apply(r)
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

    fun say(text: String) {
        _messages.tryEmit(text)
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

    /** Logs in to the owner's server with the account and password. Returns what went wrong, or null. */
    suspend fun logIn(server: String, account: String, password: String): String? = onboard {
        it.logIn(server, account.trim(), password, deviceLabel)
    }

    /** Sets a new server up with the setup code it printed, keeping this account and password, and logs in to it. */
    suspend fun setUp(server: String, setupCode: String, account: String, password: String): String? = onboard {
        it.setup(server, setupCode.trim(), account.trim(), password, deviceLabel)
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

    suspend fun recheck(machineId: String) {
        report { api!!.checkAgents(machineId) }
    }

    /** Hides a coding agent a machine lists (one the owner doesn't trust, as on the desktop), or shows it again. */
    suspend fun setAgentHidden(machineId: String, agent: String, hidden: Boolean) {
        report { api!!.setAgentHidden(machineId, agent, hidden) }
    }

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
