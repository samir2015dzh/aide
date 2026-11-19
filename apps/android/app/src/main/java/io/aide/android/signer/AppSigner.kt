package io.aide.android.signer

import io.aide.android.hermeshcore.Signer
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.runBlocking
import java.util.Locale

/**
 * The owner role asks this app, through the shared core's signer
 * (internal/signer, RunSigner), to create root keys and to sign with them.
 * Each batch is signed only after the person approved what it says (the
 * cards, in the signing sheet, or in the app's own sheet that led to it: an
 * Expectation) and then confirmed it is them (BiometricPrompt: a strong
 * biometric or the screen lock), as the iPhone signs after Face ID.
 *
 * The core calls these on threads of its own and waits for the answer, so
 * they block until the person answered.
 */
class AppSigner : Signer {
    /** A batch waiting for the person in the signing sheet. */
    data class Pending(val cards: List<Card>, val answer: CompletableDeferred<Boolean>)

    /** The biometric prompt to show, for the batch whose cards these are. */
    data class Confirm(val title: String, val answer: CompletableDeferred<Boolean>)

    /** Two digits to type on the device or machine just approved. */
    data class Match(val label: String, val digits: String, val machine: Boolean)

    private val _pending = MutableStateFlow<Pending?>(null)
    val pending = _pending.asStateFlow()
    private val _confirm = MutableStateFlow<Confirm?>(null)
    val confirm = _confirm.asStateFlow()
    private val _match = MutableStateFlow<Match?>(null)
    val match = _match.asStateFlow()
    private val _failures = MutableSharedFlow<String>(extraBufferCapacity = 8)
    val failures = _failures.asSharedFlow()

    @Volatile private var expected: Expectation? = null
    @Volatile private var lastTitle = ""
    @Volatile private var ownBatch = false
    /** The person cancelled the confirmation of their own setup or join: the page that asked goes back to its button. */
    @Volatile var cancelledOwn = false

    /** The next batch was confirmed in the app already. */
    fun expect(e: Expectation) {
        expected = e
        cancelledOwn = false
    }

    fun clearMatch() {
        _match.value = null
    }

    override fun createKey(): ByteArray = RootKeys.create()

    override fun deleteKey(keyID: String) = RootKeys.delete(keyID)

    override fun approve(cards: String): Boolean {
        val parsed = Cards.parse(cards)
        lastTitle = parsed.joinToString(" · ") { it.title }
        ownBatch = parsed.isNotEmpty() && parsed.all { it.own }
        val e = expected
        if (e != null && e.matches(parsed)) {
            expected = null
            return true
        }
        val p = Pending(parsed, CompletableDeferred())
        _pending.value = p
        return try {
            runBlocking { p.answer.await() }
        } finally {
            _pending.compareAndSet(p, null)
        }
    }

    override fun sign(keyID: String, messages: String): String {
        val msgs = Cards.messages(messages)
        val key = RootKeys.key(keyID)
        val sigs = ArrayList<ByteArray>(msgs.size)
        var confirmed = false
        for (m in msgs) {
            while (true) {
                if (!confirmed) {
                    if (!confirmPerson()) {
                        cancelledOwn = ownBatch
                        throw Exception("Not confirmed: nothing was signed")
                    }
                    confirmed = true
                }
                try {
                    sigs += RootKeys.sign(key, m)
                    break
                } catch (e: RootKeys.NeedsConfirmation) {
                    confirmed = false // the time ran out mid-batch: ask again
                }
            }
        }
        return Cards.signatures(sigs)
    }

    /** Shows the biometric prompt (MainActivity) and waits for its answer. */
    private fun confirmPerson(): Boolean {
        val c = Confirm(lastTitle, CompletableDeferred())
        _confirm.value = c
        return try {
            runBlocking { c.answer.await() }
        } finally {
            _confirm.compareAndSet(c, null)
        }
    }

    override fun failed(message: String) {
        _failures.tryEmit(message)
    }

    override fun matched(label: String, digits: String, machine: Boolean) {
        _match.value = Match(label, digits, machine)
    }

    override fun show() {}

    override fun language(): String = if (Locale.getDefault().language == "zh") "zh-CN" else "en"
}
