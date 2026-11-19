package io.aide.android.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import io.aide.android.api.PairingDetails
import io.aide.android.model.AppModel
import io.aide.android.model.PairingLink
import io.aide.android.signer.Card as SignCard
import kotlinx.coroutines.launch

/** Requests to connect that wait for this phone: machines, agents and trusted devices. */
@Composable
fun ApprovalsTab(model: AppModel, open: (String) -> Unit) {
    val world by model.world.collectAsState()
    val dismissed by model.dismissed.collectAsState()
    val pairings = world.waitingPairings.filter { it.code !in dismissed }
    var code by remember { mutableStateOf("") }
    val scope = rememberCoroutineScope()
    Column(Modifier.fillMaxSize()) {
        PageTitle(t("Needs you", "需要你"))
        SyncLine(model)
        Row(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            OutlinedTextField(code, { code = it }, label = { Text(t("Pairing code", "配对码")) }, placeholder = { Text("ABCD-EFGH-JKLM") }, singleLine = true, modifier = Modifier.weight(1f))
            OutlinedButton(onClick = {
                val c = PairingLink.code(code) ?: return@OutlinedButton model.say(t("That isn’t a pairing code.", "这不是配对码。"))
                scope.launch { if (model.lookup(c) != null) code = "" }
            }, modifier = Modifier.padding(top = 8.dp)) { Text(t("Look up", "查找")) }
        }
        LazyColumn(Modifier.fillMaxSize(), contentPadding = androidx.compose.foundation.layout.PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            // Sessions waiting for an answer or an approval: open one to answer it there.
            val sessions = world.buckets.needsYou
            if (sessions.isNotEmpty()) {
                item(key = "h-sessions") { SectionHeader(t("Tasks waiting for you", "等你处理的任务") + " · ${sessions.size}") }
                items(sessions, key = { it.sessionId }) { s ->
                    Card(Modifier.fillMaxWidth()) { SessionRow(s, world) { open(s.sessionId) } }
                }
            }
            if (pairings.isNotEmpty()) {
                item(key = "h-pairings") { SectionHeader(t("Requests to join", "接入请求") + " · ${pairings.size}") }
            }
            if (pairings.isEmpty() && sessions.isEmpty()) {
                item { Text(t("Nothing waits for you.", "没有等你处理的事。"), color = MaterialTheme.colorScheme.onSurfaceVariant) }
            }
            items(pairings, key = { it.code }) { PairingCard(model, it) }
        }
    }
}

private fun kindText(kind: String) = when (kind) {
    "PAIRING_KIND_MACHINE" -> t("A machine asks to join", "一台机器请求接入")
    "PAIRING_KIND_CLIENT" -> t("An agent asks to connect", "一个 Agent 请求连接")
    "PAIRING_KIND_DEVICE" -> t("A device asks to be trusted", "一台设备请求成为可信设备")
    else -> kind
}

@Composable
private fun PairingCard(model: AppModel, p: PairingDetails) {
    var approving by remember { mutableStateOf(false) }
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(kindText(p.kind), style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.primary)
            Text(p.label.ifEmpty { p.hostname }, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
            if (p.hostname.isNotEmpty() && p.hostname != p.label) Labeled(t("Host", "主机"), p.hostname)
            if (p.platform.isNotEmpty()) Labeled(t("Platform", "平台"), p.platform)
            if (p.custodian.isNotEmpty()) Labeled(t("Key held by", "密钥托管方"), p.custodian)
            if (p.expiresAt.isNotEmpty()) Labeled(t("Expires", "过期"), p.expiresAt.take(16).replace('T', ' '))
            if (p.verificationCode.isNotEmpty() && !p.confirmed) {
                Text(t("Approve only if it shows this code:", "只有在对方显示相同验证码时才批准："), style = MaterialTheme.typography.bodySmall)
                Text(p.verificationCode, fontFamily = FontFamily.Monospace, fontSize = 24.sp, fontWeight = FontWeight.Bold)
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Button(onClick = { approving = true }) { Text(t("Approve…", "批准…")) }
                TextButton(onClick = { model.dismiss(p) }) { Text(t("Dismiss", "忽略")) }
            }
        }
    }
    // The two digits to type there, when it asked for them, come from the signer (MatchDialog).
    if (approving) ApproveDialog(model, p, done = { approving = false })
}

