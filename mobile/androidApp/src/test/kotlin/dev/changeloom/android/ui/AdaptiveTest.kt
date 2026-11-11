package dev.changeloom.android.ui

import kotlin.test.Test
import kotlin.test.assertEquals

class AdaptiveTest {
    @Test
    fun compactEndsJustBelow600dp() {
        assertEquals(WindowWidth.Compact, windowWidthOf(0f))
        assertEquals(WindowWidth.Compact, windowWidthOf(599f))
        assertEquals(WindowWidth.Compact, windowWidthOf(599.9f))
    }

    @Test
    fun mediumSpans600Through839dp() {
        assertEquals(WindowWidth.Medium, windowWidthOf(600f))
        assertEquals(WindowWidth.Medium, windowWidthOf(839f))
        assertEquals(WindowWidth.Medium, windowWidthOf(839.9f))
    }

    @Test
    fun expandedStartsAt840dp() {
        assertEquals(WindowWidth.Expanded, windowWidthOf(840f))
        assertEquals(WindowWidth.Expanded, windowWidthOf(1280f))
    }
}
