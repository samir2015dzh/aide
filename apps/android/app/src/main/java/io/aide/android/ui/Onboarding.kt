package io.aide.android.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import io.aide.android.model.AppModel
import io.aide.android.model.AppModel.Join
import io.aide.android.model.ConnectAddress
import io.aide.android.model.serverURL
import kotlinx.coroutines.launch

/**
 * Making this phone a trusted device: logging in with the owner's account
 * and password (the default; docs/password-login.md), joining an owner who
 * has one already (their connection address; another trusted device
 * approves, and two digits confirm it), or claiming a new server of one's
 * own with its claim code. Each makes a root key in this phone's Keystore.
 * Each starts with the owner's own server, typed: no build names one and
 * there is no official service to fall back to.
 */
@Composable
fun Onboarding(model: AppModel, snackbar: SnackbarHostState) {
    val join by model.join.collectAsState()
    val world by model.world.collectAsState()
    Scaffold(snackbarHost = { SnackbarHost(snackbar) }) { pad ->
        Column(
            Modifier.fillMaxSize().padding(pad).padding(24.dp).verticalScroll(rememberScrollState()),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            Spacer(Modifier.height(24.dp))
            Text("Aide", style = MaterialTheme.typography.displaySmall, fontWeight = FontWeight.SemiBold, color = MaterialTheme.colorScheme.primary)
            if (world.status.removed) {
                Text(t("This phone was removed from its owner’s trusted devices.", "这台手机已被移出可信设备。"), color = MaterialTheme.colorScheme.error)
            }
            when (val j = join) {
                is Join.Waiting -> Waiting(model, j)
                is Join.Digits -> Digits(model, j)
                Join.Asking -> {
                    Text(t("Asking to join…", "正在请求加入…"))
                    CircularProgressIndicator()
                }
                else -> Start(model, (j as? Join.Failed)?.message)
            }
        }
    }
}

/** How this phone becomes a trusted device: the owner's account and password (the default), a connection address, or a new server's claim code. */
private enum class Way { Password, Join, Claim }

