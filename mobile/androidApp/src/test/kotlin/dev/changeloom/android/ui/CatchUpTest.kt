package dev.changeloom.android.ui

import dev.changeloom.shared.data.StorySummary
import dev.changeloom.shared.data.TimelineState
import kotlin.test.Test
import kotlin.test.assertEquals

class CatchUpTest {
    private fun story(id: Long, read: Boolean = false, seen: Boolean = false) = StorySummary(
        id = id, title = "t$id", summary = "s", kind = "release", importance = 3, publishedAt = "2026-10-01T00:00:00Z",
        topics = emptyList(), isRead = read, seen = seen,
    )

    @Test
    fun theQueueIsUnreadUnseenStoriesInFeedOrderLessThoseSeenThisSession() {
        val items = listOf(story(1), story(2, read = true), story(3, seen = true), story(4), story(5))
        assertEquals(listOf(1L, 5L), catchUpQueue(items, seenInSession = setOf(4)))
    }

    @Test
    fun theCountIsTheServersLessThoseSeenThisSessionButNeverBelowTheQueue() {
        val items = listOf(story(1), story(2), story(3, seen = true))
        // 10 unseen on the server; 2 of them seen here (3 was already seen before, so it isn't one of them).
        assertEquals(9, catchUpCount(TimelineState(items = items, unseenCount = 10), setOf(1, 3)))
        assertEquals(2, catchUpCount(TimelineState(items = items, unseenCount = 0), emptySet()))
        // A cached or offline feed has no server count: only the loaded ones.
        assertEquals(1, catchUpCount(TimelineState(items = items), setOf(2)))
    }

    @Test
    fun pagesFollowTheQueueAndDropStoriesNoLongerInTheTimeline() {
        val items = listOf(story(3), story(1))
        assertEquals(listOf(1L, 3L), catchUpPages(listOf(1, 2, 3), items).map { it.id })
    }

    @Test
    fun laterStoriesJoinTheEndAndQueuedOnesKeepTheirPlace() {
        // 1 was seen on the way, so it is no longer queued, but stays where it was.
        assertEquals(listOf(1L, 2L, 4L), extendCatchUp(listOf(1, 2), listOf(2, 4)))
        val queue = listOf(1L, 2L)
        assertEquals(queue, extendCatchUp(queue, listOf(2)))
    }
}
