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
import io.ktor.http.HttpMethod
import io.ktor.http.HttpStatusCode
import io.ktor.http.content.TextContent
import io.ktor.http.headersOf
import kotlinx.io.IOException
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.yield
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertNull

private class FakeAuth(private val token: String?) : AuthRepository {
    override val currentUser: StateFlow<AuthUser?> = MutableStateFlow(null)
    override suspend fun idToken(forceRefresh: Boolean) = token
    override suspend fun signInWithEmail(email: String, password: String) = Unit
    override suspend fun registerWithEmail(email: String, password: String) = Unit
    override suspend fun signInWithGoogleIdToken(googleIdToken: String) = false
    override fun signOut() = Unit
    override fun needsReauth() = false
    override suspend fun reauthenticateWithPassword(password: String) = Unit
    override suspend fun reauthenticateWithGoogleIdToken(googleIdToken: String) = Unit
    override suspend fun deleteUser() = Unit
}

/** Hands out "old" until a forced refresh, then "new". */
private class RefreshingAuth : AuthRepository {
    var refreshes = 0
    override val currentUser: StateFlow<AuthUser?> = MutableStateFlow(null)
    override suspend fun idToken(forceRefresh: Boolean): String {
        if (forceRefresh) refreshes++
        return if (refreshes > 0) "new" else "old"
    }
    override suspend fun signInWithEmail(email: String, password: String) = Unit
    override suspend fun registerWithEmail(email: String, password: String) = Unit
    override suspend fun signInWithGoogleIdToken(googleIdToken: String) = false
    override fun signOut() = Unit
    override fun needsReauth() = false
    override suspend fun reauthenticateWithPassword(password: String) = Unit
    override suspend fun reauthenticateWithGoogleIdToken(googleIdToken: String) = Unit
    override suspend fun deleteUser() = Unit
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
    fun appCheckTokenIsSentWhenAvailable() = runTest {
        val seen = mutableListOf<String?>()
        val engine = MockEngine { request ->
            seen += request.headers[ChangeloomApi.APP_CHECK_HEADER]
            respond("""{"items":[]}""", headers = jsonHeaders)
        }
        var token: String? = "attested"
        val api = ChangeloomApi(lazyOf(ChangeloomApi.createClient("http://api.test", engine)), FakeAuth("tok")) { token }
        api.topics()
        token = null
        api.topics()
        assertEquals(listOf("attested", null), seen)
    }

    @Test
    fun deleteMeSendsDelete() = runTest {
        val engine = MockEngine { request ->
            assertEquals(HttpMethod.Delete, request.method)
            assertEquals("http://api.test/v1/me", request.url.toString())
            respond("", HttpStatusCode.NoContent)
        }
        ChangeloomApi(ChangeloomApi.createClient("http://api.test", engine), FakeAuth("tok")).deleteMe()
    }

    @Test
    fun clientIsBuiltOnFirstRequest() = runTest {
        var built = 0
        val api = ChangeloomApi(
            lazy {
                built++
                ChangeloomApi.createClient("http://api.test", MockEngine { respond("""{"items":[]}""", headers = jsonHeaders) })
            },
            FakeAuth("tok"),
        )
        assertEquals(0, built)
        api.topics()
        api.topics()
        assertEquals(1, built)
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
    fun errorBodyGivesTheCode() = runTest {
        val engine = MockEngine {
            respond("""{"code":"not_found","message":"story not found"}""", HttpStatusCode.NotFound, jsonHeaders)
        }
        val api = ChangeloomApi(ChangeloomApi.createClient("http://api.test", engine), FakeAuth("tok"))
        val e = assertFailsWith<ApiException> { api.story(1) }
        assertEquals(404, e.status)
        assertEquals("not_found", e.code)
    }

    @Test
    fun errorWithoutBodyHasNoCode() = runTest {
        val engine = MockEngine { respondError(HttpStatusCode.BadRequest) }
        val api = ChangeloomApi(ChangeloomApi.createClient("http://api.test", engine), FakeAuth("tok"))
        assertNull(assertFailsWith<ApiException> { api.me() }.code)
    }

    @Test
    fun unauthorizedRefreshesTheTokenOnceAndResends() = runTest {
        val auth = RefreshingAuth()
        val tokens = mutableListOf<String?>()
        val engine = MockEngine { request ->
            tokens += request.headers[HttpHeaders.Authorization]
            if (request.headers[HttpHeaders.Authorization] == "Bearer new") {
                respond("""{"id":1,"topics":[]}""", headers = jsonHeaders)
            } else {
                respondError(HttpStatusCode.Unauthorized)
            }
        }
        val api = ChangeloomApi(ChangeloomApi.createClient("http://api.test", engine), auth)
        assertEquals(1L, api.me().id)
        assertEquals(listOf<String?>("Bearer old", "Bearer new"), tokens)
        assertEquals(1, auth.refreshes)
    }

    @Test
    fun secondUnauthorizedExpiresTheSession() = runTest {
        val engine = MockEngine { respondError(HttpStatusCode.Unauthorized) }
        val api = ChangeloomApi(ChangeloomApi.createClient("http://api.test", engine), RefreshingAuth())
        val expired = async { api.sessionExpired.first() }
        yield() // let the collector subscribe
        assertEquals(401, assertFailsWith<ApiException> { api.me() }.status)
        expired.await()
    }

    @Test
    fun getsAreRetriedOnUnavailableButWritesAreNot() = runTest {
        val seen = mutableListOf<String>()
        val engine = MockEngine { request ->
            seen += request.method.value
            if (seen.count { it == request.method.value } == 1) {
                respondError(HttpStatusCode.ServiceUnavailable)
            } else {
                respond("""{"id":1,"topics":["a"]}""", headers = jsonHeaders)
            }
        }
        val api = ChangeloomApi(ChangeloomApi.createClient("http://api.test", engine), FakeAuth("tok"))
        assertEquals(1L, api.me().id)
        assertEquals(503, assertFailsWith<ApiException> { api.putMyTopics(listOf("a")) }.status)
        assertEquals(listOf("GET", "GET", "PUT"), seen)
    }

    @Test
    fun getsAreRetriedOnNetworkErrors() = runTest {
        var calls = 0
        val engine = MockEngine {
            if (++calls < 3) throw IOException("connection reset")
            respond("""{"id":1,"topics":[]}""", headers = jsonHeaders)
        }
        val api = ChangeloomApi(ChangeloomApi.createClient("http://api.test", engine), FakeAuth("tok"))
        assertEquals(1L, api.me().id)
        assertEquals(3, calls)
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

    @Test
    fun topicRequestsListAndCreate() = runTest {
        val req = """{"id":7,"text":"Rust","status":"merged","topic":"lang/rust","note":"Already covered","created_at":"2026-09-01T00:00:00Z","resolved_at":"2026-09-02T00:00:00Z"}"""
        var postBody: String? = null
        val engine = MockEngine { request ->
            if (request.method == HttpMethod.Post) {
                postBody = (request.body as TextContent).text
                respond(req, HttpStatusCode.Created, jsonHeaders)
            } else {
                respond("""{"items":[$req]}""", headers = jsonHeaders)
            }
        }
        val api = ChangeloomApi(ChangeloomApi.createClient("http://api.test", engine), FakeAuth("tok"))
        val created = api.requestTopic("Rust")
        assertEquals("""{"text":"Rust"}""", postBody)
        assertEquals("merged", created.status)
        val list = api.topicRequests()
        assertEquals(listOf("lang/rust"), list.map { it.topic })
        assertEquals("Already covered", list.single().note)
    }
}