@Composable
private fun Start(model: AppModel, failure: String?) {
    var way by rememberSaveable { mutableStateOf(Way.Password) }
    var address by rememberSaveable { mutableStateOf("") }
    var claim by rememberSaveable { mutableStateOf("") }
    var account by rememberSaveable { mutableStateOf("") }
    var password by rememberSaveable { mutableStateOf("") }
    var error by rememberSaveable { mutableStateOf<String?>(null) }
    var busy by rememberSaveable { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val claiming = way == Way.Claim
    Text(
        when (way) {
            Way.Password -> t("Log in with your Aide account and password (set on your computer: Settings › Account & Devices). No other device needs to approve.",
                "用你的 Aide 账号和密码登录（在电脑上的 设置 › 账号与设备 里设定），不需要别的设备批准。")
            Way.Claim -> t("Claim a new server of your own: this phone becomes your first trusted device.", "认领一台新的自建服务器：这台手机将成为你的第一台可信设备。")
            Way.Join -> t("Join as a trusted device: enter your connection address. One of your trusted devices approves this phone.", "以可信设备加入：输入你的连接地址，由你已有的一台可信设备批准这台手机。")
        },
    )
    OutlinedTextField(
        value = address, onValueChange = { address = it; error = null },
        label = { Text(if (way == Way.Join) t("Connection address", "连接地址") else t("Server address", "服务器地址")) },
        placeholder = { Text(if (way == Way.Join) "mesh.example.com/o_…" else "mesh.example.com") },
        singleLine = true, modifier = Modifier.fillMaxWidth(),
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
    )
    if (way == Way.Password) {
        OutlinedTextField(value = account, onValueChange = { account = it; error = null }, label = { Text(t("Account", "账号")) }, singleLine = true, modifier = Modifier.fillMaxWidth())
        OutlinedTextField(
            value = password, onValueChange = { password = it; error = null }, label = { Text(t("Password", "密码")) }, singleLine = true,
            modifier = Modifier.fillMaxWidth(), visualTransformation = PasswordVisualTransformation(),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
        )
    }
    if (claiming) {
        OutlinedTextField(value = claim, onValueChange = { claim = it; error = null }, label = { Text(t("Claim code", "认领码")) }, singleLine = true, modifier = Modifier.fillMaxWidth())
    }
    if (address.trim().startsWith("http://") || address.trim().matches(Regex("""\d{1,3}(\.\d{1,3}){3}.*"""))) {
        Text(
            t("Not encrypted (HTTP). Sessions stay end-to-end encrypted, but the claim code and which machines are online travel in the clear.",
                "未加密（HTTP）。会话仍是端到端加密的，但认领码与哪些机器在线会明文传输。"),
            style = MaterialTheme.typography.bodySmall, color = Warning,
        )
    }
    (error ?: failure)?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
    Button(
        onClick = {
            when (way) {
                Way.Password -> {
                    val server = serverURL(address) ?: return@Button run { error = t("That isn’t a server address.", "这不是服务器地址。") }
                    busy = true
                    scope.launch {
                        error = model.passwordLogin(server, account, password)
                        busy = false
                    }
                }
                Way.Claim -> {
                    val server = serverURL(address) ?: return@Button run { error = t("That isn’t a server address.", "这不是服务器地址。") }
                    busy = true
                    scope.launch {
                        error = model.claim(server, claim)
                        busy = false
                    }
                }
                Way.Join -> {
                    val a = ConnectAddress.parse(address) ?: return@Button run {
                        error = t("That isn’t a connection address. It looks like relay.example.com/o_…", "这不是连接地址，形如 relay.example.com/o_…")
                    }
                    model.join(a)
                }
            }
        },
        enabled = !busy && when (way) {
            Way.Password -> address.isNotBlank() && account.isNotBlank() && password.isNotEmpty()
            Way.Claim -> address.isNotBlank() && claim.isNotBlank()
            Way.Join -> address.isNotBlank()
        },
        modifier = Modifier.fillMaxWidth(),
    ) { Text(if (way == Way.Password) t("Log In", "登录") else t("Continue", "继续")) }
    if (busy) CircularProgressIndicator()
    for (other in Way.entries.filter { it != way }) {
        TextButton(onClick = { way = other; error = null }) {
            Text(
                when (other) {
                    Way.Password -> t("Log in with account and password instead", "改用账号密码登录")
                    Way.Join -> t("Join with a connection address (another device approves)…", "用连接地址加入（由另一台设备批准）…")
                    Way.Claim -> t("A new server? Enter its claim code…", "新服务器？输入认领码…")
                },
            )
        }
    }
}

@Composable
private fun Waiting(model: AppModel, j: Join.Waiting) {
    Text(t("Approve this phone on one of your trusted devices.", "请在你的一台可信设备上批准这台手机。"), style = MaterialTheme.typography.titleMedium)
    if (j.verificationCode.isNotEmpty()) {
        Text(t("Check that it shows this code:", "确认它显示的是这个验证码："))
        Text(j.verificationCode, fontFamily = FontFamily.Monospace, fontSize = 32.sp, fontWeight = FontWeight.Bold, textAlign = TextAlign.Center, modifier = Modifier.fillMaxWidth())
    }
    Text(t("Pairing code: ", "配对码：") + j.code, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    CircularProgressIndicator()
    TextButton(onClick = model::cancelJoin) { Text(t("Cancel", "取消")) }
}

@Composable
private fun Digits(model: AppModel, j: Join.Digits) {
    var digits by rememberSaveable { mutableStateOf("") }
    Text(
        (if (j.approvedBy.isNotEmpty()) t("Approved on ${j.approvedBy}. ", "已在 ${j.approvedBy} 上批准。") else "") +
            t("Type the two digits it shows.", "请输入它显示的两位数字。"),
        style = MaterialTheme.typography.titleMedium,
    )
    OutlinedTextField(
        value = digits, onValueChange = { if (it.length <= 2) digits = it.filter(Char::isDigit) },
        singleLine = true, keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.NumberPassword),
        label = { Text(t("Digits", "数字")) },
    )
    j.error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
    Button(onClick = { model.confirmDigits(digits) }, enabled = digits.length == 2, modifier = Modifier.fillMaxWidth()) { Text(t("Confirm", "确认")) }
    TextButton(onClick = model::cancelJoin) { Text(t("Cancel", "取消")) }
}
