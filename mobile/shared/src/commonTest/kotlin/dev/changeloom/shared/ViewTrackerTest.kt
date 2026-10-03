package dev.changeloom.shared

import dev.changeloom.shared.data.ViewTracker
import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class ViewTrackerTest {
    @Test
    fun deduplicatesAndBatches() = runTest {
        val sent = mutableListOf<List<Long>>()
        val tracker = ViewTracker(maxBatch = 2) { sent += it }
        listOf(1L, 2L, 1L, 3L, 2L).forEach { tracker.seen(it) }
        tracker.flush()
        assertEquals(listOf(listOf(1L, 2L), listOf(3L)), sent)
        tracker.seen(3)
        tracker.flush()
        assertEquals(2, sent.size) // already reported: nothing more to send
    }

    @Test
    fun failedFlushKeepsUnsentIds() = runTest {
        val sent = mutableListOf<List<Long>>()
        var failing = true
        val tracker = ViewTracker(maxBatch = 2) {
            if (failing) throw IllegalStateException("offline")
            sent += it
        }
        listOf(1L, 2L, 3L).forEach { tracker.seen(it) }
        tracker.flush()
        assertTrue(sent.isEmpty())
        failing = false
        tracker.flush()
        assertEquals(listOf(listOf(1L, 2L), listOf(3L)), sent)
    }
}
