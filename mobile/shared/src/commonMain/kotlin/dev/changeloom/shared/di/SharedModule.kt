package dev.changeloom.shared.di

import dev.changeloom.shared.data.ChangeloomApi
import dev.changeloom.shared.data.TimelineRepository
import dev.changeloom.shared.data.ViewTracker
import io.ktor.client.engine.HttpClientEngine
import org.koin.dsl.module

/**
 * [baseUrl] and [engine] are platform-provided; [engine] is called on the api's first request. `AuthRepository` and
 * `StoryCache` must be bound by the platform module; `AppCheckTokens` may be.
 */
fun sharedModule(baseUrl: String, engine: () -> HttpClientEngine) = module {
    single { ChangeloomApi(lazy { ChangeloomApi.createClient(baseUrl, engine()) }, get(), getOrNull()) }
    single { TimelineRepository(get(), get()) }
    // One per signed-in session (it remembers what it has reported), so a factory, not a single.
    factory { get<ChangeloomApi>().let { api -> ViewTracker { ids -> api.recordViews(ids) } } }
}
