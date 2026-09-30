package dev.changeloom.shared.data

/** Local copy of what the user last saw, so the app opens offline. Implementations must not throw on a missing/corrupt cache. */
interface StoryCache {
    suspend fun loadTimeline(): List<StorySummary>
    suspend fun saveTimeline(items: List<StorySummary>)
    suspend fun loadStory(id: Long): Story?
    suspend fun saveStory(story: Story)
    suspend fun clear()
}
