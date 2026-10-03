package dev.changeloom.android.ui

import androidx.lifecycle.SavedStateHandle
import dev.changeloom.shared.data.TimelineRepository
import io.ktor.client.engine.mock.respond
import io.ktor.http.HttpHeaders
import io.ktor.http.headersOf
import kotlinx.coroutines.test.runTest
import org.junit.Rule
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

private val json = headersOf(HttpHeaders.ContentType, "application/json")
private const val ONE_RESULT =
    """{"items":[{"id":7,"title":"t","summary":"s","kind":"release","importance":3,"published_at":"2026-10-01T00:00:00Z","topics":["x"],"is_read":false}]}"""

class SearchViewModelTest {
    @get:Rule val main = MainDispatcherRule()

    private fun vm(saved: SavedStateHandle, requests: MutableList<String>): SearchViewModel {
        val auth = FakeAuth()
        val api = fakeApi(auth, requests) { respond(ONE_RESULT, headers = json) }
        return SearchViewModel(api, TimelineRepository(api, MemoryCache()), NoAnalytics, saved)
    }

    @Test
    fun aRestoredSearchRunsAgain() = runTest {
        val requests = mutableListOf<String>()
        val vm = vm(SavedStateHandle(mapOf("query" to "kotlin 2", "searched" to "kotlin 2")), requests)
        assertEquals("kotlin 2", vm.query.value)
        assertEquals(listOf(7L), vm.state.await { it.items.isNotEmpty() }.items.map { it.id })
        assertEquals(1, requests.count { it.startsWith("GET /v1/search?q=kotlin+2") })
    }

    @Test
    fun theQueryAndLastSearchAreSaved() = runTest {
        val saved = SavedStateHandle()
        val vm = vm(saved, mutableListOf())
        vm.setQuery(" go ")
        vm.search()
        assertEquals(" go ", saved.get<String>("query"))
        assertEquals("go", saved.get<String>("searched"))
        vm.setQuery("")
        vm.search()
        assertNull(saved.get<String>("searched"))
    }
}
