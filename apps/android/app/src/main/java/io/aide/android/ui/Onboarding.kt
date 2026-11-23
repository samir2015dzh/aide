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
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import io.aide.android.model.AppModel
import io.aide.android.model.MIN_PASSWORD
import io.aide.android.model.newLoginProblem
import io.aide.android.model.plainHttp
import io.aide.android.model.serverURL
import kotlinx.coroutines.launch

/**
 * Making this phone one of the owner's devices (docs/accounts.md), two
 * ways: logging in to the owner's server with the account and password,
 * or setting a new server up with the setup code it printed and the
 * account and password it will keep. Either way the server signs this
 * phone in at once: no other device approves, nothing is compared. Each
 * starts with the owner's own server, typed: no build names one.
 */
@Composable
fun Onboarding(model: AppModel, snackbar: SnackbarHostState) {
    val world by model.world.collectAsState()
    Scaffold(snackbarHost = { IosSnackbarHost(snackbar) }) { pad ->
        Column(
            Modifier.fillMaxSize().padding(pad).padding(horizontal = 20.dp, vertical = 24.dp).verticalScroll(rememberScrollState()),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            Spacer(Modifier.height(24.dp))
            Text("Aide", style = MaterialTheme.typography.displaySmall)
            if (world.status.removed) {
                Text(t("This phone was signed out by another of your devices. Log in again to use it.", "这台手机已被你的另一台设备登出。重新登录即可继续使用。"), color = MaterialTheme.colorScheme.error)
            }
            Start(model)
        }
    }
}

/** What a new password needs (relay.CheckNewLogin), under its field. */
val passwordHint: @Composable () -> Unit = {
    Text(t("At least $MIN_PASSWORD characters, not the account name.", "至少 $MIN_PASSWORD 个字符，不能和账号相同。"))
}

/** The two ways in: log in to a server, or set a new one up. */
private enum class Way { LogIn, SetUp }

@Composable
private fun Start(model: AppModel) {
    var way by rememberSaveable { mutableStateOf(Way.LogIn) }
    var address by rememberSaveable { mutableStateOf("") }
    var setupCode by rememberSaveable { mutableStateOf("") }
    var account by rememberSaveable { mutableStateOf("") }
    // Not saved: a password is not kept past the screen.
    var password by remember { mutableStateOf("") }
    var error by rememberSaveable { mutableStateOf<String?>(null) }
    var busy by rememberSaveable { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val setUp = way == Way.SetUp
    Text(if (setUp) t("Set up a new server", "设置新的服务器") else t("Log in", "登录"), style = MaterialTheme.typography.headlineSmall)
    Text(
        if (setUp) {
            t(
                "A new server prints a setup code when it first starts. Type it, and the account and password the server will keep: you log in with them on every device.",
                "新的服务器第一次启动时会显示一个设置码。输入设置码，再设定服务器要保存的账号和密码：以后每台设备都用它们登录。",
            )
        } else {
            t("Log in with your server, account and password. No other device needs to approve.", "用你的服务器、账号和密码登录，不需要别的设备批准。")
        },
        style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
    IosField(
        value = address, onValueChange = { address = it; error = null },
        label = t("Server", "服务器"), placeholder = "aide.example.com", modifier = Modifier.fillMaxWidth(),
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
    )
    if (setUp) {
        IosField(value = setupCode, onValueChange = { setupCode = it; error = null }, label = t("Setup code", "设置码"), modifier = Modifier.fillMaxWidth())
    }
    IosField(
        value = account, onValueChange = { account = it; error = null }, label = t("Account", "账号"),
        modifier = Modifier.fillMaxWidth(), keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Email),
    )
    IosField(
        value = password, onValueChange = { password = it; error = null }, label = t("Password", "密码"),
        modifier = Modifier.fillMaxWidth(), visualTransformation = PasswordVisualTransformation(),
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
        supportingText = passwordHint.takeIf { setUp },
    )
    if (plainHttp(address)) {
        Text(
            t(
                "Not encrypted (HTTP): your password and setup code travel readable on the network. Sessions stay end-to-end encrypted.",
                "未加密（HTTP）：密码和设置码会在网络上明文传输。会话仍是端到端加密的。",
            ),
            style = MaterialTheme.typography.bodySmall, color = Warning,
        )
    }
    error?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
    FilledButton(
        large = true,
        onClick = {
            val server = serverURL(address) ?: return@FilledButton run { error = t("That isn’t a server address.", "这不是服务器地址。") }
            if (setUp) newLoginProblem(account, password)?.let { return@FilledButton run { error = it } }
            busy = true
            scope.launch {
                error = if (setUp) model.setUp(server, setupCode, account, password) else model.logIn(server, account, password)
                busy = false
                if (error == null) password = ""
            }
        },
        enabled = !busy && address.isNotBlank() && account.isNotBlank() && password.isNotEmpty() && (!setUp || setupCode.isNotBlank()),
        modifier = Modifier.fillMaxWidth(),
    ) { Text(if (setUp) t("Set Up", "设置") else t("Log In", "登录")) }
    if (busy) ActivityIndicator(size = 24.dp)
    PlainButton(modifier = Modifier.fillMaxWidth(), onClick = { way = if (setUp) Way.LogIn else Way.SetUp; error = null }) {
        Text(if (setUp) t("Log in to a server instead", "改为登录已有的服务器") else t("A new server? Set it up with its setup code…", "新的服务器？用设置码设置新的服务器…"))
    }
}
