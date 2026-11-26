package io.aide.android.model

import io.aide.android.api.Session
import io.aide.android.api.SessionStatus
import io.aide.android.ui.t

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
 * Pictures this phone sent (工单 #143), by their SHA-256: a message's bubble shows each as its thumbnail. Only those
 * sent from here while the app runs: the machine has no way to give a file back yet, so one sent elsewhere shows by name.
 */
object SentPictures {
    private const val MOST = 60
    private val kept = LinkedHashMap<String, androidx.compose.ui.graphics.ImageBitmap>()

    fun sha256(bytes: ByteArray): String =
        java.security.MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") { "%02x".format(it) }

    @Synchronized fun keep(bytes: ByteArray, thumb: androidx.compose.ui.graphics.ImageBitmap) {
        kept[sha256(bytes)] = thumb
        while (kept.size > MOST) kept.remove(kept.keys.first())
    }

    @Synchronized fun of(sha: String): androidx.compose.ui.graphics.ImageBitmap? = kept[sha]
}
