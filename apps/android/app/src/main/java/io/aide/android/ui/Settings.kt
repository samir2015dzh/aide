package io.aide.android.ui

import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import io.aide.android.api.Device
import io.aide.android.api.GetLoginResponse
import io.aide.android.api.SetLoginRequest
import io.aide.android.model.AppModel
import io.aide.android.model.MIN_PASSWORD
import io.aide.android.model.newLoginProblem
import io.aide.android.model.normalizeAccount
import kotlinx.coroutines.launch

/**
 * Settings: the account the server keeps (docs/accounts.md §4: its name
 * and password, changed with the current password, and logging in turned
 * off or on), the owner's devices (any other one can be signed out), and
 * this phone. Laid out as iOS's Settings (工单 #95): inset grouped cards,
 * a row each, explanations as the group's footnote.
 */
@Composable
fun SettingsTab(model: AppModel) {
    val world by model.world.collectAsState()
    val st = world.status
    val keep by model.keepalive.enabled.collectAsState()
    var login by remember { mutableStateOf<GetLoginResponse?>(null) }
    // Bumped after a change: the account is read again.
    var version by remember { mutableIntStateOf(0) }
    LaunchedEffect(version) {
        login = model.login()
        if (version == 0) model.refreshDevices()
    }
    var editing by remember { mutableStateOf<AccountChange?>(null) }
    var signingOut by remember { mutableStateOf<Device?>(null) }
    val list = rememberLazyListState()
    Column(Modifier.fillMaxSize()) {
        PageTitle(t("Settings", "设置"), (login?.account?.ifEmpty { null } ?: st.account).takeIf { it.isNotEmpty() }?.let { t("Signed in as $it", "已登录账号 $it") }, collapsed = list.collapsed())
        SyncLine(model)
        LazyColumn(Modifier.fillMaxSize(), state = list, contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 24.dp)) {
            item { SectionHeader(t("Account", "账号")) }
            item {
                val l = login
                // Who is signed in, said as iOS's account row: the account large, its server under it.
                val account = l?.account?.ifEmpty { null } ?: st.account
                Column(Modifier.fillMaxWidth().aideCard()) {
                    Row(Modifier.padding(16.dp), verticalAlignment = Alignment.CenterVertically) {
                        MachineMark(account.ifEmpty { "?" }, online = true, size = 52)
                        HSpace(14)
                        Column(Modifier.weight(1f)) {
                            Text(account, style = MaterialTheme.typography.titleLarge, maxLines = 1)
                            Text(l?.server?.ifEmpty { null } ?: st.relayUrl, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
                        }
                    }
                    if (l != null && l.convert) {
                        Hairline(Modifier.padding(start = 16.dp))
                        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                            Text(
                                t("Your password login from before accounts waits to be converted: type its password once.", "账号功能之前的密码登录还没有转换：输入一次它的密码即可。"),
                                style = MaterialTheme.typography.bodySmall, color = Warning,
                            )
                            FilledButton(onClick = { editing = AccountChange.Convert }) { Text(t("Convert…", "转换…")) }
                        }
                    }
                    if (l != null && !l.convert) {
                        Hairline(Modifier.padding(start = 16.dp))
                        Row(Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 11.dp, bottom = 11.dp), verticalAlignment = Alignment.CenterVertically) {
                            Column(Modifier.weight(1f)) {
                                Text(t("Logging in", "允许登录"), style = MaterialTheme.typography.bodyLarge)
                                Text(
                                    if (l.off) {
                                        t("Off: no new device, machine or agent can log in. Those logged in stay.", "已关闭：新的设备、机器或 Agent 都无法登录，已登录的不受影响。")
                                    } else {
                                        t("On: whatever has the account and password can log in.", "已开启：知道账号和密码就能登录。")
                                    },
                                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                            }
                            Spacer(Modifier.width(12.dp))
                            IosSwitch(checked = !l.off, onCheckedChange = { editing = AccountChange.Logins(off = !it) })
                        }
                        Hairline(Modifier.padding(start = 16.dp))
                        ActionLine(t("Change account name…", "修改账号…")) { editing = AccountChange.Name }
                        Hairline(Modifier.padding(start = 16.dp))
                        ActionLine(t("Change password…", "修改密码…")) { editing = AccountChange.Password }
                    }
                }
                Footnote(t("Log in on a new device, computer or agent with this server, account and password.", "在新的设备、电脑或 Agent 上，用这个服务器、账号和密码登录。"))
            }

            item { SectionHeader(t("Devices", "设备"), count = world.devices.size) }
            itemsIndexed(world.devices, key = { _, d -> d.keyId }) { i, d ->
                Box(Modifier.groupRow(i, world.devices.size, inset = 64.dp)) { DeviceRow(d, signOut = { signingOut = d }) }
            }

            item {
                SectionHeader(t("This phone", "这台手机"))
                Column(Modifier.fillMaxWidth().aideCard().padding(start = 16.dp)) {
                    Row(Modifier.padding(end = 16.dp, top = 12.dp, bottom = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                        Text(t("Aide on this phone", "这台手机上的 Aide"), style = MaterialTheme.typography.titleMedium, modifier = Modifier.weight(1f))
                        // The same state the Connection line under it says (KeepaliveSettings).
                        val link by model.link.collectAsState()
                        if (link == io.aide.android.connection.Link.Connected) StatusPill(t("Connected", "已连接"), Tone.Good) else StatusPill(linkText(link), Tone.Quiet)
                    }
                    InfoRow(t("Server", "服务器"), st.relayUrl)
                    if (st.account.isNotEmpty()) InfoRow(t("Account", "账号"), st.account)
                    InfoRow(t("Version", "版本"), st.version)
                    KeepaliveSettings(model)
                    InfoRow(
                        t("Notifications", "通知"),
                        if (keep) t("through Aide’s own connection", "经 Aide 自己的连接") else t("while Aide is open", "Aide 打开时"),
                    )
                }
            }
        }
    }
    editing?.let { c ->
        val l = login
        if (l != null) AccountDialog(model, c, l, done = { changed -> editing = null; if (changed) version++ })
    }
    signingOut?.let { d -> SignOutDialog(model, d, world.machines.firstOrNull { it.machineId == d.machineId }?.status?.her?.let { it.enabled || it.data } == true) { signingOut = null } }
}

