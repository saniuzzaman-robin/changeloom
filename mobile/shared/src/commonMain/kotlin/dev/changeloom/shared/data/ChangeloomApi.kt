package dev.changeloom.shared.data

import dev.changeloom.shared.auth.AuthRepository
import io.ktor.client.HttpClient
import io.ktor.client.call.body
import io.ktor.client.plugins.HttpTimeout
import io.ktor.client.plugins.contentnegotiation.ContentNegotiation
import io.ktor.client.plugins.defaultRequest
import io.ktor.client.request.delete
import io.ktor.client.request.get
import io.ktor.client.request.parameter
import io.ktor.client.request.header
import io.ktor.client.request.put
import io.ktor.client.request.setBody
import io.ktor.client.statement.HttpResponse
import io.ktor.http.ContentType
import io.ktor.http.HttpHeaders
import io.ktor.http.contentType
import io.ktor.http.isSuccess
import io.ktor.serialization.kotlinx.json.json
import kotlinx.serialization.json.Json

class ApiException(val status: Int, message: String) : Exception(message)

class ChangeloomApi(
    private val client: HttpClient,
    private val auth: AuthRepository,
) {
    suspend fun topics(): List<Topic> = client.get("v1/topics") { authorize() }.parse<TopicList>().items

    suspend fun me(): Me = client.get("v1/me") { authorize() }.parse()

    suspend fun putMyTopics(slugs: List<String>): Me =
        client.put("v1/me/topics") {
            authorize()
            contentType(ContentType.Application.Json)
            setBody(PutTopicsRequest(slugs))
        }.parse()

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
        val token = auth.idToken() ?: throw ApiException(401, "Not signed in")
        header(HttpHeaders.Authorization, "Bearer $token")
    }

    private suspend inline fun <reified T> HttpResponse.parse(): T {
        checkSuccess()
        return body()
    }

    private fun HttpResponse.checkSuccess() {
        if (!status.isSuccess()) throw ApiException(status.value, "Request failed: HTTP ${status.value}")
    }

    companion object {
        const val TIMELINE_PAGE_SIZE = 20
        private const val REQUEST_TIMEOUT_MS = 15_000L
        private const val CONNECT_TIMEOUT_MS = 10_000L

        fun createClient(baseUrl: String, engine: io.ktor.client.engine.HttpClientEngine): HttpClient =
            HttpClient(engine) {
                expectSuccess = false
                install(ContentNegotiation) { json(Json { ignoreUnknownKeys = true }) }
                install(HttpTimeout) {
                    requestTimeoutMillis = REQUEST_TIMEOUT_MS
                    connectTimeoutMillis = CONNECT_TIMEOUT_MS
                }
                defaultRequest { url(baseUrl.trimEnd('/') + "/") }
            }
    }
}
