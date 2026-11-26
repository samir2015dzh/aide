package io.aide.android.ui

import kotlinx.coroutines.withTimeoutOrNull
import androidx.compose.material3.SnackbarResult
import androidx.compose.material3.SnackbarDuration
import io.aide.android.model.Notice
import android.content.ActivityNotFoundException
import android.content.ContentResolver
import android.content.Context
import android.content.Intent
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.net.Uri
import android.provider.MediaStore
import android.provider.OpenableColumns
import android.speech.RecognizerIntent
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.KeyboardArrowUp
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.addPathNodes
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.core.content.FileProvider
import io.aide.android.api.AttachmentIn
import io.aide.android.model.AppModel
import io.aide.android.model.AttachKind
import io.aide.android.model.PendingFile
import io.aide.android.model.SentPictures
import io.aide.android.model.admitAttachments
import io.aide.android.model.attachKind
import io.aide.android.model.attachmentNote
import io.aide.android.model.kindWord
import io.aide.android.model.sizeText
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.ByteArrayOutputStream
import java.io.File
import java.util.UUID

// The box to write in (工单 #92, as the desktop's, after Claude's input box; one for a session's page, a bot's page,
// a personal agent's conversation and a room): a rounded box with the line to write in, + at its foot on the left for
// what goes with the message — a photo taken now, pictures, files, audio, a recording (工单 #91) — and the page's own
// items, the microphone and the send button on the right; under it, on the right, the choices the message goes with,
// and on the left what became of the last message (工单 #61). Typing / lists the page's commands when it has some.

sealed interface ComposerState {
    data object Idle : ComposerState
    data object Sending : ComposerState
    data class Done(val text: String) : ComposerState
    data class Failed(val text: String, val retry: (() -> Unit)? = null) : ComposerState
}

data class PlusItem(val label: String, val enabled: Boolean = true, val run: () -> Unit)

/** A command typed after /: its name and what it does. */
data class SlashCommand(val name: String, val label: String, val run: () -> Unit)

/** A line over the box: why it can't send, or what the next message does. */
data class ComposerWhy(val text: String, val warn: Boolean = false, val action: Pair<String, () -> Unit>? = null)

/** A file about to go with the message, read when it was added. */
class Draft(val name: String, val type: String, val bytes: ByteArray, val thumb: ImageBitmap? = null) {
    val id = UUID.randomUUID().toString()
    val pending get() = PendingFile(name, type, bytes.size.toLong())
    val kind get() = attachKind(pending)
}

/** The files about to go with a message: the page sends them (wire) and clears them once sent. */
class Attachments {
    val files = mutableStateListOf<Draft>()
    /** Why some were not added, in words naming their kind (shown in the box until the next change). */
    var refused by mutableStateOf<List<String>>(emptyList())

    fun add(more: List<Draft>) {
        val (take, refused) = admitAttachments(files, more) { it.pending }
        files += take
        this.refused = refused
    }
    fun refuse(why: String) { refused = listOf(why) }
    fun remove(d: Draft) { files.remove(d); refused = emptyList() }
    fun clear() { files.clear(); refused = emptyList() }
    fun wire(): List<AttachmentIn> = files.map {
        it.thumb?.let { th -> SentPictures.keep(it.bytes, th) } // its bubble shows it (工单 #143)
        AttachmentIn(it.name, it.type, java.util.Base64.getEncoder().encodeToString(it.bytes))
    }
}

@Composable
fun rememberAttachments(key: Any? = null): Attachments = remember(key) { Attachments() }

/** What the phone knows of a picked file before reading it. */
private fun metaOf(cr: ContentResolver, uri: Uri): PendingFile {
    var name = uri.lastPathSegment?.substringAfterLast('/') ?: "file"
    var size = -1L
    cr.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE), null, null, null)?.use { c ->
        if (c.moveToFirst()) {
            c.getString(0)?.let { name = it }
            if (!c.isNull(1)) size = c.getLong(1)
        }
    }
    return PendingFile(name, cr.getType(uri).orEmpty(), size)
}

private fun thumbOf(bytes: ByteArray): ImageBitmap? = runCatching {
    val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
    BitmapFactory.decodeByteArray(bytes, 0, bytes.size, bounds)
    var sample = 1
    while (maxOf(bounds.outWidth, bounds.outHeight) / (sample * 2) >= 160) sample *= 2
    BitmapFactory.decodeByteArray(bytes, 0, bytes.size, BitmapFactory.Options().apply { inSampleSize = sample })?.asImageBitmap()
}.getOrNull()

