package io.hermesh.android

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.biometric.BiometricManager.Authenticators.BIOMETRIC_STRONG
import androidx.biometric.BiometricManager.Authenticators.DEVICE_CREDENTIAL
import androidx.biometric.BiometricPrompt
import androidx.core.content.ContextCompat
import androidx.fragment.app.FragmentActivity
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.lifecycleScope
import androidx.lifecycle.repeatOnLifecycle
import io.hermesh.android.model.PairingLink
import io.hermesh.android.push.Push
import io.hermesh.android.signer.AppSigner
import io.hermesh.android.ui.HermeshTheme
import io.hermesh.android.ui.Root
import io.hermesh.android.ui.t
import kotlinx.coroutines.launch

/**
 * The app's one window. It also asks the person to confirm it is them
 * (BiometricPrompt, a strong biometric or the screen lock) when the signer
 * is about to sign, which needs an activity in front.
 */
class MainActivity : FragmentActivity() {
    private val model get() = (application as HermeshApp).model

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent { HermeshTheme { Root(model) } }
        lifecycleScope.launch {
            repeatOnLifecycle(Lifecycle.State.STARTED) {
                model.signer.confirm.collect { c -> if (c != null) prompt(c) }
            }
        }
        handle(intent)
        // Notifications need the person's leave on Android 13 and later.
        if (Build.VERSION.SDK_INT >= 33 &&
            ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) {
            registerForActivityResult(ActivityResultContracts.RequestPermission()) {}.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        handle(intent)
    }

    /** hermesh://pair?code=…: a request to approve; or a notification the app showed, tapped. */
    private fun handle(intent: Intent?) {
        Push.target(intent)?.let { model.open(it); return }
        val code = intent?.data?.toString()?.let(PairingLink::code) ?: return
        lifecycleScope.launch { model.lookup(code) }
    }

    private fun prompt(c: AppSigner.Confirm) {
        if (c.answer.isCompleted) return
        val info = BiometricPrompt.PromptInfo.Builder()
            .setTitle(t("Confirm it’s you", "确认是你本人"))
            .setSubtitle(c.title.take(120))
            .setDescription(t("Aide signs this with this phone’s key.", "Aide 将用这台手机的密钥签署此请求。"))
            .setAllowedAuthenticators(BIOMETRIC_STRONG or DEVICE_CREDENTIAL)
            .build()
        BiometricPrompt(this, ContextCompat.getMainExecutor(this), object : BiometricPrompt.AuthenticationCallback() {
            override fun onAuthenticationSucceeded(result: BiometricPrompt.AuthenticationResult) {
                c.answer.complete(true)
            }

            override fun onAuthenticationError(errorCode: Int, errString: CharSequence) {
                if (errorCode == BiometricPrompt.ERROR_NO_BIOMETRICS || errorCode == BiometricPrompt.ERROR_NO_DEVICE_CREDENTIAL) {
                    model.say(t("Set a screen lock to sign with this phone.", "请先设置锁屏，这台手机才能签署。"))
                }
                c.answer.complete(false)
            }
        }).authenticate(info)
    }
}