/** A row of a card's key and value, under a hairline (iOS's value1 cell); the card's leading padding is the hairline's inset. */
@Composable
fun InfoRow(label: String, value: String) {
    Hairline()
    Box(Modifier.padding(end = 16.dp, top = 9.dp, bottom = 9.dp)) { Labeled(label, value) }
}

/** A row that acts, in the accent (as iOS's button rows in a list). */
@Composable
private fun ActionLine(text: String, onClick: () -> Unit) {
    Text(text, style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.primary, modifier = Modifier.fillMaxWidth().rowPress(onClick).padding(horizontal = 16.dp, vertical = 11.dp))
}

/** A group's footnote, under its card. */
@Composable
fun Footnote(text: String) {
    Text(text, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 7.dp))
}

@Composable
private fun DeviceRow(d: Device, signOut: () -> Unit) {
    Row(Modifier.fillMaxWidth().padding(start = 16.dp, end = 6.dp, top = 10.dp, bottom = 10.dp), verticalAlignment = Alignment.CenterVertically) {
        MachineMark(d.label, d.thisDevice || d.machineOnline, 36)
        HSpace(12)
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(d.label, style = MaterialTheme.typography.titleMedium, maxLines = 1, modifier = Modifier.weight(1f, fill = false))
                if (d.thisDevice) { HSpace(8); StatusPill(t("This phone", "这台手机"), Tone.Live) }
            }
            Text(
                listOfNotNull(
                    d.platform.takeIf { it.isNotEmpty() },
                    if (d.machineId.isNotEmpty()) (if (d.machineOnline) t("its machine online", "它的机器在线") else t("its machine offline", "它的机器离线")) else null,
                    if (d.legacy) t("set up before accounts", "账号功能之前设置") else null,
                ).joinToString(" · "),
                style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        // Any device signs out another, never itself.
        if (!d.thisDevice) PlainButton(onClick = signOut) { Text(t("Sign out…", "登出…"), color = MaterialTheme.colorScheme.error) }
    }
}

/** What the account dialog changes; each needs the current password. */
private sealed interface AccountChange {
    data object Name : AccountChange
    data object Password : AccountChange
    data class Logins(val off: Boolean) : AccountChange
    data object Convert : AccountChange
}

