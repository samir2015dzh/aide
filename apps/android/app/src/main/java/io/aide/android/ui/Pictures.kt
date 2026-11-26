package io.aide.android.ui

import android.content.ClipData
import android.content.ClipboardManager
import android.content.ContentValues
import android.content.Context
import android.graphics.Bitmap
import android.os.Build
import android.provider.MediaStore
import android.view.WindowManager
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.compose.ui.window.DialogWindowProvider
import io.aide.android.api.ReadAttachmentResponse
import io.aide.android.model.ReplyPicture
import io.aide.android.model.SentPictures
import io.aide.android.model.sizeText

// A session's pictures on the phone (工单 #156, #158): the thumbnails of what a message attached, from the machine
// when sent from elsewhere; the pictures a reply shows, under it, each once; full screen over a blurred page on a tap.

/** Where a conversation's pictures come from: its session's machine (none outside a session: only those sent from here show). */
class PictureSource(val sessionId: String, val read: suspend (String, String, String) -> ReadAttachmentResponse?)

val LocalPictureSource = compositionLocalOf<PictureSource?> { null }

/** A picture to draw: null while it comes; gone: the machine can't give it back. */
private class Fetched(val pic: ImageBitmap?, val gone: Boolean)

@Composable
private fun picture(sha: String, path: String): Fetched {
    val src = LocalPictureSource.current
    val key = SentPictures.key(src?.sessionId.orEmpty(), sha, path)
    var got by remember(key) { mutableStateOf(SentPictures.of(key)?.let { Fetched(it, false) } ?: Fetched(null, src == null)) }
    LaunchedEffect(key, src) {
        if (got.pic == null && src != null) got = SentPictures.fetch(src.sessionId, sha, path, src.read).let { Fetched(it, it == null) }
    }
    return got
}

/** An attached picture's thumbnail, opened full screen on a tap; null when there is none to show (the caller shows the file by name). */
@Composable
internal fun SentPicture(sha: String, name: String, size: Long): Boolean {
    val got = picture(sha, "")
    val pic = got.pic ?: return false
    var open by remember { mutableStateOf(false) }
    Image(pic, name, Modifier.size(96.dp).clickable { open = true }, contentScale = ContentScale.Crop)
    if (open) Lightbox(pic, name, size, path = "") { open = false }
    return true
}

/** The pictures a reply shows, under it, in the order it shows them, each once. */
@Composable
internal fun ReplyPictures(pictures: List<ReplyPicture>, modifier: Modifier = Modifier) {
    if (pictures.isEmpty() || LocalPictureSource.current == null) return
    Row(modifier.fillMaxWidth().horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        for (p in pictures) ReplyPictureView(p)
    }
}

@Composable
private fun ReplyPictureView(p: ReplyPicture) {
    val got = picture(p.kept?.sha256.orEmpty(), if (p.kept == null) p.path else "")
    val name = p.kept?.name?.ifEmpty { null } ?: p.path.substringAfterLast('/').substringAfterLast('\\')
    var open by remember { mutableStateOf(false) }
    val shape = RoundedCornerShape(Radius.Lg)
    Column(Modifier.widthIn(max = 260.dp)) {
        val pic = got.pic
        when {
            pic != null -> Image(
                pic, p.alt.ifEmpty { name },
                Modifier.heightIn(max = 220.dp).clip(shape).background(aide.fill2).clickable { open = true },
                contentScale = ContentScale.Fit,
            )
            got.gone -> Text(t("Picture not available now", "现在取不到这张图片") + " · " + name, fontSize = 12.sp, color = aide.faint, modifier = Modifier.clip(shape).background(aide.fill2).padding(10.dp))
            else -> Box(Modifier.size(160.dp, 120.dp).clip(shape).background(aide.fill2))
        }
        if (p.alt.isNotEmpty()) Text(p.alt, fontSize = 11.sp, color = aide.faint, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 2.dp, start = 2.dp))
        if (open && pic != null) Lightbox(pic, name, SentPictures.sizeOf(p.kept?.sha256 ?: ""), p.path) { open = false }
    }
}

