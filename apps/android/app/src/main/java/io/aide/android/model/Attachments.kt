package io.aide.android.model

import android.graphics.BitmapFactory
import android.util.Base64
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import io.aide.android.api.ReadAttachmentResponse
import io.aide.android.api.Session
import io.aide.android.api.SessionStatus
import io.aide.android.ui.t
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.sync.Semaphore
import kotlinx.coroutines.sync.withPermit
import kotlinx.coroutines.withContext

// What goes with a message (工单 #91), as the desktop's (packages/core attachments.ts): images, audio and any other
// file. The machine takes at most 20 files and 32 MiB per message, saves each beside the session and lists its path in
// the message; PNG, JPEG, GIF and WebP up to 3.5 MiB are also shown to the agent as images. What does not fit is said,
// by kind and name, before anything is sent.

const val MAX_ATTACHMENTS = 20
const val MAX_ATTACHMENT_BYTES = 32L shl 20
const val MAX_INLINE_IMAGE = 3584L shl 10
private val inlineImages = setOf("image/png", "image/jpeg", "image/gif", "image/webp")
private val audioExt = Regex("""\.(mp3|wav|m4a|aac|ogg|oga|opus|flac|amr|wma|aiff?|caf|3gp)$""", RegexOption.IGNORE_CASE)
private val imageExt = Regex("""\.(png|jpe?g|gif|webp|heic|heif|bmp|tiff?|svg|avif)$""", RegexOption.IGNORE_CASE)

enum class AttachKind { IMAGE, AUDIO, FILE }

/** A file about to go with a message: what the phone knows of it before reading it. */
data class PendingFile(val name: String, val type: String, val size: Long)

fun attachKind(f: PendingFile): AttachKind = when {
    f.type.startsWith("image/") || (f.type.isEmpty() && imageExt.containsMatchIn(f.name)) -> AttachKind.IMAGE
    f.type.startsWith("audio/") || (f.type.isEmpty() && audioExt.containsMatchIn(f.name)) -> AttachKind.AUDIO
    else -> AttachKind.FILE
}

fun kindWord(k: AttachKind) = when (k) {
    AttachKind.IMAGE -> t("Image", "图片")
    AttachKind.AUDIO -> t("Audio", "音频")
    AttachKind.FILE -> t("File", "文件")
}

/** A size as people read it: 820 B, 12 KB, 3.4 MB. */
fun sizeText(n: Long): String = when {
    n >= 1L shl 20 -> String.format(java.util.Locale.ROOT, "%.1f", n / (1 shl 20).toDouble()).removeSuffix(".0") + " MB"
    n >= 1L shl 10 -> "${Math.round(n / 1024.0)} KB"
    else -> "$n B"
}

/** How the agent gets it, when that is not what the owner might think (an image it is not shown as one). */
fun attachmentNote(f: PendingFile): String = when {
    attachKind(f) != AttachKind.IMAGE || (f.type in inlineImages && f.size <= MAX_INLINE_IMAGE) -> ""
    f.type in inlineImages -> t("Over 3.5 MB: it gets the file, not the picture to look at", "超过 3.5 MB：它会收到这个文件，但不会直接当图片看")
    else -> t("In this format it gets the file, not the picture to look at", "这种格式的图片：它会收到这个文件，但不会直接当图片看")
}

/** Which of the new files fit with those already there; why each other does not, in words naming its kind. */
fun <T> admitAttachments(have: List<T>, add: List<T>, of: (T) -> PendingFile): Pair<List<T>, List<String>> {
    val take = mutableListOf<T>()
    val refused = mutableListOf<String>()
    var count = have.size
    var bytes = have.sumOf { of(it).size }
    for (x in add) {
        val f = of(x)
        val kind = kindWord(attachKind(f))
        when {
            count >= MAX_ATTACHMENTS -> refused += t("$kind “${f.name}” was not added: a message takes at most $MAX_ATTACHMENTS attachments", "$kind「${f.name}」没加上：一条消息最多带 $MAX_ATTACHMENTS 个附件")
            bytes + f.size > MAX_ATTACHMENT_BYTES -> refused += t("$kind “${f.name}” was not added: a message’s attachments add up to 32 MB at most", "$kind「${f.name}」没加上：一条消息的附件合计最多 32 MB")
            else -> { take += x; count++; bytes += f.size }
        }
    }
    return take to refused
}

