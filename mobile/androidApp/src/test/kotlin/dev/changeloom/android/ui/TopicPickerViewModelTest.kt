package dev.changeloom.android.ui

import dev.changeloom.shared.data.TimelineRepository
import io.ktor.client.engine.mock.respond
import io.ktor.http.HttpHeaders
import io.ktor.http.headersOf
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.test.runTest
import org.junit.Rule
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class TopicPickerViewModelTest {
    @get:Rule val main = MainDispatcherRule()

    private val json = headersOf(HttpHeaders.ContentType, "application/json")

    @Test
    fun signInOpensTheFeedOnTheAccountWithoutWaitingForTheCatalog() = runTest {
        val catalog = CompletableDeferred<Unit>()
        val requests = mutableListOf<String>()
        val api = fakeApi(FakeAuth(), requests) { request ->
            when (request.url.encodedPath) {
                "/v1/me" -> respond("""{"id":1,"topics":["go"]}""", headers = json)
                "/v1/topics" -> {
                    catalog.await()
                    respond("""{"items":[{"slug":"go","name":"Go","description":"d"}]}""", headers = json)
                }
                else -> respond("""{"items":[]}""", headers = json)
            }
        }
        val repo = TimelineRepository(api, MemoryCache())
        val vm = TopicPickerViewModel(api, TopicCatalog(api), repo, MemoryCache(), NoAnalytics, testStrings)

        val early = vm.state.await { it.followed.isNotEmpty() }
        assertEquals(listOf("go"), early.followed)
        assertTrue(early.loading, "the catalog is still loading")
        repo.state.await { !it.refreshing }
        assertTrue(requests.any { it.startsWith("GET /v1/timeline") }, "the timeline is fetched alongside the account")

        catalog.complete(Unit)
        val loaded = vm.state.await { !it.loading }
        assertEquals(listOf("go"), loaded.followed)
    }
}
