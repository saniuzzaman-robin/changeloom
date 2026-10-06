package dev.changeloom.android.push

import dev.changeloom.shared.data.StorySummary
import java.time.Duration
import java.time.LocalDateTime
import kotlin.test.Test
import kotlin.test.assertEquals

class DailyDigestTest {
    private fun at(hour: Int, minute: Int = 0) = LocalDateTime.of(2026, 10, 4, hour, minute)

    @Test
    fun `next digest is the following 9am, 5pm or 930pm, else tomorrow 9am`() {
        assertEquals(Duration.ofHours(2), delayUntilNextDigest(at(7)))
        assertEquals(Duration.ofHours(8), delayUntilNextDigest(at(9)))
        assertEquals(Duration.ofHours(1), delayUntilNextDigest(at(16)))
        assertEquals(Duration.ofMinutes(4 * 60 + 30), delayUntilNextDigest(at(17)))
        assertEquals(Duration.ofMinutes(30), delayUntilNextDigest(at(21)))
        assertEquals(Duration.ofMinutes(11 * 60 + 30), delayUntilNextDigest(at(21, 30)))
        assertEquals(Duration.ofHours(10), delayUntilNextDigest(at(23)))
    }

    private fun story(id: Long, match: String?, read: Boolean = false, importance: Int = 1) = StorySummary(
        id = id, title = "t$id", summary = "", kind = "update", importance = importance,
        publishedAt = "2026-10-04T00:00:00Z", topics = emptyList(), isRead = read, match = match,
    )

    @Test
    fun `digest prefers unread stories matching interests`() {
        val items = listOf(story(1, "explore"), story(2, "followed"), story(3, "profession", read = true), story(4, "profession"))
        assertEquals(listOf(2L, 4L), digestStories(items).map { it.id })
    }

    @Test
    fun `digest previews the most important stories first, ties in timeline order`() {
        val items = listOf(
            story(1, "followed", importance = 2), story(2, "profession", importance = 5), story(3, "followed", importance = 2),
            story(4, "explore", importance = 5), story(5, "followed", read = true, importance = 5),
        )
        assertEquals(listOf(2L, 1L, 3L), digestStories(items).map { it.id })
    }

    @Test
    fun `digest falls back to any unread story`() {
        val items = listOf(story(1, "explore"), story(2, "related", read = true))
        assertEquals(listOf(1L), digestStories(items).map { it.id })
    }
}
