package io.hermesh.android.ui

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.provider.Settings
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import io.hermesh.android.connection.Link
import io.hermesh.android.model.AppModel

fun linkText(l: Link): String = when (l) {
    Link.Connected -> t("connected", "已连接")
    Link.Reconnecting -> t("reconnecting…", "重连中…")
    Link.Disconnected -> t("disconnected", "已断开")
}

/** This phone's app settings, where HyperOS keeps Battery saver and Autostart. */
fun openAppSettings(context: Context) {
    runCatching {
        context.startActivity(
            Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.fromParts("package", context.packageName, null))
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
        )
    }
}

/** The switch for staying connected in the background, the connection, and the way to HyperOS's settings (Machines › This phone). */
@Composable
fun KeepaliveSettings(model: AppModel) {
    val on by model.keepalive.enabled.collectAsState()
    val link by model.link.collectAsState()
    val context = LocalContext.current
    Labeled(t("Connection", "连接"), linkText(link))
    Row(Modifier.fillMaxWidth().padding(vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(t("Stay connected in the background", "后台保持连接"), style = MaterialTheme.typography.bodyMedium)
            Text(
                if (on) {
                    t("Notifies you while Aide is in the background, with a quiet notification that it is connected.", "Aide 在后台时也能通知你，并显示一条安静的「已连接」常驻通知。")
                } else {
                    t("Notifies you only while Aide is open.", "仅在 Aide 打开时通知你。")
                },
                style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Spacer(Modifier.width(12.dp))
        Switch(checked = on, onCheckedChange = { model.keepalive.enable(it) })
    }
    if (on) TextButton(onClick = { openAppSettings(context) }) { Text(t("Battery and autostart settings", "省电策略与自启动设置")) }
}

/**
 * Once, while staying connected is on: how to keep HyperOS from ending
 * the connection (Battery saver: No restrictions; Autostart on). Skipped
 * or followed, it is not shown again.
 */
@Composable
fun BackgroundGuide(model: AppModel) {
    val on by model.keepalive.enabled.collectAsState()
    val guided by model.keepalive.guided.collectAsState()
    if (!on || guided) return
    val context = LocalContext.current
    AlertDialog(
        onDismissRequest = { model.keepalive.guideDone() },
        title = { Text(t("Get notified in the background", "在后台也收到通知")) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(
                    t(
                        "Aide keeps its own connection to notify you. So that HyperOS doesn’t end it in the background:",
                        "Aide 靠自己的连接通知你。为免 HyperOS 在后台把它断掉，建议：",
                    ),
                )
                Text(t("1. Battery saver: No restrictions", "1. 省电策略设为「无限制」"))
                Text(t("2. Autostart: on", "2. 打开「自启动」"))
                Text(
                    t(
                        "Both are in Settings › Apps › Manage apps › Aide (or long-press the Aide icon › App info).",
                        "两项都在 设置 › 应用设置 › 应用管理 › Aide（或长按桌面上的 Aide 图标 › 应用信息）。",
                    ),
                    style = MaterialTheme.typography.bodySmall,
                )
                Text(
                    t(
                        "Even so, after Aide is swiped away from recent apps, notifications may not arrive.",
                        "即使这样设置，从最近任务里划掉 Aide 后，通知仍可能收不到。",
                    ),
                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        },
        confirmButton = {
            TextButton(onClick = {
                model.keepalive.guideDone()
                openAppSettings(context)
            }) { Text(t("Open settings", "去设置")) }
        },
        dismissButton = { TextButton(onClick = { model.keepalive.guideDone() }) { Text(t("Skip", "跳过")) } },
    )
}