@Composable
private fun AccountDialog(model: AppModel, c: AccountChange, login: GetLoginResponse, done: (changed: Boolean) -> Unit) {
    var current by remember { mutableStateOf("") }
    var name by remember { mutableStateOf(login.account) }
    var fresh by remember { mutableStateOf("") }
    var again by remember { mutableStateOf("") }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    fun change() {
        val req = when (c) {
            AccountChange.Name -> {
                newLoginProblem(name, "x".repeat(MIN_PASSWORD))?.let { error = it; return }
                SetLoginRequest(current, account = name.trim(), off = login.off)
            }
            AccountChange.Password -> {
                if (fresh != again) {
                    error = t("The two new passwords differ.", "两次输入的新密码不一样。")
                    return
                }
                newLoginProblem(login.account, fresh)?.let { error = it; return }
                SetLoginRequest(current, newPassword = fresh, off = login.off)
            }
            is AccountChange.Logins -> SetLoginRequest(current, off = c.off)
            AccountChange.Convert -> SetLoginRequest(current, off = login.off)
        }
        busy = true
        scope.launch {
            val signedOut = model.setLogin(req) { error = it }
            busy = false
            if (signedOut == null) return@launch
            model.say(
                when (c) {
                    AccountChange.Password -> t("Password changed; $signedOut devices and agents signed out", "密码已修改，登出了 $signedOut 台设备和 Agent")
                    AccountChange.Name -> t("Account name changed", "账号已修改")
                    is AccountChange.Logins -> if (c.off) t("Logging in is off", "登录已关闭") else t("Logging in is on", "登录已开启")
                    AccountChange.Convert -> t("Converted", "已转换")
                },
            )
            done(true)
        }
    }
    val ready = !busy && current.isNotEmpty() && when (c) {
        AccountChange.Name -> name.isNotBlank() && normalizeAccount(name) != login.account
        AccountChange.Password -> fresh.isNotEmpty() && again.isNotEmpty()
        else -> true
    }
    IosAlert(
        onDismissRequest = { if (!busy) done(false) },
        title = when (c) {
            AccountChange.Name -> t("Change the account name", "修改账号")
            AccountChange.Password -> t("Change the password", "修改密码")
            is AccountChange.Logins -> if (c.off) t("Turn logging in off", "关闭登录") else t("Turn logging in on", "开启登录")
            AccountChange.Convert -> t("Convert the password login", "转换密码登录")
        },
        actions = listOf(
            AlertAction(t("Cancel", "取消"), AlertRole.Cancel, enabled = !busy) { done(false) },
            AlertAction(t("Change", "修改"), AlertRole.Preferred, enabled = ready) { change() },
        ),
    ) {
        when (c) {
            AccountChange.Name -> {
                IosField(name, { name = it; error = null }, label = t("New account name", "新账号"))
                Text(t("Every device, computer and agent logs in with it from now on.", "以后所有设备、电脑和 Agent 都用它登录。"), style = MaterialTheme.typography.bodySmall)
            }
            AccountChange.Password -> {
                Text(
                    t(
                        "A new password signs out your other devices and every agent that logged in: they log in again with it. This phone and your machines stay.",
                        "新密码会登出你的其他设备和所有已登录的 Agent，它们需要用新密码重新登录。这台手机和你的机器不受影响。",
                    ),
                    style = MaterialTheme.typography.bodySmall, color = Warning,
                )
                PasswordField(fresh, { fresh = it; error = null }, t("New password", "新密码"), hint = passwordHint)
                PasswordField(again, { again = it; error = null }, t("New password again", "再次输入新密码"))
            }
            is AccountChange.Logins -> Text(
                if (c.off) {
                    t("No new device, machine or agent can log in until you turn it on again. Those logged in stay.", "在你重新开启之前，新的设备、机器或 Agent 都无法登录；已登录的不受影响。")
                } else {
                    t("Whatever has the account and password can log in again.", "知道账号和密码就又可以登录了。")
                },
                style = MaterialTheme.typography.bodySmall,
            )
            AccountChange.Convert -> Text(
                t("The server keeps the account from now on; devices and agents approved before keep working.", "从此由服务器保存账号；之前批准的设备和 Agent 照常可用。"),
                style = MaterialTheme.typography.bodySmall,
            )
        }
        PasswordField(current, { current = it; error = null }, t("Current password", "当前密码"))
        error?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
    }
}

@Composable
private fun PasswordField(value: String, onChange: (String) -> Unit, label: String, hint: (@Composable () -> Unit)? = null) {
    IosField(value, onChange, label = label, password = true, keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password), supportingText = hint)
}

/** Signing another device out: it can no longer connect, and its own machine is removed (RemoveDevice). */
@Composable
private fun SignOutDialog(model: AppModel, d: Device, herDataOnItsMachine: Boolean, done: () -> Unit) {
    var eraseHer by remember { mutableStateOf(false) }
    var busy by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    IosAlert(
        onDismissRequest = { if (!busy) done() },
        title = t("Sign out “${d.label}”?", "登出“${d.label}”？"),
        message = t(
            "It can no longer connect" + (if (d.machineId.isNotEmpty()) ", and its own machine is removed" else "") + ". It can log in again with the account and password.",
            "它将无法再连接" + (if (d.machineId.isNotEmpty()) "，它自己的机器也会被移除" else "") + "。之后它可以用账号和密码重新登录。",
        ),
        actions = listOf(
            AlertAction(t("Cancel", "取消"), AlertRole.Cancel, enabled = !busy, onClick = done),
            AlertAction(t("Sign Out", "登出"), AlertRole.Destructive, enabled = !busy) {
                busy = true
                scope.launch {
                    if (model.removeDevice(d, eraseHer)) {
                        model.say(t("“${d.label}” signed out", "已登出“${d.label}”"))
                        done()
                    } else {
                        busy = false
                    }
                }
            },
        ),
        // Data Her left there, from before Aide dropped her: it can go with the machine.
        content = if (!herDataOnItsMachine) null else ({
            Row(
                Modifier.toggleable(value = eraseHer, interactionSource = remember { MutableInteractionSource() }, indication = null, role = Role.Checkbox, onValueChange = { eraseHer = it }),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                RoundCheck(eraseHer)
                HSpace(10)
                Text(t("Also erase Her’s data on its machine", "同时清除它机器上 Her 的数据"), style = MaterialTheme.typography.bodyMedium)
            }
        }),
    )
}
