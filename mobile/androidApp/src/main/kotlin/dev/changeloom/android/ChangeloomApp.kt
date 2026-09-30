package dev.changeloom.android

import android.app.Application
import dev.changeloom.android.auth.FirebaseAuthRepository
import dev.changeloom.android.data.FileStoryCache
import dev.changeloom.android.push.DeviceRegistrar
import dev.changeloom.android.push.createNotificationChannel
import dev.changeloom.android.ui.BookmarksViewModel
import dev.changeloom.android.ui.OnboardingViewModel
import dev.changeloom.android.ui.SearchViewModel
import dev.changeloom.android.ui.SettingsViewModel
import dev.changeloom.android.ui.SignInViewModel
import dev.changeloom.android.ui.StoryDetailViewModel
import dev.changeloom.android.ui.TimelineViewModel
import dev.changeloom.shared.auth.AuthRepository
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
        createNotificationChannel(this)
        startKoin {
            androidContext(this@ChangeloomApp)
            modules(
                sharedModule(BuildConfig.API_BASE_URL, OkHttp.create()),
                module {
                    single { FirebaseAuthRepository() } bind AuthRepository::class
                    viewModelOf(::SignInViewModel)
                    single { FileStoryCache(File(androidContext().filesDir, "changeloom")) } bind StoryCache::class
                    viewModelOf(::OnboardingViewModel)
                    viewModelOf(::TimelineViewModel)
                    single { DeviceRegistrar(get()) }
                    viewModelOf(::SettingsViewModel)
                    viewModelOf(::SearchViewModel)
                    viewModelOf(::BookmarksViewModel)
                    viewModel { (id: Long) -> StoryDetailViewModel(id, get()) }
                },
            )
        }
    }
}
