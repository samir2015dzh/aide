package io.aide.android

import io.aide.android.model.Tool
import org.junit.Assert.assertEquals
import org.junit.Test

/** A tool call shows by its title, else a summary that reads as one; never raw arguments across the row. */
class ToolLineTest {
    @Test fun theMachinesTitleFirst() {
        assertEquals("src/app.ts · +2 −3", Tool("c1", "Edit", """{"file_path":"src/app.ts"}""", title = "src/app.ts · +2 −3").line)
    }

    @Test fun rawArgumentsFromAMachineBeforeTitlesShowNothing() {
        assertEquals("", Tool("c1", "pwsh", """{"command":"cd repo","description":"x"}""").line)
    }

    @Test fun aSummaryOfItsOwnReadsAsItIsOnOneLine() {
        assertEquals("internal/app.go", Tool("c1", "read", "internal/app.go").line)
        assertEquals("first", Tool("c1", "shell", "first\nsecond").line)
    }
}
