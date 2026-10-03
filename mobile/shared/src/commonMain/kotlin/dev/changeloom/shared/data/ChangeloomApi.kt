package dev.changeloom.shared.data

import dev.changeloom.shared.auth.AuthRepository
import io.ktor.client.HttpClient
import io.ktor.client.call.body
import io.ktor.client.plugins.HttpRequestRetry
import io.ktor.client.plugins.HttpSend
import io.ktor.client.plugins.HttpTimeout
import io.ktor.client.plugins.contentnegotiation.ContentNegotiation
import io.ktor.client.plugins.defaultRequest
import io.ktor.client.plugins.plugin
import io.ktor.client.request.delete
import io.ktor.client.request.get
import io.ktor.client.request.parameter
import io.ktor.client.request.header
import io.ktor.client.request.post
import io.ktor.client.request.put
import io.ktor.client.request.setBody
import io.ktor.client.statement.HttpResponse
import io.ktor.http.ContentType
import io.ktor.http.HttpHeaders
import io.ktor.http.HttpMethod
import io.ktor.http.HttpStatusCode
import io.ktor.http.contentType
import io.ktor.http.isSuccess
import io.ktor.serialization.kotlinx.json.json
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.io.IOException
import kotlinx.serialization.json.Json

/** A non-2xx response. [code] is the api's error code (`not_found`, `rate_limited`, ...) when the body has one. */
class ApiException(val status: Int, val code: String? = null, message: String) : Exception(message)

/** Firebase App Check: proves a request comes from the genuine app. Null when no token is available. */
fun interface AppCheckTokens {
    suspend fun token(): String?
}

/**
 * [httpClient] is built on the first request, so app start (and a signed-out session) never pays for the HTTP stack.
 * [appCheck] adds an App Check token to every signed-in request; the api checks it (APPCHECK_ENFORCE).
 */
