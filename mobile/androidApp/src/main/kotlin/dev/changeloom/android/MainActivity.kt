package dev.changeloom.android

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Color
import android.os.Build
import android.os.Bundle
import android.view.animation.AnimationUtils
import androidx.activity.ComponentActivity
import androidx.activity.SystemBarStyle
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.annotation.RequiresApi
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.Surface
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.core.content.ContextCompat
import androidx.lifecycle.lifecycleScope
import dev.changeloom.android.push.DeviceRegistrar
import dev.changeloom.android.push.EXTRA_STORY_ID
import dev.changeloom.android.ui.AppRoot
import dev.changeloom.android.ui.theme.ChangeloomTheme
import dev.changeloom.android.ui.theme.ThemePreferences
import dev.changeloom.android.ui.theme.isDark
import dev.changeloom.shared.auth.AuthRepository
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch
import org.koin.android.ext.android.inject
import java.time.Duration
import java.time.Instant

private const val SPLASH_EXIT_MS = 500L
private const val SPLASH_EXIT_SCALE = 1.15f

class MainActivity : ComponentActivity() {
    private val auth: AuthRepository by inject()
    private val registrar: DeviceRegistrar by inject()
    private val themePreferences: ThemePreferences by inject()

    /** Story to open from a tapped push notification, until the UI has consumed it. */
    private val pendingStoryId = MutableStateFlow<Long?>(null)

    private val notificationPermission =
        registerForActivityResult(ActivityResultContracts.RequestPermission()) { /* push is optional; registration does not depend on it */ }

    override fun onCreate(savedInstanceState: Bundle?) {
        setTheme(R.style.Theme_Changeloom)
        super.onCreate(savedInstanceState)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) animateSplashExit()
        readStoryId(intent)
        enableEdgeToEdge()
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
                Surface(Modifier.fillMaxSize()) {
                    val user by auth.currentUser.collectAsState()
                    val storyId by pendingStoryId.collectAsState()
                    val signedIn = user != null
                    LaunchedEffect(signedIn) {
                        if (signedIn) {
                            askForNotificationPermission()
                            registrar.register()
                        }
                    }
                    AppRoot(
                        signedIn = signedIn,
                        onSignOut = ::signOut,
                        openStoryId = storyId,
                        onOpenStoryHandled = { pendingStoryId.value = null },
                    )
                }
            }
        }
    }

    /** Lets the splash mark finish weaving, then lifts it away: a slight zoom while the splash fades out. */
    @RequiresApi(Build.VERSION_CODES.S)
    private fun animateSplashExit() {
        splashScreen.setOnExitAnimationListener { view ->
            val start = view.iconAnimationStart
            val duration = view.iconAnimationDuration
            val remaining = if (start != null && duration != null) Duration.between(Instant.now(), start + duration).toMillis() else 0L
            val delay = remaining.coerceAtLeast(0L)
            val easing = AnimationUtils.loadInterpolator(this, R.interpolator.ease_out_expo)
            view.iconView?.animate()?.scaleX(SPLASH_EXIT_SCALE)?.scaleY(SPLASH_EXIT_SCALE)
                ?.setStartDelay(delay)?.setDuration(SPLASH_EXIT_MS)?.setInterpolator(easing)?.start()
            view.animate().alpha(0f).setStartDelay(delay).setDuration(SPLASH_EXIT_MS).setInterpolator(easing)
                .withEndAction(view::remove).start()
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        readStoryId(intent)
    }

    private fun readStoryId(intent: Intent?) {
        intent?.getStringExtra(EXTRA_STORY_ID)?.toLongOrNull()?.let { pendingStoryId.value = it }
    }

    private fun askForNotificationPermission() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) {
            notificationPermission.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
    }

    /** Stops pushes for this device while still signed in, then signs out. */
    private fun signOut() {
        lifecycleScope.launch {
            registrar.unregister()
            auth.signOut()
        }
    }
}
