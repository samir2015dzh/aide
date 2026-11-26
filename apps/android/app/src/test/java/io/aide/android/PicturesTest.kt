package io.aide.android

import io.aide.android.api.AgentMessage
import io.aide.android.api.AttachmentInfo
import io.aide.android.api.SessionEvent
import io.aide.android.api.TurnCompleted
import io.aide.android.model.Item
import io.aide.android.model.MdInline
import io.aide.android.model.Transcript
import io.aide.android.model.isLocalPicture
import io.aide.android.model.parseInlineMarkdown
import io.aide.android.model.replyPictures
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** 工单 #156 / #158: the pictures a reply shows, as the desktop's (session/pictures.ts) and the machine's (shown.go). */
class PicturesTest {
    @Test fun aReplysPicturesAreFilesOnTheMachineNeverTheWeb() {
        for (src in listOf("/tmp/a.png", "shots/b.png", "file:///tmp/e.png", "C:\\shots\\w.png")) assertTrue(src, isLocalPicture(src))
        for (src in listOf("https://example.com/x.png", "data:image/png;base64,AAAA", "//example.com/x.png", "")) assertFalse(src, isLocalPicture(src))
    }

    @Test fun eachPictureOnceByWhatItsTurnKept() {
        val shot = AttachmentInfo(name = "a.png", path = "/tmp/a.png", sha256 = "aa", image = true)
        val same = AttachmentInfo(name = "copy.png", path = "/tmp/copy.png", sha256 = "aa", image = true)
        val text = "![one](/tmp/a.png) again ![two](/tmp/a.png) ![copy](/tmp/copy.png) ![b](<shots/b c.png> \"t\") ![w](https://example.com/x.png)"
        val kept = replyPictures(text, mapOf(shot.path to shot, same.path to same))
        assertEquals(listOf("aa", "shots/b c.png"), kept.map { it.key })
        assertEquals("one", kept[0].alt)
        // Before its turn kept them: by path.
        assertEquals(listOf("/tmp/a.png", "/tmp/copy.png", "shots/b c.png"), replyPictures(text, emptyMap()).map { it.key })
    }

    @Test fun inTheTextAPictureIsItsWordsAndOneOfTheWebStaysALink() {
        assertEquals(listOf(MdInline.Text("看 "), MdInline.Picture("界面", "/tmp/a.png"), MdInline.Text(" 完")), parseInlineMarkdown("看 ![界面](/tmp/a.png) 完"))
        assertEquals(listOf(MdInline.Text("!"), MdInline.Link("x", "https://example.com/x.png")), parseInlineMarkdown("![x](https://example.com/x.png)"))
        val bangs = "![".repeat(5000)
        assertEquals(listOf(MdInline.Text(bangs)), parseInlineMarkdown(bangs))
    }

    @Test fun aTurnsRepliesGetWhatItKeptWhenItEnds() {
        val shot = AttachmentInfo(name = "a.png", path = "/tmp/a.png", sha256 = "aa", image = true)
        val t = Transcript()
        t.add(SessionEvent(seq = 1, turnId = "1", agentMessage = AgentMessage("![a](/tmp/a.png)")))
        assertEquals(emptyMap<String, AttachmentInfo>(), (t.items()[0] as Item.Agent).kept)
        t.add(SessionEvent(seq = 2, turnId = "1", turnCompleted = TurnCompleted(shown = listOf(shot))))
        assertEquals(shot, (t.items()[0] as Item.Agent).kept["/tmp/a.png"])
    }
}
