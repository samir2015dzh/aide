package io.aide.android.model

import io.aide.android.api.AttachmentInfo

// The pictures an agent's reply shows (工单 #158), as the desktop's (session/pictures.ts): ![alt](a file on the
// machine), each once, by the SHA-256 its turn kept it under, else by its path; never a web address or inline data.

/** A picture a reply shows: as the reply wrote it, and what its turn kept of it (null: not kept yet, or never). */
data class ReplyPicture(val alt: String, val path: String, val kept: AttachmentInfo?) {
    val key: String get() = kept?.sha256?.ifEmpty { null } ?: path
}

// A Markdown image, ![alt](path "title"), the path maybe in <>: as the machine reads them (internal/machine/shown.go).
private val image = Regex("""!\[([^\]\n]*)]\(\s*(<[^>\n]+>|[^)\s]+)(?:\s+"[^"\n]*")?\s*\)""")

/** A file on the machine (a path, a file: URL, a Windows drive path), not a web address or inline data. */
fun isLocalPicture(src: String): Boolean {
    if (src.startsWith("file:", ignoreCase = true) || Regex("""^[A-Za-z]:[\\/]""").containsMatchIn(src)) return true
    return src.isNotEmpty() && !src.startsWith("//") && !Regex("""^[a-zA-Z][a-zA-Z0-9+.-]*:""").containsMatchIn(src)
}

/** The pictures of a reply in the order it shows them, each once. */
fun replyPictures(text: String, kept: Map<String, AttachmentInfo>): List<ReplyPicture> {
    if (!text.contains("![")) return emptyList()
    val out = LinkedHashMap<String, ReplyPicture>()
    for (m in image.findAll(text)) {
        val path = m.groupValues[2].removePrefix("<").removeSuffix(">")
        if (!isLocalPicture(path)) continue
        val p = ReplyPicture(m.groupValues[1], path, kept[path])
        out.putIfAbsent(p.key, p)
    }
    return out.values.toList()
}
