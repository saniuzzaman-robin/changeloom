package dev.changeloom.shared.data

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable
data class Topic(
    val slug: String,
    val name: String,
    val parent: String? = null,
    val description: String,
    /** Important stories in a headline topic (or below it) reach every user's timeline. */
    val headline: Boolean = false,
)

@Serializable
data class Profession(
    val slug: String,
    val name: String,
    val description: String = "",
    /** Root topic slugs (areas) this profession suggests, in display order. */
    val topics: List<String> = emptyList(),
    /** False when not yet offered at onboarding; it stays valid for users who already picked it. */
    val launched: Boolean = true,
)

@Serializable
data class TopicList(val items: List<Topic>, val professions: List<Profession> = emptyList())

/** Topics near the followed ones that the user might follow, best first (`GET /v1/me/topic-suggestions`). */
@Serializable
data class TopicSuggestions(val items: List<Topic>)

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
    @SerialName("muted_topics") val mutedTopics: List<String> = emptyList(),
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
    /** Up to 3 short takeaways; empty when the story has none or the server predates them. */
    @SerialName("key_points") val keyPoints: List<String> = emptyList(),
    /** Readers must upgrade, migrate or patch. */
    @SerialName("action_required") val actionRequired: Boolean = false,
    /**
     * Timeline only: the story was on screen before the page's time, on any device. Lists that don't send it (and
     * older servers) default to true, so nothing is marked new there.
     */
    val seen: Boolean = true,
    /** When Changeloom added the story, which can be days after [publishedAt]; null from older servers. */
    @SerialName("added_at") val addedAt: String? = null,
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
    @SerialName("key_points") val keyPoints: List<String> = emptyList(),
    @SerialName("action_required") val actionRequired: Boolean = false,
    /** The release this story is about (e.g. "1.25.0"); null when it has none. */
    val version: String? = null,
    /** Other recent stories about the same project or CVE, newest first; empty when there are none. */
    val thread: List<StoryThreadItem> = emptyList(),
    /** When Changeloom added the story, which can be days after [publishedAt]; null from older servers. */
    @SerialName("added_at") val addedAt: String? = null,
)

/** A story in another story's thread: the summary fields the thread shows, plus its release. */
@Serializable
data class StoryThreadItem(
    val id: Long,
    val title: String,
    val kind: String,
    val severity: String? = null,
    @SerialName("published_at") val publishedAt: String,
    @SerialName("is_read") val isRead: Boolean = false,
    @SerialName("action_required") val actionRequired: Boolean = false,
    val version: String? = null,
)

@Serializable
data class TimelinePage(
    val items: List<StorySummary>,
    @SerialName("next_cursor") val nextCursor: String? = null,
    /** Timeline first page only: unread stories matching the filter that weren't on screen before. */
    @SerialName("unseen_count") val unseenCount: Long? = null,
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

@Serializable
data class CreateFeedback(
    /** `bug`, `idea`, `praise` or `other`. */
    val category: String,
    val message: String,
    /** Optional 1-5 star rating. */
    val rating: Int? = null,
    @SerialName("app_version") val appVersion: String? = null,
)
