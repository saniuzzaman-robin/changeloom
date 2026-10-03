package dev.changeloom.android

import android.app.Application
import android.os.StrictMode
import dev.changeloom.android.ads.AdsConfig
import dev.changeloom.android.ads.ConsentManager
import dev.changeloom.android.ads.NativeAdRepository
import dev.changeloom.android.auth.FirebaseAppCheckTokens
import dev.changeloom.android.auth.FirebaseAuthRepository
import dev.changeloom.android.auth.SessionManager
import dev.changeloom.android.data.FileStoryCache
import dev.changeloom.android.push.DeviceRegistrar
import dev.changeloom.android.play.AppPreferences
import dev.changeloom.android.play.InAppUpdater
import dev.changeloom.android.play.ReviewPrompter
import dev.changeloom.android.push.createNotificationChannels
import dev.changeloom.android.telemetry.Analytics
import dev.changeloom.android.telemetry.FirebaseAnalyticsTracker
import dev.changeloom.android.ui.AndroidStrings
import dev.changeloom.android.ui.BookmarksViewModel
import dev.changeloom.android.ui.ProfileViewModel
import dev.changeloom.android.ui.SearchViewModel
import dev.changeloom.android.ui.SignInViewModel
import dev.changeloom.android.ui.StoryDetailViewModel
import dev.changeloom.android.ui.Strings
import dev.changeloom.android.ui.TimelineViewModel
import dev.changeloom.android.ui.TopicCatalog
import dev.changeloom.android.ui.TopicPickerViewModel
import dev.changeloom.android.ui.theme.ThemePreferences
import dev.changeloom.shared.auth.AuthRepository
import dev.changeloom.shared.data.AppCheckTokens
import dev.changeloom.shared.data.StoryCache
import dev.changeloom.shared.di.sharedModule
import io.ktor.client.engine.okhttp.OkHttp
import org.koin.android.ext.koin.androidContext
import org.koin.core.context.startKoin
import org.koin.core.module.dsl.viewModelOf
import org.koin.core.module.dsl.viewModel
import org.koin.dsl.bind
import org.koin.dsl.module
import java.io.File

class ChangeloomApp : Application() {
    override fun onCreate() {
        super.onCreate()
        if (BuildConfig.DEBUG) enableStrictMode()
        createNotificationChannels(this)
        startKoin {
            androidContext(this@ChangeloomApp)
            modules(
                sharedModule(BuildConfig.API_BASE_URL) { OkHttp.create() },
                module {
                    single { FirebaseAuthRepository() } bind AuthRepository::class
                    single { FirebaseAppCheckTokens() } bind AppCheckTokens::class
                    single { AndroidStrings(androidContext()) } bind Strings::class
                    single { AppPreferences(androidContext()) }
                    single { ReviewPrompter(get<AppPreferences>()) }
                    single { InAppUpdater(androidContext()) }
                    single { ThemePreferences(androidContext()) }
                    single { FirebaseAnalyticsTracker(androidContext()) } bind Analytics::class
                    single { ConsentManager(androidContext(), get()) }
                    single { AdsConfig() }
                    single { NativeAdRepository(androidContext(), BuildConfig.ADMOB_NATIVE_AD_UNIT_ID, get(), get()) }
                    // Created at start so it is listening for expired sessions before the first request.
                    single(createdAtStart = true) { SessionManager(get(), get(), get(), get()) }
                    viewModelOf(::SignInViewModel)
                    single { FileStoryCache(File(androidContext().filesDir, "changeloom")) } bind StoryCache::class
                    single { TopicCatalog(get()) }
                    viewModelOf(::TopicPickerViewModel)
                    viewModelOf(::TimelineViewModel)
                    single { DeviceRegistrar(get()) }
                    viewModelOf(::ProfileViewModel)
                    viewModelOf(::SearchViewModel)
                    viewModelOf(::BookmarksViewModel)
                    viewModel { (id: Long) -> StoryDetailViewModel(id, get(), get(), get(), get()) }
                },
            )
        }
    }

    /** Debug only: logs main-thread disk/network access and leaked resources (tag StrictMode). */
    private fun enableStrictMode() {
        StrictMode.setThreadPolicy(StrictMode.ThreadPolicy.Builder().detectAll().penaltyLog().build())
        StrictMode.setVmPolicy(StrictMode.VmPolicy.Builder().detectAll().penaltyLog().build())
    }
}
