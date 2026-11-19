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
import io.aide.android.api.GrantSpec
import io.aide.android.api.LocalApi
import io.aide.android.api.PairingDetails
import io.aide.android.api.Session
import io.aide.android.connection.ConnectionService
import io.aide.android.connection.Keepalive
import io.aide.android.connection.Link
import io.aide.android.connection.link
import io.aide.android.hermeshcore.Hermeshcore
import io.aide.android.push.LiveNotices
import io.aide.android.push.NoticeTarget
import io.aide.android.push.Push
import io.aide.android.push.show
import io.aide.android.signer.AppSigner
import io.aide.android.signer.Expectation
import io.aide.android.ui.t
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
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
 * Otherwise in the background the core pauses (no relay, no signing), as
 * on the iPhone, and the app syncs when it comes back to the front. What
 * the Watch brings that needs the person is notified (push/LiveNotices.kt).
 */
class AppModel(private val context: Context) {
    enum class Phase { Starting, Failed, Onboarding, Ready }

    val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    val signer = AppSigner()

    private val _phase = MutableStateFlow(Phase.Starting)
    val phase = _phase.asStateFlow()
    private val _world = MutableStateFlow(World())
    val world = _world.asStateFlow()
    private val _foreground = MutableStateFlow(false)
    val foreground = _foreground.asStateFlow()
    private val _messages = MutableSharedFlow<String>(extraBufferCapacity = 8)
    val messages = _messages.asSharedFlow()
    /** Pairings the person dismissed here (they expire on the relay). */
    private val _dismissed = MutableStateFlow(setOf<String>())
    val dismissed = _dismissed.asStateFlow()
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
                val dir = File(context.filesDir, "hermesh").apply { mkdirs() }
                val info = withContext(Dispatchers.IO) { Hermeshcore.start(dir.absolutePath) }
                api = LocalApi(info.port, info.token)
                Hermeshcore.runSigner(signer)
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
        scope.launch { signer.failures.collect { _messages.tryEmit("Nothing was signed: $it") } }
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
            scope.launch(coreCalls) { Hermeshcore.resume() }
            if (watching?.isActive != true) watching = scope.launch { watch() }
        } else if (_active.value) {
            watching?.cancel()
            watching = null
            scope.launch(coreCalls) { Hermeshcore.pause() }
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
    private suspend fun watch() {
        val api = api ?: return
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
                if (current != null && current != network) Hermeshcore.networkChanged()
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
                if (network == current) Hermeshcore.pathChanged()
            }
        })
    }

    private val _opening = MutableStateFlow<NoticeTarget?>(null)
    /** What a notification tapped opens, until the screens took it (opened). */
    val opening = _opening.asStateFlow()

    fun open(target: NoticeTarget) {
        _opening.value = target
        if (target.pairing.isNotEmpty()) scope.launch {
            phase.first { it == Phase.Ready }
            lookup(target.pairing)
        }
    }

    fun opened() {
        _opening.value = null
    }

    fun say(text: String) {
        _messages.tryEmit(text)
    }

    private inline fun <T> report(block: () -> T): T? = try {
        block()
    } catch (e: ApiError) {
        say(e.message.ifEmpty { e.code })
        null
    } catch (e: Exception) {
        say(e.message ?: e.toString())
        null
    }

    // Joining and setting up.

    val deviceLabel: String get() = listOf(Build.MANUFACTURER.replaceFirstChar { it.uppercase() }, Build.MODEL).distinct().joinToString(" ")

    /** Claims a new self-hosted server with its claim code: this phone becomes the owner's first trusted device. */
    suspend fun claim(server: String, claimCode: String): String? {
        val api = api ?: return "The core is not running"
        signer.expect(Expectation.Own)
        return try {
            api.setup(server, claimCode.trim(), deviceLabel)
            refreshStatus()
            null
        } catch (e: ApiError) {
            if (signer.cancelledOwn) null else e.message
        }
    }

    /**
     * Logs in with the owner's account and password: this phone becomes a
     * trusted device with no other device approving, nothing to compare and
     * nothing to sign here (docs/password-login.md). server empty: the
     * build's own. Returns what went wrong, or null.
     */
    suspend fun passwordLogin(server: String, account: String, password: String): String? {
        val api = api ?: return "The core is not running"
        return try {
            api.passwordLogin(server, account.trim(), password, deviceLabel)
            refreshStatus()
            null
        } catch (e: ApiError) {
            when (e.code) {
                "permission_denied" -> t("Wrong account or password.", "账号或密码不对。")
                "resource_exhausted" -> t("Too many wrong passwords. Try again in a few minutes.", "密码错误次数太多，请过几分钟再试。")
                else -> e.message.ifEmpty { e.code }
            }
        }
    }

    sealed interface Join {
        data object Idle : Join
        data object Asking : Join
        /** Waiting for a trusted device to approve: it shows the same verification code. */
        data class Waiting(val code: String, val verificationCode: String) : Join
        /** Approved: type the two digits the approving device shows. */
        data class Digits(val code: String, val approvedBy: String, val error: String? = null) : Join
        data class Failed(val message: String) : Join
    }

    private val _join = MutableStateFlow<Join>(Join.Idle)
    val join = _join.asStateFlow()
    private var joining: Job? = null

    /** Asks the owner at address to take this phone as a trusted device, and waits for one of theirs to approve. */
    fun join(address: ConnectAddress) {
        val api = api ?: return
        joining?.cancel()
        _join.value = Join.Asking
        joining = scope.launch {
            signer.expect(Expectation.Own)
            try {
                val res = api.requestJoin(address.relay, address.owner, deviceLabel)
                _join.value = Join.Waiting(res.code, res.verificationCode)
                while (true) {
                    val w = api.waitJoin(res.code)
                    if (w.done && !w.matchRequired) {
                        api.confirmJoin(res.code, accept = true)
                        _join.value = Join.Idle
                        refreshStatus()
                        return@launch
                    }
                    if (w.done) {
                        _join.value = Join.Digits(res.code, w.approvedBy)
                        return@launch
                    }
                    if (w.expired) throw ApiError("expired", "The request expired. Start again.")
                }
            } catch (e: ApiError) {
                _join.value = if (signer.cancelledOwn) Join.Idle else Join.Failed(e.message.ifEmpty { e.code })
            }
        }
    }

    fun confirmDigits(digits: String) {
        val d = _join.value as? Join.Digits ?: return
        scope.launch {
            try {
                api!!.confirmJoin(d.code, accept = true, match = digits.trim())
                _join.value = Join.Idle
                refreshStatus()
            } catch (e: ApiError) {
                _join.value = d.copy(error = if (e.code == "invalid_argument") "Those aren’t the digits. Check the other device and try again." else e.message)
            }
        }
    }

    fun cancelJoin() {
        joining?.cancel()
        val code = when (val j = _join.value) {
            is Join.Waiting -> j.code
            is Join.Digits -> j.code
            else -> null
        }
        if (code != null) scope.launch { runCatching { api?.confirmJoin(code, accept = false) } }
        _join.value = Join.Idle
    }

    // Approvals.

    fun dismiss(p: PairingDetails) {
        _dismissed.value = _dismissed.value + p.code
    }

    /** Approves a machine; the answer carries the two digits to type on it when it asked for them. */
    suspend fun approveMachine(p: PairingDetails, name: String): String? = report {
        if (!p.confirmed) signer.expect(Expectation.Code(p.verificationCode))
        api!!.approveMachine(p.code, name).matchCode.also { dismiss(p) }
    }

    /** Approves an agent: every project and coding agent, for ttlDays. */
    suspend fun approveClient(p: PairingDetails, label: String, ttlDays: Int): Boolean = report {
        if (!p.confirmed) signer.expect(Expectation.Code(p.verificationCode))
        api!!.approveClient(p.code, GrantSpec(label = label, agents = listOf("*"), ttlDays = ttlDays))
        dismiss(p)
        true
    } ?: false

    /** Approves a trusted device; the answer carries the two digits to type on it. */
    suspend fun approveDevice(p: PairingDetails): String? = report {
        if (!p.confirmed) signer.expect(Expectation.Code(p.verificationCode))
        api!!.approveDevice(p.code).matchCode.also { dismiss(p) }
    }

    /** A pairing code typed or scanned here: its request, to approve. */
    suspend fun lookup(code: String): PairingDetails? = report {
        val p = api!!.lookupPairing(code) ?: return@report null
        live.known(p.code) // looked up here: no notification
        _dismissed.value = _dismissed.value - p.code
        val w = _world.value
        _world.value = w.copy(pairings = w.pairings.filter { it.code != p.code } + p)
        p
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

    suspend fun interrupt(s: Session) {
        report { api!!.interrupt(s.sessionId)?.let { replace(it) } }
    }

    suspend fun recheck(machineId: String) {
        report { api!!.checkAgents(machineId) }
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

    private fun replace(s: Session) {
        val w = _world.value
        _world.value = w.copy(sessions = mergeSessions(w.sessions, listOf(s)))
    }
}
