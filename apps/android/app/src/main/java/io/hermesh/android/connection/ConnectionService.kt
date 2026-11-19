package io.hermesh.android.connection

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.IBinder
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import io.hermesh.android.HermeshApp
import io.hermesh.android.MainActivity
import io.hermesh.android.R
import io.hermesh.android.ui.t
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch

/**
 * Keeps the core's relay connection, and the Watch the app's notices come
 * from (push/LiveNotices.kt), while the app is in the background: a
 * foreground service with a quiet, ongoing notification
 * (docs/android-app.md §4). START_STICKY: when the system reclaims it,
 * it starts it again when it may; nothing here goes around the system's
 * limits. HyperOS may still end it when the app is swiped away.
 */
class ConnectionService : Service() {
    private val model get() = (application as HermeshApp).model
    private var showing: Job? = null

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        try {
            ServiceCompat.startForeground(this, NOTIFICATION, notification(model.link.value), ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
        } catch (e: Exception) {
            // Android 12+: not allowed to start one now (ForegroundServiceStartNotAllowedException).
            stopSelf()
            return START_NOT_STICKY
        }
        // Turned off meanwhile, or restarted by the system after it was: stays off
        // (in the foreground first, as startForegroundService requires).
        if (!model.keepalive.enabled.value) {
            ServiceCompat.stopForeground(this, ServiceCompat.STOP_FOREGROUND_REMOVE)
            stopSelf()
            return START_NOT_STICKY
        }
        model.keep(true)
        if (showing == null) showing = model.scope.launch {
            model.link.collect { l ->
                runCatching { getSystemService(NotificationManager::class.java)?.notify(NOTIFICATION, notification(l)) }
            }
        }
        return START_STICKY
    }

    override fun onDestroy() {
        showing?.cancel()
        model.keep(false)
        super.onDestroy()
    }

    private fun notification(l: Link): Notification {
        val nm = getSystemService(NotificationManager::class.java)
        nm?.createNotificationChannel(
            NotificationChannel(CHANNEL, t("Connection", "连接状态"), NotificationManager.IMPORTANCE_LOW).apply {
                setShowBadge(false)
            },
        )
        val open = Intent(this, MainActivity::class.java).apply { flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP }
        return NotificationCompat.Builder(this, CHANNEL)
            .setSmallIcon(R.drawable.ic_stat_aide)
            .setContentTitle(
                when (l) {
                    Link.Connected -> t("Aide is connected", "Aide 已连接")
                    Link.Reconnecting -> t("Aide is reconnecting…", "Aide 正在重连…")
                    Link.Disconnected -> t("Aide is disconnected", "Aide 已断开")
                },
            )
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .setCategory(NotificationCompat.CATEGORY_SERVICE)
            .setOngoing(true)
            .setShowWhen(false)
            .setSilent(true)
            .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)
            .setContentIntent(PendingIntent.getActivity(this, 0, open, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT))
            .build()
    }

    companion object {
        private const val CHANNEL = "connection"
        private const val NOTIFICATION = 1

        /** Starts it (the app is in front: Android 12+ allows it then); again is a no-op. */
        fun start(context: Context) {
            runCatching { ContextCompat.startForegroundService(context, Intent(context, ConnectionService::class.java)) }
        }

        fun stop(context: Context) {
            context.stopService(Intent(context, ConnectionService::class.java))
        }
    }
}