/** Reads what fits of the picked files (their sizes checked first, so nothing too big is read) into a's list. */
private suspend fun addUris(context: Context, a: Attachments, uris: List<Uri>) {
    val cr = context.contentResolver
    val metas = withContext(Dispatchers.IO) { uris.map { it to metaOf(cr, it) } }
    val (take, refused) = admitAttachments(a.files.map { null to it.pending }, metas.map { it.first to it.second }) { it.second }
    val drafts = withContext(Dispatchers.IO) {
        take.mapNotNull { (uri, f) ->
            val bytes = cr.openInputStream(uri!!)?.use { it.readBytes() } ?: return@mapNotNull null
            Draft(f.name, f.type, bytes, if (attachKind(f) == AttachKind.IMAGE) thumbOf(bytes) else null)
        }
    }
    a.add(drafts)
    if (refused.isNotEmpty()) a.refused = refused + a.refused
}

/** A photo just taken, at most 2048 px on its long side as JPEG, so the agent can look at it (up to 3.5 MB). */
private fun photoOf(file: File): Draft? {
    val full = BitmapFactory.decodeFile(file.path) ?: return null
    val scale = minOf(1f, 2048f / maxOf(full.width, full.height))
    val bmp = if (scale < 1f) Bitmap.createScaledBitmap(full, (full.width * scale).toInt(), (full.height * scale).toInt(), true) else full
    val out = ByteArrayOutputStream()
    bmp.compress(Bitmap.CompressFormat.JPEG, 85, out)
    file.delete()
    val bytes = out.toByteArray()
    return Draft("photo-${System.currentTimeMillis() / 1000}.jpg", "image/jpeg", bytes, thumbOf(bytes))
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
fun Composer(
    value: String,
    onChange: (String) -> Unit,
    onSend: () -> Unit,
    placeholder: String,
    state: ComposerState,
    enabled: Boolean = true,
    why: ComposerWhy? = null,
    plus: List<PlusItem> = emptyList(),
    chips: (@Composable () -> Unit)? = null,
    attach: Attachments? = null,
    commands: List<SlashCommand> = emptyList(),
    mentions: (@Composable () -> Unit)? = null,
) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var hint by remember { mutableStateOf("") }
    val sending = state is ComposerState.Sending
    val files = attach?.files.orEmpty()
    val can = enabled && !sending && (value.isNotBlank() || files.isNotEmpty())
    val slash = Regex("""^/(\S*)$""").find(value)?.groupValues?.get(1)?.lowercase()
    val shown = if (slash == null) emptyList() else commands.filter { it.name.startsWith(slash) || it.label.lowercase().contains(slash) }
    // What + offers (工单 #91, #91 扩充二): a photo taken now, pictures, files, audio, a recording; each read into the box.
    val pickMedia = rememberLauncherForActivityResult(ActivityResultContracts.PickMultipleVisualMedia(MAX_PICK)) { uris -> if (attach != null && uris.isNotEmpty()) scope.launch { addUris(context, attach, uris) } }
    val pickFiles = rememberLauncherForActivityResult(ActivityResultContracts.OpenMultipleDocuments()) { uris -> if (attach != null && uris.isNotEmpty()) scope.launch { addUris(context, attach, uris) } }
    var shot by remember { mutableStateOf<File?>(null) }
    val takePhoto = rememberLauncherForActivityResult(ActivityResultContracts.TakePicture()) { ok ->
        val f = shot
        if (ok && f != null && attach != null) scope.launch { withContext(Dispatchers.IO) { photoOf(f) }?.let { attach.add(listOf(it)) } }
    }
    val record = rememberLauncherForActivityResult(ActivityResultContracts.StartActivityForResult()) { r -> r.data?.data?.let { uri -> if (attach != null) scope.launch { addUris(context, attach, listOf(uri)) } } }
    val listen = rememberLauncherForActivityResult(ActivityResultContracts.StartActivityForResult()) { r ->
        r.data?.getStringArrayListExtra(RecognizerIntent.EXTRA_RESULTS)?.firstOrNull()?.let { said -> onChange(if (value.isBlank()) said else "$value $said") }
    }
    fun tryLaunch(none: String, go: () -> Unit) = try { go(); hint = "" } catch (_: ActivityNotFoundException) { hint = none }
    val items = (if (attach == null) emptyList() else listOf(
        PlusItem(t("Take a photo", "拍照")) {
            tryLaunch(t("This phone has no camera app to call", "这台手机没有可调用的相机")) {
                val f = File(File(context.cacheDir, "camera").apply { mkdirs() }, "shot-${System.currentTimeMillis()}.jpg")
                shot = f
                takePhoto.launch(FileProvider.getUriForFile(context, context.packageName + ".attach", f))
            }
        },
        PlusItem(t("Photos", "相册")) { tryLaunch(t("This phone has no photo picker", "这台手机没有可调用的相册")) { pickMedia.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageAndVideo)) } },
        PlusItem(t("File", "文件")) { tryLaunch(t("This phone has no file picker", "这台手机没有可调用的文件选择器")) { pickFiles.launch(arrayOf("*/*")) } },
        PlusItem(t("Audio", "音频")) { tryLaunch(t("This phone has no file picker", "这台手机没有可调用的文件选择器")) { pickFiles.launch(arrayOf("audio/*")) } },
        PlusItem(t("Record", "录音")) {
            tryLaunch(t("No recorder on this phone answers Aide: pick a recording under Audio instead", "这台手机的录音程序不接受调用：可以先录好，再从「音频」里选")) { record.launch(Intent(MediaStore.Audio.Media.RECORD_SOUND_ACTION)) }
        },
    )) + plus
    Column(Modifier.fillMaxWidth().imePadding().padding(start = 10.dp, end = 10.dp, top = 6.dp, bottom = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        why?.let { w ->
            Row(Modifier.padding(horizontal = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(w.text, style = MaterialTheme.typography.labelMedium, color = if (w.warn) Warning else MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.weight(1f, fill = false))
                w.action?.let { (label, run) ->
                    HSpace(8)
                    Text(label, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.primary, fontWeight = FontWeight.SemiBold, modifier = Modifier.pressable(run))
                }
            }
        }
        // The commands what is typed names, over the box.
        if (shown.isNotEmpty()) {
            Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(Radius.Xl)).background(aide.menu).border(1.dp, aide.cardEdge, RoundedCornerShape(Radius.Xl))) {
                shown.forEach { c ->
                    Row(Modifier.fillMaxWidth().rowPress { onChange(""); c.run() }.padding(horizontal = 16.dp, vertical = 11.dp)) {
                        Text("/" + c.name, fontWeight = FontWeight.SemiBold, modifier = Modifier.widthIn(min = 90.dp))
                        Text(c.label, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                }
            }
        }
        Column(
            Modifier.fillMaxWidth().clip(RoundedCornerShape(20.dp)).background(aide.card).border(1.dp, aide.separator, RoundedCornerShape(20.dp)).padding(start = 6.dp, end = 6.dp, top = 8.dp, bottom = 6.dp),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            mentions?.let {
                FlowRow(Modifier.padding(horizontal = 6.dp), horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) { it() }
            }
            if (files.isNotEmpty() && attach != null) {
                Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = 6.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    files.forEach { d -> FileChip(d) { attach.remove(d) } }
                }
            }
            Box(Modifier.fillMaxWidth().padding(horizontal = 8.dp, vertical = 4.dp)) {
                if (value.isEmpty()) Text(if (commands.isNotEmpty()) t("Type / for commands", "输入 / 以使用命令") else placeholder, style = MaterialTheme.typography.bodyLarge, color = aide.faint, maxLines = 1, overflow = TextOverflow.Ellipsis)
                BasicTextField(
                    value, { onChange(it); hint = "" }, Modifier.fillMaxWidth().heightIn(max = 140.dp).semantics { contentDescription = placeholder }, enabled = enabled,
                    textStyle = MaterialTheme.typography.bodyLarge.copy(color = MaterialTheme.colorScheme.onSurface),
                    cursorBrush = SolidColor(MaterialTheme.colorScheme.primary), maxLines = 6,
                )
            }
            Row(verticalAlignment = Alignment.CenterVertically) {
                if (items.isNotEmpty()) {
                    var open by remember { mutableStateOf(false) }
                    Box {
                        RoundButton(enabled = enabled, filled = false, onClick = { open = true }) { Icon(Icons.Filled.Add, t("More", "更多"), Modifier.size(20.dp)) }
                        IosMenu(open, { open = false }, items.map { p -> MenuEntry(p.label, p.enabled) { open = false; p.run() } })
                    }
                }
                Box(Modifier.weight(1f))
                // The system's speech input (RecognizerIntent): what was said goes into the line.
                RoundButton(enabled = enabled, filled = false, onClick = {
                    tryLaunch(t("This phone has no speech input to call: the keyboard's own voice key works too", "这台手机没有可调用的语音输入：也可以用输入法自带的语音键")) {
                        listen.launch(Intent(RecognizerIntent.ACTION_RECOGNIZE_SPEECH).putExtra(RecognizerIntent.EXTRA_LANGUAGE_MODEL, RecognizerIntent.LANGUAGE_MODEL_FREE_FORM))
                    }
                }) { Icon(MicIcon, t("Dictate", "语音输入"), Modifier.size(20.dp)) }
                HSpace(4)
                RoundButton(enabled = can, filled = true, onClick = onSend) {
                    if (sending) ActivityIndicator(size = 18.dp, color = Color.White)
                    else Icon(Icons.Filled.KeyboardArrowUp, t("Send", "发送"), Modifier.size(22.dp))
                }
            }
        }
        // Under the box: what became of the last message (工单 #61), what was not added and why (工单 #91), on the
        // left; the choices it goes with on the right.
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.weight(1f)) {
                val refused = attach?.refused.orEmpty()
                when {
                    refused.isNotEmpty() -> StateLine(refused.joinToString("\n"), Warning)
                    hint.isNotEmpty() -> StateLine(hint, aide.faint)
                    state == ComposerState.Sending -> StateLine(t("Sending…", "正在发送…"), aide.faint)
                    state is ComposerState.Done -> StateLine(state.text, Positive)
                    state is ComposerState.Failed -> Row(Modifier.padding(horizontal = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                        Text(state.text, style = MaterialTheme.typography.labelMedium, color = Warning, modifier = Modifier.weight(1f, fill = false))
                        state.retry?.let { HSpace(8); Text(t("Retry", "重试"), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.primary, modifier = Modifier.pressable(it)) }
                    }
                    else -> {}
                }
            }
            chips?.let { FlowRow(Modifier.padding(end = 6.dp), horizontalArrangement = Arrangement.spacedBy(6.dp, Alignment.End), verticalArrangement = Arrangement.spacedBy(4.dp)) { it() } }
        }
    }
}

