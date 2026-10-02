package dev.changeloom.shared

import dev.changeloom.shared.auth.AuthRepository
import dev.changeloom.shared.auth.AuthUser
import dev.changeloom.shared.data.ApiException
import dev.changeloom.shared.data.ChangeloomApi
import dev.changeloom.shared.data.MeStats
import io.ktor.client.engine.mock.MockEngine
import io.ktor.client.engine.mock.respond
import io.ktor.client.engine.mock.respondError
import io.ktor.http.HttpHeaders
import io.ktor.http.HttpStatusCode
import io.ktor.http.content.TextContent
import io.ktor.http.headersOf
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith

private class FakeAuth(private val token: String?) : AuthRepository {
    override val currentUser: StateFlow<AuthUser?> = MutableStateFlow(null)
    override suspend fun idToken() = token
    override suspend fun signInWithEmail(email: String, password: String) = Unit
    override suspend fun registerWithEmail(email: String, password: String) = Unit
    override suspend fun signInWithGoogleIdToken(googleIdToken: String) = Unit
    override fun signOut() = Unit
}

private val jsonHeaders = headersOf(HttpHeaders.ContentType, "application/json")

class ChangeloomApiTest {
    @Test
    fun topicsSendsBearerTokenAndParses() = runTest {
        val engine = MockEngine { request ->
            assertEquals("Bearer tok", request.headers[HttpHeaders.Authorization])
            assertEquals("http://api.test/v1/topics", request.url.toString())
            respond("""{"items":[{"slug":"languages/go","name":"Go","description":"d","extra":1}]}""", headers = jsonHeaders)
        }
        val api = ChangeloomApi(ChangeloomApi.createClient("http://api.test", engine), FakeAuth("tok"))
        assertEquals("languages/go", api.topics().single().slug)
    }

    @Test
    fun putMyTopicsSendsSlugs() = runTest {
        val engine = MockEngine { request ->
            assertEquals("""{"topics":["a","b"]}""", (request.body as TextContent).text)
            respond("""{"id":1,"topics":["a","b"]}""", headers = jsonHeaders)
        }
        val api = ChangeloomApi(ChangeloomApi.createClient("http://api.test", engine), FakeAuth("tok"))
        assertEquals(listOf("a", "b"), api.putMyTopics(listOf("a", "b")).topics)
    }

    @Test
    fun meParsesStatsAndDefaultsWhenAbsent() = runTest {
        var body = """{"id":1,"topics":[],"stats":{"saved":3,"read":7}}"""
        val engine = MockEngine { respond(body, headers = jsonHeaders) }
        val api = ChangeloomApi(ChangeloomApi.createClient("http://api.test", engine), FakeAuth("tok"))
        assertEquals(MeStats(saved = 3, read = 7), api.me().stats)
        body = """{"id":1,"topics":[]}"""
        assertEquals(MeStats(), api.me().stats)
    }

    @Test
    fun httpErrorBecomesApiException() = runTest {
        val engine = MockEngine { respondError(HttpStatusCode.Unauthorized) }
        val api = ChangeloomApi(ChangeloomApi.createClient("http://api.test", engine), FakeAuth("tok"))
        assertEquals(401, assertFailsWith<ApiException> { api.me() }.status)
    }

    @Test
    fun signedOutFailsWithoutRequest() = runTest {
        val engine = MockEngine { error("no request expected") }
        val api = ChangeloomApi(ChangeloomApi.createClient("http://api.test", engine), FakeAuth(null))
        assertEquals(401, assertFailsWith<ApiException> { api.me() }.status)
    }

    @Test
    fun searchBookmarkAndDeviceCallsUseExpectedRoutes() = runTest {
        val seen = mutableListOf<String>()
        val engine = MockEngine { request ->
            seen += "${request.method.value} ${request.url.encodedPath}?${request.url.encodedQuery}"
            if (request.url.encodedPath.endsWith("search") || request.url.encodedPath.endsWith("bookmarks")) {
                respond("""{"items":[]}""", headers = jsonHeaders)
            } else {
                respond("", HttpStatusCode.NoContent)
            }
        }
        val api = ChangeloomApi(ChangeloomApi.createClient("http://api.test", engine), FakeAuth("tok"))
        api.search("go 1.30", cursor = "c")
        api.bookmarks()
        api.addBookmark(5)
        api.removeBookmark(5)
        api.registerDevice("tok-1", "android")
        api.unregisterDevice("tok-1")
        assertEquals(
            listOf(
                "GET /v1/search?q=go+1.30&limit=20&cursor=c",
                "GET /v1/bookmarks?limit=20",
                "PUT /v1/stories/5/bookmark?",
                "DELETE /v1/stories/5/bookmark?",
                "PUT /v1/me/devices?",
                "DELETE /v1/me/devices?token=tok-1",
            ),
            seen,
        )
    }
}
