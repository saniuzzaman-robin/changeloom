package dev.changeloom.android

import android.content.Intent
import android.graphics.Color
import android.os.Bundle
import android.os.SystemClock
import android.view.animation.AnimationUtils
import androidx.activity.ComponentActivity
import androidx.activity.SystemBarStyle
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.Surface
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.ExperimentalComposeUiApi
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.testTagsAsResourceId
import androidx.core.splashscreen.SplashScreen
import androidx.core.splashscreen.SplashScreen.Companion.installSplashScreen
import androidx.lifecycle.lifecycleScope
import dev.changeloom.android.ads.ConsentManager
import dev.changeloom.android.auth.SessionManager
import dev.changeloom.android.play.InAppUpdater
import dev.changeloom.android.play.UpdateReadyBanner
import dev.changeloom.android.push.DeviceRegistrar
import dev.changeloom.android.push.EXTRA_STORY_ID
import dev.changeloom.android.telemetry.Analytics
import dev.changeloom.android.ui.AppRoot
import dev.changeloom.android.ui.theme.ChangeloomTheme
import dev.changeloom.android.ui.theme.ThemePreferences
import dev.changeloom.android.ui.theme.isDark
import dev.changeloom.shared.auth.AuthRepository
import dev.changeloom.shared.data.TimelineRepository
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.koin.android.ext.android.inject

private const val SPLASH_EXIT_MS = 500L
private const val SPLASH_EXIT_SCALE = 1.15f

/** The weave's length (windowSplashScreenAnimationDuration); the exit never waits longer than this for it. */
private const val SPLASH_ICON_MAX_WAIT_MS = 900L

/** Longest the splash waits for the first screen; after that the in-app loading mark shows instead. */
private const val SPLASH_MAX_MS = 1_500L

class MainActivity : ComponentActivity() {
    private val auth: AuthRepository by inject()
    private val registrar: DeviceRegistrar by inject()
    private val themePreferences: ThemePreferences by inject()
    private val session: SessionManager by inject()
    private val analytics: Analytics by inject()
    private val consent: ConsentManager by inject()
    private val updater: InAppUpdater by inject()
    private val timeline: TimelineRepository by inject()

    /** Set once AppRoot shows a real screen (sign-in, onboarding or the feed); the splash stays up until then. */
    @Volatile private var contentReady = false

    /** Story to open from a tapped push notification, until the UI has consumed it. */
    private val pendingStoryId = MutableStateFlow<Long?>(null)

    /** Play's update confirmation; declining just skips this version until the next launch. */
    private val updateFlow = registerForActivityResult(ActivityResultContracts.StartIntentSenderForResult()) {}

    override fun onCreate(savedInstanceState: Bundle?) {
        val splash = installSplashScreen()
        super.onCreate(savedInstanceState)
        val splashDeadline = SystemClock.uptimeMillis() + SPLASH_MAX_MS
        splash.setKeepOnScreenCondition { !contentReady && SystemClock.uptimeMillis() < splashDeadline }
        animateSplashExit(splash)
        // A recreated activity still holds the launch intent; its notification was already handled.
        if (savedInstanceState == null) readStoryId(intent)
        enableEdgeToEdge()
        lifecycleScope.launch { updater.check(updateFlow) }
        setContent {
            val mode by themePreferences.mode.collectAsState()
            val dark = mode.isDark()
            DisposableEffect(dark) {
                val bars = if (dark) {
                    SystemBarStyle.dark(Color.TRANSPARENT)
                } else {
                    SystemBarStyle.light(Color.TRANSPARENT, Color.TRANSPARENT)
                }
                enableEdgeToEdge(statusBarStyle = bars, navigationBarStyle = bars)
                onDispose {}
            }
            ChangeloomTheme(mode) {
                // Test tags double as resource ids for the macrobenchmarks in :baselineprofile.
                @OptIn(ExperimentalComposeUiApi::class)
                Surface(Modifier.fillMaxSize().semantics { testTagsAsResourceId = true }) {
                    val user by auth.currentUser.collectAsState()
                    val storyId by pendingStoryId.collectAsState()
                    val updateReady by updater.readyToInstall.collectAsState()
                    val signedIn = user != null
                    LaunchedEffect(signedIn) {
                        if (signedIn) registrar.register()
                    }
                    Box {
                        AppRoot(
                            userId = user?.uid,
                            onSignOut = session::signOut,
                            openStoryId = storyId,
                            onOpenStoryHandled = { pendingStoryId.value = null },
                            onContentReady = ::onContentReady,
                        )
                        if (updateReady) UpdateReadyBanner(onInstall = updater::install, Modifier.align(Alignment.TopCenter))
                    }
                }
            }
        }
    }

    /** The app is closing or leaving the foreground: sync bookmark changes made since the last sync, if any. */
    override fun onStop() {
        super.onStop()
        lifecycleScope.launch { withContext(NonCancellable) { timeline.flushBookmarks() } }
    }

    private fun onContentReady() {
        if (contentReady) return
        contentReady = true
        // Only now, so the consent form (where one is needed) never holds up the first screen.
        consent.gather(this)
    }

    /** Lets the splash mark finish weaving (API 31+), then lifts it away: a slight zoom while the splash fades out. */
    private fun animateSplashExit(splash: SplashScreen) {
        splash.setOnExitAnimationListener { provider ->
            // Both are 0 when the icon doesn't animate (before API 31). Capped, so an odd start time never holds the
            // splash over a ready app.
            val end = provider.iconAnimationStartMillis + provider.iconAnimationDurationMillis
            val delay = (end - System.currentTimeMillis()).coerceIn(0L, SPLASH_ICON_MAX_WAIT_MS)
            val easing = AnimationUtils.loadInterpolator(this, R.interpolator.ease_out_expo)
            // The icon fades itself: on API 31+ it is drawn in its own surface, which the splash view's fade doesn't
            // reach, so it would otherwise stay on top of the app until the splash is removed.
            provider.iconView.animate().alpha(0f).scaleX(SPLASH_EXIT_SCALE).scaleY(SPLASH_EXIT_SCALE)
                .setStartDelay(delay).setDuration(SPLASH_EXIT_MS).setInterpolator(easing).start()
            provider.view.animate().alpha(0f).setStartDelay(delay).setDuration(SPLASH_EXIT_MS).setInterpolator(easing)
                .withEndAction(provider::remove).start()
        }
    }

    override fun onResume() {
        super.onResume()
        lifecycleScope.launch { updater.refresh() }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        readStoryId(intent)
    }

    private fun readStoryId(intent: Intent?) {
        intent?.getStringExtra(EXTRA_STORY_ID)?.toLongOrNull()?.let {
            analytics.notificationOpen(it)
            pendingStoryId.value = it
        }
    }
}