/** What approving needs: a machine's name, an agent's name and how long it may work. Then the signer asks to confirm it is you. */
@Composable
private fun ApproveDialog(model: AppModel, p: PairingDetails, done: () -> Unit) {
    var name by remember { mutableStateOf(p.label.ifEmpty { p.hostname }) }
    var ttl by remember { mutableIntStateOf(30) }
    var busy by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    AlertDialog(
        onDismissRequest = { if (!busy) done() },
        title = { Text(kindText(p.kind)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                when (p.kind) {
                    "PAIRING_KIND_MACHINE" -> OutlinedTextField(name, { name = it }, label = { Text(t("Machine name", "机器名称")) }, singleLine = true)
                    "PAIRING_KIND_CLIENT" -> {
                        OutlinedTextField(name, { name = it }, label = { Text(t("Agent name", "Agent 名称")) }, singleLine = true)
                        Text(t("It may use every project and coding agent, for:", "它可以使用全部项目与编码 Agent，期限："))
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            listOf(7, 30, 90).forEach { d -> FilterChip(selected = ttl == d, onClick = { ttl = d }, label = { Text(t("$d days", "$d 天")) }) }
                        }
                    }
                    else -> Text(t("It becomes a trusted device: it can approve and sign as this phone does.", "它将成为可信设备，可以像这台手机一样批准与签署。"))
                }
            }
        },
        confirmButton = {
            Button(enabled = !busy && (p.kind == "PAIRING_KIND_DEVICE" || name.isNotBlank()), onClick = {
                busy = true
                scope.launch {
                    when (p.kind) {
                        "PAIRING_KIND_MACHINE" -> if (model.approveMachine(p, name.trim()) != null) done() else busy = false
                        "PAIRING_KIND_CLIENT" -> if (model.approveClient(p, name.trim(), ttl)) done() else busy = false
                        else -> if (model.approveDevice(p) != null) done() else busy = false
                    }
                }
            }) { Text(t("Approve", "批准")) }
        },
        dismissButton = { TextButton(enabled = !busy, onClick = done) { Text(t("Cancel", "取消")) } },
    )
}

/** A batch the core asks this phone to sign, as the shared core words it: approve, then confirm it is you. */
@Composable
fun SigningSheet(model: AppModel) {
    val pending by model.signer.pending.collectAsState()
    val p = pending ?: return
    AlertDialog(
        onDismissRequest = {},
        title = { Text(t("Approve with this phone’s key", "用这台手机的密钥批准")) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                p.cards.forEach { CardView(it) }
            }
        },
        confirmButton = { Button(onClick = { p.answer.complete(true) }) { Text(t("Approve", "批准")) } },
        dismissButton = { TextButton(onClick = { p.answer.complete(false) }) { Text(t("Deny", "拒绝")) } },
    )
}

@Composable
private fun CardView(c: SignCard) {
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Text(c.title, fontWeight = FontWeight.SemiBold)
        if (c.code.isNotEmpty()) {
            Text(t("Verification code", "验证码"), style = MaterialTheme.typography.labelSmall)
            Text(c.code, fontFamily = FontFamily.Monospace, fontSize = 22.sp, fontWeight = FontWeight.Bold)
        }
        c.lines.forEach { Text(it, style = MaterialTheme.typography.bodySmall) }
        if (c.why.isNotEmpty()) Text(c.why, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        if (c.warning.isNotEmpty()) Text(c.warning, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
    }
}

/** The two digits to type on the device or machine a batch just admitted. */
@Composable
fun MatchDialog(model: AppModel) {
    val match by model.signer.match.collectAsState()
    val m = match ?: return
    AlertDialog(
        onDismissRequest = model.signer::clearMatch,
        title = { Text(if (m.label.isNotEmpty()) t("Type these digits on ${m.label}", "请在 ${m.label} 上输入") else t("Type these digits there", "请在对方输入这两位数字")) },
        text = { Text(m.digits, fontFamily = FontFamily.Monospace, fontSize = 40.sp, textAlign = TextAlign.Center, modifier = Modifier.fillMaxWidth()) },
        confirmButton = { TextButton(onClick = model.signer::clearMatch) { Text(t("Done", "完成")) } },
    )
}
