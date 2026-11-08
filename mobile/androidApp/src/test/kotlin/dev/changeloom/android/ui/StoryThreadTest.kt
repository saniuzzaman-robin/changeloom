package dev.changeloom.android.ui

import dev.changeloom.shared.data.Story
import dev.changeloom.shared.data.StoryThreadItem
import kotlin.test.Test
import kotlin.test.assertEquals

class StoryThreadTest {
    private val story = Story(
        id = 2, title = "go 1.25", summary = "s", kind = "release", importance = 3, publishedAt = "2026-08-12T00:00:00Z",
        topics = emptyList(), isRead = false, bodyMd = "b", sources = emptyList(), version = "1.25.0",
    )

    @Test
    fun noThreadMeansNoEntries() {
        assertEquals(emptyList(), threadEntries(story))
    }

    @Test
    fun theOpenStoryTakesItsPlaceByDateAmongItsThread() {
        val thread = listOf(
            StoryThreadItem(3, "go 1.26", "release", publishedAt = "2026-10-01T00:00:00Z", version = "1.26.0"),
            StoryThreadItem(1, "go 1.24", "release", publishedAt = "2026-02-11T00:00:00Z", version = "1.24.0"),
            StoryThreadItem(4, "go survey", "article", publishedAt = "2026-08-12T00:00:00Z"),
        )
        val entries = threadEntries(story.copy(thread = thread))
        // Same time as the open story: the higher id goes first, as on the server.
        assertEquals(listOf(3L, 4L, 2L, 1L), entries.map { it.id })
        assertEquals(listOf(false, false, true, false), entries.map { it.current })
        assertEquals("1.25.0", entries[2].version)
    }
}
