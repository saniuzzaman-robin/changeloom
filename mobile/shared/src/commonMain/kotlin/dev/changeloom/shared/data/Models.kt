package dev.changeloom.shared.data

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable
data class Topic(
    val slug: String,
    val name: String,
    val parent: String? = null,
    val description: String,
)

@Serializable
data class Profession(
    val slug: String,
    val name: String,
    val description: String = "",
    /** Root topic slugs (areas) this profession suggests, in display order. */
    val topics: List<String> = emptyList(),
)

@Serializable
data class TopicList(val items: List<Topic>, val professions: List<Profession> = emptyList())

/** The api's error body (`Error` in api/openapi.yaml). */
@Serializable
internal data class ApiError(val code: String, val message: String)

@Serializable
data class Me(
    val id: Long,
    val email: String? = null,
    val topics: List<String>,
    val professions: List<String> = emptyList(),
    /** ISO 3166-1 alpha-2 code the user chose; null until set. */
    val country: String? = null,
    val stats: MeStats = MeStats(),
)

@Serializable
data class MeStats(val saved: Long = 0, val read: Long = 0)

@Serializable
data class PutTopicsRequest(
    @SerialName("topics") val topics: List<String>,
)

@Serializable
data class PutProfessionsRequest(
    @SerialName("professions") val professions: List<String>,
)

@Serializable
data class PutCountryRequest(
    @SerialName("country") val country: String?,
)

@Serializable
data class RecordViewsRequest(val ids: List<Long>)

@Serializable
data class SyncBookmarksRequest(val add: List<Long>, val remove: List<Long>)

@Serializable
data class StorySummary(
    val id: Long,
    val title: String,
    val summary: String,
    val kind: String,
    val severity: String? = null,
    val importance: Int,
    @SerialName("published_at") val publishedAt: String,
    val topics: List<String>,
    @SerialName("is_read") val isRead: Boolean,
    @SerialName("read_at") val readAt: String? = null,
    @SerialName("is_bookmarked") val isBookmarked: Boolean = false,
    /** Timeline only: `followed`, `profession`, `related` or `explore`; null elsewhere. */
    val match: String? = null,
)

@Serializable
data class StorySource(val url: String, val name: String)

@Serializable
data class Story(
    val id: Long,
    val title: String,
    val summary: String,
    val kind: String,
    val severity: String? = null,
    val importance: Int,
    @SerialName("published_at") val publishedAt: String,
    val topics: List<String>,
    @SerialName("is_read") val isRead: Boolean,
    @SerialName("read_at") val readAt: String? = null,
    @SerialName("body_md") val bodyMd: String,
    val sources: List<StorySource>,
    @SerialName("is_bookmarked") val isBookmarked: Boolean = false,
)

@Serializable
data class TimelinePage(
    val items: List<StorySummary>,
    @SerialName("next_cursor") val nextCursor: String? = null,
)

@Serializable
data class RegisterDeviceRequest(val token: String, val platform: String)

@Serializable
data class CreateTopicRequest(val text: String)

@Serializable
data class TopicRequest(
    val id: Long,
    val text: String,
    /** `pending`, `accepted` (a new topic was created), `merged` (mapped to an existing topic) or `rejected`. */
    val status: String,
    val topic: String? = null,
    val note: String? = null,
    @SerialName("created_at") val createdAt: String,
    @SerialName("resolved_at") val resolvedAt: String? = null,
)

@Serializable
data class TopicRequestList(val items: List<TopicRequest>)
