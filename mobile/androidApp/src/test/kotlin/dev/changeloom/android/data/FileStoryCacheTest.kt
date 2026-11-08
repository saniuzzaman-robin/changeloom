package dev.changeloom.android.data

import dev.changeloom.shared.data.Story
import dev.changeloom.shared.data.StorySummary
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Rule
import org.junit.rules.TemporaryFolder
import java.io.File
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

private fun summary(id: Long) =
    StorySummary(id, "t", "s", "release", importance = 3, publishedAt = "2026-11-01T00:00:00Z", topics = emptyList(), isRead = false)

private fun story(id: Long) = Story(
    id, "t", "s", "release", importance = 3, publishedAt = "2026-11-01T00:00:00Z", topics = emptyList(), isRead = false,
    bodyMd = "b", sources = emptyList(),
)

class FileStoryCacheTest {
    @get:Rule val folder = TemporaryFolder()

    /** Timeline writes run on the test scheduler, so a test decides when they happen. */
    private fun TestScope.cache(dir: File = folder.root) = FileStoryCache(dir, backgroundScope)

    @Test
    fun aBurstOfTimelineSavesWritesOnlyTheLatest() = runTest {
        val cache = cache()
        cache.saveTimeline(listOf(summary(1)))
        cache.saveTimeline(listOf(summary(1), summary(2)))
        cache.saveTimeline(listOf(summary(3)))
        assertFalse(File(folder.root, "timeline.json").exists())
        // Not on disk yet, but a load already sees it.
        assertEquals(listOf(3L), cache.loadTimeline().map { it.id })

        runCurrent()
        assertEquals(listOf(3L), FileStoryCache(folder.root).loadTimeline().map { it.id })
    }

    @Test
    fun clearDropsATimelineNotYetWritten() = runTest {
        val cache = cache()
        cache.saveTimeline(listOf(summary(1)))
        cache.clear()
        runCurrent()
        assertTrue(cache.loadTimeline().isEmpty())
        assertFalse(File(folder.root, "timeline.json").exists())
    }

    @Test
    fun storiesArePrunedToTheNewestHundredOnlyPastTheSlack() = runTest {
        val cache = cache()
        val stories = File(folder.root, "stories")
        for (id in 1L..120L) cache.saveStory(story(id))
        // Within the slack: no prune, and resaving a known story doesn't count twice.
        cache.saveStory(story(1))
        assertEquals(120, stories.list()?.size)

        cache.saveStory(story(121))
        assertEquals(100, stories.list()?.size)
    }

    @Test
    fun aNewCacheCountsTheStoriesAlreadyOnDisk() = runTest {
        val first = cache()
        for (id in 1L..120L) first.saveStory(story(id))
        val reopened = cache()
        reopened.saveStory(story(121))
        assertEquals(100, File(folder.root, "stories").list()?.size)
    }
}
