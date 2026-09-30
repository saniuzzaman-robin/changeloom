package dev.changeloom.android

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.core.content.ContextCompat
import androidx.lifecycle.lifecycleScope
import dev.changeloom.android.push.DeviceRegistrar
import dev.changeloom.android.push.EXTRA_STORY_ID
import dev.changeloom.android.ui.AppRoot
import dev.changeloom.shared.auth.AuthRepository
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch
import org.koin.android.ext.android.inject

class MainActivity : ComponentActivity() {
    private val auth: AuthRepository by inject()
    private val registrar: DeviceRegistrar by inject()

    /** Story to open from a tapped push notification, until the UI has consumed it. */
    private val pendingStoryId = MutableStateFlow<Long?>(null)

    private val notificationPermission =
        registerForActivityResult(ActivityResultContracts.RequestPermission()) { /* push is optional; registration does not depend on it */ }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        readStoryId(intent)
        setContent {
            MaterialTheme {
                Surface {
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