private const val MAX_PICK = 20

/** Material's microphone (the icons this app ships lack it). */
private val MicIcon: ImageVector = ImageVector.Builder("Mic", 24.dp, 24.dp, 24f, 24f).addPath(
    addPathNodes("M12,14c1.66,0 2.99,-1.34 2.99,-3L15,5c0,-1.66 -1.34,-3 -3,-3S9,3.34 9,5v6c0,1.66 1.34,3 3,3zM17.3,11c0,3 -2.54,5.1 -5.3,5.1S6.7,14 6.7,11L5,11c0,3.41 2.72,6.23 6,6.72L11,21h2v-3.28c3.28,-0.48 6,-3.3 6,-6.72h-1.7z"),
    fill = SolidColor(Color.Black),
).build()

/** One file in the box: a picture's thumbnail, else its name, kind and size; × takes it off. */
@Composable
private fun FileChip(d: Draft, remove: () -> Unit) {
    Box(Modifier.height(52.dp).clip(RoundedCornerShape(12.dp)).background(aide.fill2)) {
        if (d.thumb != null) Image(d.thumb, d.name, Modifier.size(52.dp), contentScale = ContentScale.Crop)
        else Column(Modifier.widthIn(max = 200.dp).padding(start = 10.dp, end = 28.dp, top = 8.dp, bottom = 8.dp)) {
            Text(d.name, fontSize = 12.sp, fontWeight = FontWeight.Medium, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text("${kindWord(d.kind)} · ${sizeText(d.bytes.size.toLong())}", fontSize = 11.sp, color = aide.faint, maxLines = 1)
        }
        attachmentNote(d.pending).takeIf { it.isNotEmpty() }?.let { Box(Modifier.align(Alignment.BottomStart).size(6.dp).clip(CircleShape).background(Warning)) }
        Box(Modifier.align(Alignment.TopEnd).padding(3.dp).size(18.dp).clip(CircleShape).background(Color(0x8C000000)).pressable(remove), contentAlignment = Alignment.Center) {
            Icon(Icons.Filled.Close, t("Remove ${d.name}", "移除 ${d.name}"), Modifier.size(12.dp), tint = Color.White)
        }
    }
}

@Composable
private fun StateLine(text: String, color: Color) {
    Text(text, style = MaterialTheme.typography.labelMedium, color = color, modifier = Modifier.padding(horizontal = 12.dp))
}

@Composable
private fun RoundButton(enabled: Boolean, filled: Boolean, onClick: () -> Unit, content: @Composable () -> Unit) {
    val bg = when {
        filled && enabled -> aide.accent
        filled -> aide.fill
        else -> Color.Transparent
    }
    val fg = if (filled && enabled) Color.White else if (enabled) MaterialTheme.colorScheme.onSurfaceVariant else aide.disabled
    Box(
        Modifier.size(34.dp).clip(RoundedCornerShape(Radius.Inner)).background(bg).then(if (enabled) Modifier.pressable(onClick) else Modifier),
        contentAlignment = Alignment.Center,
    ) {
        androidx.compose.runtime.CompositionLocalProvider(androidx.compose.material3.LocalContentColor provides fg) { content() }
    }
}

/**
 * Fluent dropdown selector chip in the composer (工单 #106: 10dp rounded bordered dropdown button).
 */
@Composable
fun <T> ChipPicker(label: String, value: T, options: List<Pair<T, String>>, notes: List<String> = emptyList(), chip: ((T) -> String?)? = null, onChange: (T) -> Unit) {
    var open by remember { mutableStateOf(false) }
    val shape = RoundedCornerShape(Radius.Md)
    Box {
        Row(
            Modifier.heightIn(min = 32.dp).clip(shape).background(aide.card).border(1.dp, aide.controlBorder, shape).pressable { open = true }.padding(start = 10.dp, end = 6.dp, top = 4.dp, bottom = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(chip?.invoke(value) ?: options.firstOrNull { it.first == value }?.second ?: label, style = IosType.Footnote, color = MaterialTheme.colorScheme.onSurface, maxLines = 1)
            Icon(Icons.Filled.KeyboardArrowDown, label, Modifier.size(16.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        IosMenu(open, { open = false }, options.map { (v, l) -> MenuEntry(l, checked = v == value) { open = false; onChange(v) } } + notes.map { MenuEntry(it, enabled = false) {} })
    }
}

/** A chip that is on or off (a room's @): Fluent subtle accent fill when on. */
@Composable
fun ToggleChip(label: String, on: Boolean, onClick: () -> Unit) {
    Text(
        if (on) "✓ $label" else label,
        Modifier.clip(CircleShape).background(if (on) aide.accentSubtle else aide.fill).pressable(onClick).padding(horizontal = 11.dp, vertical = 5.dp),
        style = IosType.Footnote, fontWeight = if (on) FontWeight.SemiBold else FontWeight.Normal,
        color = if (on) aide.blueOnSubtle else MaterialTheme.colorScheme.onSurfaceVariant,
    )
}

/** Shows what is said; one with an action (Undo, 工单 #165) for 5.2 seconds, as the desktop's toast, and runs it when tapped. */
suspend fun SnackbarHostState.showNotice(n: Notice) {
    if (n.action == null || n.run == null) {
        showSnackbar(n.text)
        return
    }
    val r = withTimeoutOrNull(5_200) { showSnackbar(n.text, actionLabel = n.action, duration = SnackbarDuration.Indefinite) }
    if (r == SnackbarResult.ActionPerformed) n.run.invoke()
}

/**
 * A page's own place to say things (工单 #61): what an action on this page says shows here, at the foot of this page,
 * while it is open — not on the tabs underneath once it is closed. Put in its Scaffold's snackbarHost.
 */
@Composable
fun PageFeedback(model: AppModel) {
    val host = remember { SnackbarHostState() }
    val scope = rememberCoroutineScope()
    DisposableEffect(model) {
        val sink: (Notice) -> Unit = { n -> scope.launch { host.showNotice(n) } }
        model.pushSink(sink)
        onDispose { model.popSink(sink) }
    }
    IosSnackbarHost(host)
}