/** A project's sessions, as the session page's switcher shows them (工单 #90). */
data class ProjectSessions(val projectId: String, val sessions: List<Session>)

/**
 * The sessions beside an open one (工单 #90, as the desktop's sessionsByProject): the same coding agent's on the same
 * machine, by project — the project with the latest activity first, each newest first — archived ones left out but the
 * open one.
 */
fun sessionsByProject(open: Session, sessions: Collection<Session>): List<ProjectSessions> {
    val by = linkedMapOf<String, MutableList<Session>>()
    for (s in sessions) {
        if (s.machineId != open.machineId || s.agent != open.agent) continue
        if (s.status == SessionStatus.ARCHIVED && s.sessionId != open.sessionId) continue
        by.getOrPut(s.projectId) { mutableListOf() } += s
    }
    if (open.projectId !in by) by[open.projectId] = mutableListOf(open)
    return by.map { (p, list) -> ProjectSessions(p, list.sortedByDescending { instant(it.updatedAt) }) }
        .sortedWith(compareByDescending<ProjectSessions> { instant(it.sessions.first().updatedAt) }.thenBy { it.projectId })
}

/**
 * Pictures of a session, by their SHA-256 (工单 #143, #156, #158): one this phone sent is here at once; any other
 * (sent from another device or before the app started, or shown by one of the agent's replies) the machine gives back
 * (ReadAttachment) when it is first drawn, and it stays while the app runs. A picture is kept at most [MAX_SIDE] pixels
 * a side, enough for its thumbnail and for it full screen.
 */
object SentPictures {
    private const val MOST = 60
    private const val MAX_SIDE = 1600
    private val kept = LinkedHashMap<String, ImageBitmap>()
    private val sizes = HashMap<String, Long>()
    /** At most this many are asked for at once, so that large ones never hold up a session's events on a slow link. */
    private val asking = Semaphore(3)

    fun sha256(bytes: ByteArray): String =
        java.security.MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") { "%02x".format(it) }

    @Synchronized fun keep(bytes: ByteArray, thumb: ImageBitmap) = keep(sha256(bytes), thumb, bytes.size.toLong())

    @Synchronized private fun keep(key: String, pic: ImageBitmap, size: Long) {
        kept.remove(key)
        kept[key] = pic
        sizes[key] = size
        while (kept.size > MOST) kept.keys.first().let { kept.remove(it); sizes.remove(it) }
    }

    @Synchronized fun of(sha: String): ImageBitmap? = kept[sha]

    /** How large the file is, when it was fetched or sent here. */
    @Synchronized fun sizeOf(key: String): Long = sizes[key] ?: 0

    /** The key a picture is kept under: its SHA-256, else (one a reply showed that no turn kept yet) its session and path. */
    fun key(sessionId: String, sha: String, path: String) = sha.ifEmpty { "$sessionId\u0000$path" }

    /** A picture of the session, fetched with read when it is not here yet; null when the machine has none (or can't say). */
    suspend fun fetch(sessionId: String, sha: String, path: String, read: suspend (String, String, String) -> ReadAttachmentResponse?): ImageBitmap? {
        val key = key(sessionId, sha, path)
        of(key)?.let { return it }
        return asking.withPermit {
            of(key) ?: read(sessionId, sha, path)?.let { res ->
                withContext(Dispatchers.Default) {
                    val bytes = runCatching { Base64.decode(res.data, Base64.DEFAULT) }.getOrNull() ?: return@withContext null
                    decode(bytes)?.also { pic ->
                        keep(key, pic, bytes.size.toLong())
                        if (sha.isEmpty() && res.sha256.isNotEmpty()) keep(res.sha256, pic, bytes.size.toLong())
                    }
                }
            }
        }
    }

    private fun decode(bytes: ByteArray): ImageBitmap? = runCatching {
        val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        BitmapFactory.decodeByteArray(bytes, 0, bytes.size, bounds)
        var sample = 1
        while (maxOf(bounds.outWidth, bounds.outHeight) / (sample * 2) >= MAX_SIDE) sample *= 2
        BitmapFactory.decodeByteArray(bytes, 0, bytes.size, BitmapFactory.Options().apply { inSampleSize = sample })?.asImageBitmap()
    }.getOrNull()
}
