package dev.changeloom.android.push

import dev.changeloom.shared.data.StorySummary
import java.time.Duration
import java.time.LocalDateTime
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

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

    @Test
    fun `next slot is the following digest time`() {
        assertEquals(at(9), nextDigestSlot(at(7)))
        assertEquals(at(17), nextDigestSlot(at(9)))
        assertEquals(at(21, 30), nextDigestSlot(at(17, 1)))
        assertEquals(at(9).plusDays(1), nextDigestSlot(at(21, 30)))
    }

    @Test
    fun `a run is on time shortly before or within half an hour after its slot`() {
        assertTrue(isOnTime(at(9), at(9, 0)))
        assertTrue(isOnTime(at(9), at(9, 29)))
        assertTrue(isOnTime(at(9), at(8, 56)))
    }

    @Test
    fun `a run far from its slot is skipped, so a delayed or time-zone-shifted one never shows at the wrong time`() {
        assertFalse(isOnTime(at(9), at(9, 31)))
        assertFalse(isOnTime(at(9), at(11, 40)))
        assertFalse(isOnTime(at(9), at(8, 50)))
        assertFalse(isOnTime(at(17), at(12, 0)))
    }

    @Test
    fun `the greeting follows the slot, not the time the run happens`() {
        assertEquals(DigestPeriod.Morning, digestPeriod(at(9)))
        assertEquals(DigestPeriod.Evening, digestPeriod(at(17)))
        assertEquals(DigestPeriod.Night, digestPeriod(at(21, 30)))
    }
}