/** A picture full screen over the blurred page (blur from Android 12): its name and size on top, a menu to save it to Photos or copy its path; a double tap zooms. */
@Composable
internal fun Lightbox(pic: ImageBitmap, name: String, size: Long, path: String, close: () -> Unit) {
    val context = LocalContext.current
    Dialog(close, DialogProperties(usePlatformDefaultWidth = false, decorFitsSystemWindows = false)) {
        val view = LocalView.current
        LaunchedEffect(Unit) {
            (view.parent as? DialogWindowProvider)?.window?.let { w ->
                w.setDimAmount(0.6f)
                if (Build.VERSION.SDK_INT >= 31) {
                    w.addFlags(WindowManager.LayoutParams.FLAG_BLUR_BEHIND)
                    w.attributes = w.attributes.apply { blurBehindRadius = 64 }
                }
            }
        }
        var zoom by remember { mutableStateOf(false) }
        var menu by remember { mutableStateOf(false) }
        Box(Modifier.fillMaxSize().background(Color.Black.copy(alpha = 0.35f)).pointerInput(Unit) { detectTapGestures(onTap = { close() }) }) {
            Image(
                pic, name,
                Modifier.align(Alignment.Center).fillMaxWidth().padding(16.dp)
                    .graphicsLayer { val s = if (zoom) 2.4f else 1f; scaleX = s; scaleY = s }
                    .pointerInput(Unit) { detectTapGestures(onDoubleTap = { zoom = !zoom }) },
                contentScale = ContentScale.Fit,
            )
            Row(Modifier.fillMaxWidth().statusBarsPadding().padding(horizontal = 4.dp, vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
                IconButton(close) { Icon(Icons.Filled.Close, t("Close", "关闭"), tint = Color.White) }
                Column(Modifier.weight(1f), horizontalAlignment = Alignment.CenterHorizontally) {
                    Text(name, color = Color.White, style = MaterialTheme.typography.titleSmall, maxLines = 1, overflow = TextOverflow.Ellipsis, textAlign = TextAlign.Center)
                    Text(listOfNotNull("${pic.width}×${pic.height}", size.takeIf { it > 0 }?.let(::sizeText)).joinToString(" · "), color = Color.White.copy(alpha = 0.72f), fontSize = 12.sp)
                }
                Box {
                    IconButton({ menu = true }) { Icon(Icons.Filled.MoreVert, t("More", "更多"), tint = Color.White) }
                    DropdownMenu(menu, { menu = false }) {
                        DropdownMenuItem({ Text(t("Save to Photos", "存储到相册")) }, { menu = false; savePicture(context, pic, name) })
                        if (path.isNotEmpty()) DropdownMenuItem({ Text(t("Copy path", "拷贝路径")) }, {
                            menu = false
                            (context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager).setPrimaryClip(ClipData.newPlainText("path", path))
                        })
                    }
                }
            }
        }
    }
}

/** Saves a picture to Photos (Pictures/Aide), no permission needed from Android 10. */
private fun savePicture(context: Context, pic: ImageBitmap, name: String) {
    runCatching {
        val values = ContentValues().apply {
            put(MediaStore.Images.Media.DISPLAY_NAME, name.substringBeforeLast('.') + ".png")
            put(MediaStore.Images.Media.MIME_TYPE, "image/png")
            put(MediaStore.Images.Media.RELATIVE_PATH, "Pictures/Aide")
        }
        val uri = context.contentResolver.insert(MediaStore.Images.Media.EXTERNAL_CONTENT_URI, values) ?: return
        context.contentResolver.openOutputStream(uri)?.use { pic.asAndroidBitmap().compress(Bitmap.CompressFormat.PNG, 100, it) }
    }
}
