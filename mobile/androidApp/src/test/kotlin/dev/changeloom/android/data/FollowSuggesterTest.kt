package dev.changeloom.android.data

import dev.changeloom.android.ui.PrefsContext
import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

class FollowSuggesterTest {
    @Test
    fun suggestsTheMostOpenedUnfollowedTopicPastTheThreshold() {
        val counts = OpenCounts(
            opens = mapOf("web/react" to 5, "web/vue" to 4, "languages/go" to 4, "cloud/aws" to FOLLOW_SUGGESTION_OPENS - 1, "databases" to 9),
            declined = setOf("databases"),
        )
        // databases is declined; web/react is followed through its root; go and vue tie, so the first slug wins.
        assertEquals("languages/go", counts.suggestion(followed = setOf("web")))
        assertEquals("web/react", counts.suggestion(followed = emptySet()))
        assertNull(OpenCounts(mapOf("cloud/aws" to FOLLOW_SUGGESTION_OPENS - 1)).suggestion(emptySet()))
    }

    @Test
    fun opensAndDeclinesSurviveARestartAndClearOnSignOut() = runTest {
        val context = PrefsContext()
        val suggester = FollowSuggester(context)
        repeat(FOLLOW_SUGGESTION_OPENS) { suggester.recordOpen(listOf("languages/go", "languages/go", "web")) }
        suggester.decline("web")

        val restarted = FollowSuggester(context)
        restarted.load()
        assertEquals(OpenCounts(mapOf("languages/go" to 3, "web" to 3), setOf("web")), restarted.counts.value)
        assertEquals("languages/go", restarted.counts.value.suggestion(emptySet()))

        restarted.clear()
        val afterSignOut = FollowSuggester(context)
        afterSignOut.load()
        assertEquals(OpenCounts(), afterSignOut.counts.value)
    }
}