class ChangeloomApi(
    httpClient: Lazy<HttpClient>,
    private val auth: AuthRepository,
    private val appCheck: AppCheckTokens? = null,
) {
    constructor(client: HttpClient, auth: AuthRepository) : this(lazyOf(client), auth)

    private val _sessionExpired = MutableSharedFlow<Unit>(extraBufferCapacity = 1)

    /** Emits when the api rejects even a freshly refreshed token; the app should sign out. */
    val sessionExpired: SharedFlow<Unit> = _sessionExpired.asSharedFlow()

    private val client: HttpClient by lazy {
        httpClient.value.also { client ->
            // On a 401, refresh the ID token once and resend; a second 401 means the session is gone.
            client.plugin(HttpSend).intercept { request ->
                val call = execute(request)
                if (call.response.status != HttpStatusCode.Unauthorized) return@intercept call
                val fresh = auth.idToken(forceRefresh = true) ?: return@intercept call
                request.headers[HttpHeaders.Authorization] = "Bearer $fresh"
                execute(request).also {
                    if (it.response.status == HttpStatusCode.Unauthorized) _sessionExpired.tryEmit(Unit)
                }
            }
        }
    }

    suspend fun topicList(): TopicList = client.get("v1/topics") { authorize() }.parse()

    suspend fun topics(): List<Topic> = topicList().items

    suspend fun me(): Me = client.get("v1/me") { authorize() }.parse()

    /** Deletes the signed-in user and all of their data on the server. The sign-in account itself is separate. */
    suspend fun deleteMe() = client.delete("v1/me") { authorize() }.checkSuccess()

    suspend fun putMyTopics(slugs: List<String>): Me =
        client.put("v1/me/topics") {
            authorize()
            contentType(ContentType.Application.Json)
            setBody(PutTopicsRequest(slugs))
        }.parse()

    suspend fun putMyProfessions(slugs: List<String>): Me =
        client.put("v1/me/professions") {
            authorize()
            contentType(ContentType.Application.Json)
            setBody(PutProfessionsRequest(slugs))
        }.parse()

    /** Reports stories the user has seen in the feed (at most [MAX_VIEW_IDS] per call); idempotent on the server. */
    suspend fun recordViews(ids: List<Long>) =
        client.post("v1/stories/views") {
            authorize()
            contentType(ContentType.Application.Json)
            setBody(RecordViewsRequest(ids))
        }.checkSuccess()

    suspend fun timeline(cursor: String? = null, limit: Int = TIMELINE_PAGE_SIZE): TimelinePage =
        client.get("v1/timeline") {
            authorize()
            parameter("limit", limit)
            cursor?.let { parameter("cursor", it) }
        }.parse()

    suspend fun story(id: Long): Story = client.get("v1/stories/$id") { authorize() }.parse()

    suspend fun markRead(id: Long) = client.put("v1/stories/$id/read") { authorize() }.checkSuccess()

    suspend fun markUnread(id: Long) = client.delete("v1/stories/$id/read") { authorize() }.checkSuccess()

    suspend fun search(query: String, cursor: String? = null, limit: Int = TIMELINE_PAGE_SIZE): TimelinePage =
        client.get("v1/search") {
            authorize()
            parameter("q", query)
            parameter("limit", limit)
            cursor?.let { parameter("cursor", it) }
        }.parse()

    suspend fun bookmarks(cursor: String? = null, limit: Int = TIMELINE_PAGE_SIZE): TimelinePage =
        client.get("v1/bookmarks") {
            authorize()
            parameter("limit", limit)
            cursor?.let { parameter("cursor", it) }
        }.parse()

    suspend fun addBookmark(id: Long) = client.put("v1/stories/$id/bookmark") { authorize() }.checkSuccess()

    suspend fun removeBookmark(id: Long) = client.delete("v1/stories/$id/bookmark") { authorize() }.checkSuccess()

    suspend fun topicRequests(): List<TopicRequest> =
        client.get("v1/topic-requests") { authorize() }.parse<TopicRequestList>().items

    suspend fun requestTopic(text: String): TopicRequest =
        client.post("v1/topic-requests") {
            authorize()
            contentType(ContentType.Application.Json)
            setBody(CreateTopicRequest(text))
        }.parse()

    suspend fun registerDevice(token: String, platform: String) =
        client.put("v1/me/devices") {
            authorize()
            contentType(ContentType.Application.Json)
            setBody(RegisterDeviceRequest(token, platform))
        }.checkSuccess()

    suspend fun unregisterDevice(token: String) =
        client.delete("v1/me/devices") {
            authorize()
            parameter("token", token)
        }.checkSuccess()

    private suspend fun io.ktor.client.request.HttpRequestBuilder.authorize() {
        val token = auth.idToken() ?: throw ApiException(401, message = "Not signed in")
        header(HttpHeaders.Authorization, "Bearer $token")
        appCheck?.token()?.let { header(APP_CHECK_HEADER, it) }
    }

    private suspend inline fun <reified T> HttpResponse.parse(): T {
        checkSuccess()
        return body()
    }

    private suspend fun HttpResponse.checkSuccess() {
        if (status.isSuccess()) return
        // Proxies and Cloud Run itself can answer without the api's JSON body; the status is enough then.
        val error = try {
            body<ApiError>()
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            null
        }
        throw ApiException(status.value, error?.code, "HTTP ${status.value} ${error?.code ?: "(no error body)"}: ${error?.message.orEmpty()}")
    }

    companion object {
        const val TIMELINE_PAGE_SIZE = 20
        const val MAX_VIEW_IDS = 100
        const val APP_CHECK_HEADER = "X-Firebase-AppCheck"
        private const val REQUEST_TIMEOUT_MS = 15_000L
        private const val CONNECT_TIMEOUT_MS = 10_000L
        private const val MAX_GET_RETRIES = 2
        private val retryableStatuses = setOf(502, 503, 504)

        fun createClient(baseUrl: String, engine: io.ktor.client.engine.HttpClientEngine): HttpClient =
            HttpClient(engine) {
                expectSuccess = false
                install(ContentNegotiation) { json(Json { ignoreUnknownKeys = true }) }
                install(HttpTimeout) {
                    requestTimeoutMillis = REQUEST_TIMEOUT_MS
                    connectTimeoutMillis = CONNECT_TIMEOUT_MS
                }
                // Only GETs are retried: they are safe to repeat. Writes surface the error instead.
                install(HttpRequestRetry) {
                    retryIf(MAX_GET_RETRIES) { request, response ->
                        request.method == HttpMethod.Get && response.status.value in retryableStatuses
                    }
                    retryOnExceptionIf(MAX_GET_RETRIES) { request, cause ->
                        request.method == HttpMethod.Get && cause is IOException
                    }
                    exponentialDelay()
                }
                defaultRequest { url(baseUrl.trimEnd('/') + "/") }
            }
    }
}
