package io.aide.android

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.core.content.ContextCompat
import io.aide.android.push.Push
import io.aide.android.ui.AideTheme
import io.aide.android.ui.Root

/** The app's one window. */
class MainActivity : ComponentActivity() {
    private val model get() = (application as AideApp).model

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent { AideTheme { Root(model) } }
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

    /** A notification the app showed, tapped: what it is about. */
    private fun handle(intent: Intent?) {
        Push.target(intent)?.let { model.open(it) }
    }
}
