package dev.changeloom.android.play

import dev.changeloom.android.ui.MemoryReviewStore
import kotlin.test.Test
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class ReviewPrompterTest {
    private var now = REVIEW_INTERVAL_MS
    private val prompter = ReviewPrompter(MemoryReviewStore()) { now }

    private fun read(times: Int) = repeat(times) { prompter.storyRead() }

    @Test
    fun dueAfterEnoughReads() {
        read(READS_BEFORE_REVIEW - 1)
        assertFalse(prompter.due.value)
        read(1)
        assertTrue(prompter.due.value)
    }

    @Test
    fun savesCountTowardThePrompt() {
        read(READS_BEFORE_REVIEW - 1)
        prompter.storySaved()
        assertTrue(prompter.due.value)
    }

    @Test
    fun atMostOncePerInterval() {
        read(READS_BEFORE_REVIEW)
        prompter.prompted()
        assertFalse(prompter.due.value)
        now += REVIEW_INTERVAL_MS - 1
        read(READS_BEFORE_REVIEW * 2)
        assertFalse(prompter.due.value)
        now += 1
        read(1)
        assertTrue(prompter.due.value)
    }
}
