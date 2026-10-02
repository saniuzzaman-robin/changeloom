package dev.changeloom.shared

import dev.changeloom.shared.data.StoryPager
import dev.changeloom.shared.data.StorySummary
import dev.changeloom.shared.data.TimelinePage
import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

private fun story(id: Long, bookmarked: Boolean = false) =
    StorySummary(id, "t$id", "s", "release", null, 3, "2026-09-01T00:00:00Z", listOf("x"), false, isBookmarked = bookmarked)

class StoryPagerTest {
    @Test
    fun refreshThenLoadMoreAppendsWithoutDuplicates() = runTest {
        val cursors = mutableListOf<String?>()
        val pager = StoryPager { cursor ->
            cursors += cursor
            if (cursor == null) TimelinePage(listOf(story(1), story(2)), "c1") else TimelinePage(listOf(story(2), story(3)), null)
        }
        pager.refresh()
        assertEquals(listOf(1L, 2L), pager.state.value.items.map { it.id })
        assertTrue(pager.state.value.loaded)
        pager.loadMore()
        assertEquals(listOf(1L, 2L, 3L), pager.state.value.items.map { it.id })
        assertNull(pager.state.value.nextCursor)
        pager.loadMore() // no cursor left: no request
        assertEquals(listOf(null, "c1"), cursors)
    }

    @Test
    fun failedRefreshKeepsItemsAndReportsError() = runTest {
        var fail = false
        val pager = StoryPager { if (fail) error("boom") else TimelinePage(listOf(story(1)), null) }
        pager.refresh()
        fail = true
        pager.refresh()
        assertEquals(listOf(1L), pager.state.value.items.map { it.id })
        assertEquals("boom", pager.state.value.error)
        assertFalse(pager.state.value.loading)
    }

    @Test
    fun setBookmarkedUpdatesOnlyThatStory() = runTest {
        val pager = StoryPager { TimelinePage(listOf(story(1), story(2, bookmarked = true)), null) }
        pager.refresh()
        pager.setBookmarked(1, true)
        pager.setBookmarked(2, false)
        assertEquals(listOf(true, false), pager.state.value.items.map { it.isBookmarked })
    }

    @Test
    fun removeDropsOnlyThatStoryAndKeepsCursor() = runTest {
        val pager = StoryPager { TimelinePage(listOf(story(1), story(2), story(3)), "c1") }
        pager.refresh()
        pager.remove(2)
        pager.remove(99) // unknown id: no change
        assertEquals(listOf(1L, 3L), pager.state.value.items.map { it.id })
        assertEquals("c1", pager.state.value.nextCursor)
    }
}
