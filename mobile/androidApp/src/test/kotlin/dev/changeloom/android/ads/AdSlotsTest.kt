package dev.changeloom.android.ads

import kotlin.test.Test
import kotlin.test.assertEquals

class AdSlotsTest {
    @Test
    fun firstAdAfterStoryThreeThenEveryInterval() {
        val slots = (0 until 20).associateWith { adSlotAfter(it, interval = 6) }.filterValues { it != null }
        assertEquals(mapOf(2 to 0, 8 to 1, 14 to 2), slots)
    }

    @Test
    fun noAdsInShortFeeds() {
        assertEquals(emptyList(), (0 until FIRST_AD_AFTER - 1).mapNotNull { adSlotAfter(it, interval = 3) })
    }
}
