package dev.changeloom.android.ui

import androidx.lifecycle.viewModelScope
import dev.changeloom.android.data.FollowSuggester
import dev.changeloom.shared.data.TimelineRepository
import dev.changeloom.shared.data.ViewTracker
import io.ktor.client.engine.mock.respond
import io.ktor.http.HttpHeaders
import io.ktor.http.HttpMethod
import io.ktor.http.HttpStatusCode
import io.ktor.http.headersOf
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
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

    private fun vm(requests: MutableList<String>): TimelineViewModel {
        val api = fakeApi(FakeAuth(), requests) { request ->
            when {
                request.url.encodedPath.endsWith("/dismiss") -> respond("", HttpStatusCode.NoContent)
                request.url.encodedPath == "/v1/me" -> respond("""{"id":1,"topics":[]}""", headers = json)
                request.url.encodedPath == "/v1/me/muted-topics" && request.method == HttpMethod.Put ->
                    respond("""{"id":1,"topics":[],"muted_topics":["web/vue"]}""", headers = json)
                else -> respond(TWO_STORIES, headers = json)
            }
        }
        return TimelineViewModel(TimelineRepository(api, MemoryCache()), NoAnalytics, ViewTracker { }, suggester)
    }

    @Test
    fun notInterestedHidesTheStoryAndUndoBringsItBack() = runTest {
        val requests: MutableList<String> = Collections.synchronizedList(mutableListOf())
        val vm = vm(requests)
        try {
            notInterested(vm, requests)
        } finally {
            vm.viewModelScope.cancel() // stops the view tracker's endless flush loop, or runTest never idles
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
    fun lessAboutHidesTheTopicsStoriesUntilTheUndoExpires() = runTest {
        val requests: MutableList<String> = Collections.synchronizedList(mutableListOf())
        val vm = vm(requests)
        try {
            lessAbout(vm)
        } finally {
            vm.viewModelScope.cancel()
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
