package dev.changeloom.android.ui

import dev.changeloom.android.data.FollowSuggester
import dev.changeloom.shared.data.TimelineRepository
import dev.changeloom.shared.data.ViewTracker
import io.ktor.client.engine.mock.respond
import io.ktor.http.HttpHeaders
import io.ktor.http.HttpMethod
import io.ktor.http.HttpStatusCode
import io.ktor.http.headersOf
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeout
import org.junit.Rule
import java.util.Collections
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertIs
import kotlin.test.assertTrue

private const val AWAIT_MS = 5_000L
private const val POLL_MS = 10L
private val json = headersOf(HttpHeaders.ContentType, "application/json")
private const val TWO_STORIES = """{"items":[""" +
    """{"id":1,"title":"t","summary":"s","kind":"release","importance":3,"published_at":"2026-10-02T00:00:00Z","topics":["web/vue"],"is_read":false},""" +
    """{"id":2,"title":"t","summary":"s","kind":"release","importance":3,"published_at":"2026-10-01T00:00:00Z","topics":["cloud/aws"],"is_read":false}]}"""

class TimelineViewModelTest {
    @get:Rule val main = MainDispatcherRule()

    private val suggester = FollowSuggester(PrefsContext())

    /** What GET /v1/me/topic-suggestions answers; null fails it. */
    @Volatile private var suggestions: String? = """{"items":[{"slug":"web/react","name":"React","description":""}]}"""

    private fun vm(requests: MutableList<String>, views: ViewTracker = ViewTracker { }): TimelineViewModel {
        val api = fakeApi(FakeAuth(), requests) { request ->
            when {
                request.url.encodedPath == "/v1/me/topic-suggestions" ->
                    suggestions?.let { respond(it, headers = json) } ?: respond("", HttpStatusCode.InternalServerError)
                request.url.encodedPath.endsWith("/dismiss") || request.url.encodedPath.endsWith("/boost") ->
                    respond("", HttpStatusCode.NoContent)
                request.url.encodedPath == "/v1/me" -> respond("""{"id":1,"topics":[]}""", headers = json)
                request.url.encodedPath == "/v1/me/muted-topics" && request.method == HttpMethod.Put ->
                    respond("""{"id":1,"topics":[],"muted_topics":["web/vue"]}""", headers = json)
                else -> respond(TWO_STORIES, headers = json)
            }
        }
        return TimelineViewModel(TimelineRepository(api, MemoryCache()), NoAnalytics, views, suggester)
    }

    @Test
    fun viewsAreSentOnlyWhileTrackedAndOnceMoreWhenTrackingStops() = runTest {
        val sent: MutableList<Long> = Collections.synchronizedList(mutableListOf())
        val vm = vm(Collections.synchronizedList(mutableListOf()), ViewTracker { sent += it })
        try {
            vm.storySeen(1)
            assertEquals(setOf(1L), vm.seenInSession.value) // catch-up leaves it out right away, before any send
            advanceTimeBy(VIEW_FLUSH_INTERVAL_MS * 2)
            assertTrue(sent.isEmpty()) // not tracked (the app is in the background): nothing is sent

            val tracking = launch { vm.trackViews() }
            advanceTimeBy(VIEW_FLUSH_INTERVAL_MS + 1)
            assertEquals(listOf(1L), sent)

            vm.storySeen(2)
            tracking.cancelAndJoin()
            assertEquals(listOf(1L, 2L), sent)
        } finally {
            vm.stop()
        }
    }

    @Test
    fun topicSuggestionsLoadBesideTheTimelineAndAFailureKeepsThem() = runTest {
        val requests: MutableList<String> = Collections.synchronizedList(mutableListOf())
        val vm = vm(requests)
        try {
            assertEquals(listOf("web/react"), vm.topicSuggestions.await { it.isNotEmpty() })
            vm.state.await { it.items.size == 2 }

            suggestions = null
            vm.refresh()
            withContext(Dispatchers.Default) {
                withTimeout(AWAIT_MS) { while (requests.count { it == "GET /v1/me/topic-suggestions" } < 2) delay(POLL_MS) }
            }
            vm.state.await { !it.refreshing }
            assertEquals(listOf("web/react"), vm.topicSuggestions.value)
        } finally {
            vm.stop()
        }
    }

    @Test
    fun notInterestedHidesTheStoryAndUndoBringsItBack() = runTest {
        val requests: MutableList<String> = Collections.synchronizedList(mutableListOf())
        val vm = vm(requests)
        try {
            notInterested(vm, requests)
        } finally {
            vm.stop()
        }
    }

    private suspend fun notInterested(vm: TimelineViewModel, requests: List<String>) {
        vm.state.await { it.items.size == 2 }
        vm.dismiss(1)
        val undo = assertIs<FeedUndo.Dismissed>(vm.undo.await { it != null })
        assertEquals(listOf(2L), vm.state.value.items.map { it.id })

        vm.undo(undo)
        assertEquals(null, vm.undo.value)
        assertEquals(listOf(1L, 2L), vm.state.value.items.map { it.id })
        assertTrue("PUT /v1/stories/1/dismiss" in requests)
        // The undo changes no state once the server has it, so wait for the request itself.
        withContext(Dispatchers.Default) { withTimeout(AWAIT_MS) { while ("DELETE /v1/stories/1/dismiss" !in requests) delay(POLL_MS) } }
    }

    @Test
    fun moreLikeThisKeepsTheStoryAndUndoUnboostsIt() = runTest {
        val requests: MutableList<String> = Collections.synchronizedList(mutableListOf())
        val vm = vm(requests)
        try {
            vm.state.await { it.items.size == 2 }
            vm.boost(1)
            val undo = assertIs<FeedUndo.Boosted>(vm.undo.await { it != null })
            assertEquals(FeedUndo.Boosted(1, "web/vue"), undo)
            assertEquals(listOf(1L, 2L), vm.state.value.items.map { it.id })
            assertEquals(setOf(1L), vm.state.value.boosted)
            assertTrue("PUT /v1/stories/1/boost" in requests)

            vm.undo(undo)
            assertEquals(null, vm.undo.value)
            vm.state.await { it.boosted.isEmpty() }
            withContext(Dispatchers.Default) { withTimeout(AWAIT_MS) { while ("DELETE /v1/stories/1/boost" !in requests) delay(POLL_MS) } }
        } finally {
            vm.stop()
        }
    }

    @Test
    fun lessAboutHidesTheTopicsStoriesUntilTheUndoExpires() = runTest {
        val requests: MutableList<String> = Collections.synchronizedList(mutableListOf())
        val vm = vm(requests)
        try {
            lessAbout(vm)
        } finally {
            vm.stop()
        }
    }

    private suspend fun lessAbout(vm: TimelineViewModel) {
        vm.state.await { it.items.size == 2 }
        vm.muteTopic("web/vue")
        val undo = assertIs<FeedUndo.Muted>(vm.undo.await { it != null })
        assertEquals(listOf(1L), undo.hidden.map { it.id })
        assertEquals(listOf(2L), vm.state.value.items.map { it.id })

        vm.undoExpired(undo)
        assertEquals(null, vm.undo.value)
        assertEquals(listOf(2L), vm.state.value.items.map { it.id })
        // A muted topic is never suggested for following.
        suggester.counts.await { "web/vue" in it.declined }
    }
}
