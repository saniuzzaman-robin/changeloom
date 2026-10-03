package dev.changeloom.android.play

import android.content.Context
import androidx.activity.result.ActivityResultLauncher
import androidx.activity.result.IntentSenderRequest
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBars
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.SystemUpdate
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.google.android.play.core.appupdate.AppUpdateManagerFactory
import com.google.android.play.core.appupdate.AppUpdateOptions
import com.google.android.play.core.install.InstallStateUpdatedListener
import com.google.android.play.core.install.model.AppUpdateType
import com.google.android.play.core.install.model.InstallStatus
import com.google.android.play.core.install.model.UpdateAvailability
import dev.changeloom.android.R
import dev.changeloom.android.telemetry.AppLog
import dev.changeloom.android.ui.components.StatusBanner
import dev.changeloom.android.ui.theme.ChangeloomTheme
import dev.changeloom.android.ui.theme.Radius
import dev.changeloom.android.ui.theme.Spacing
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.tasks.await

private const val TAG = "InAppUpdate"

/**
 * Play's flexible in-app update: the new version downloads while the app is in use, then [readyToInstall] asks
 * the user to restart into it. Only works for installs from Play; elsewhere the check fails quietly.
 */
class InAppUpdater(context: Context) {
    private val manager = AppUpdateManagerFactory.create(context)
    private val _readyToInstall = MutableStateFlow(false)
    val readyToInstall: StateFlow<Boolean> = _readyToInstall.asStateFlow()
    private var checked = false

    private val listener = InstallStateUpdatedListener { state ->
        when (state.installStatus()) {
            InstallStatus.DOWNLOADED -> _readyToInstall.value = true
            InstallStatus.FAILED -> AppLog.w(TAG, "Update download failed: ${state.installErrorCode()}")
            else -> Unit
        }
    }

    /** Offers an available update once per process. [launcher] shows Play's confirmation. */
    suspend fun check(launcher: ActivityResultLauncher<IntentSenderRequest>) {
        if (checked) return
        checked = true
        try {
            val info = manager.appUpdateInfo.await()
            if (info.installStatus() == InstallStatus.DOWNLOADED) {
                _readyToInstall.value = true
            } else if (info.updateAvailability() == UpdateAvailability.UPDATE_AVAILABLE &&
                info.isUpdateTypeAllowed(AppUpdateType.FLEXIBLE)
            ) {
                manager.registerListener(listener)
                manager.startUpdateFlowForResult(info, launcher, AppUpdateOptions.newBuilder(AppUpdateType.FLEXIBLE).build())
            }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            AppLog.w(TAG, "Couldn't check for an update", e)
        }
    }

    /** On resume: an update may have finished downloading while the app was in the background. */
    suspend fun refresh() {
        try {
            if (manager.appUpdateInfo.await().installStatus() == InstallStatus.DOWNLOADED) _readyToInstall.value = true
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            AppLog.w(TAG, "Couldn't read the update state", e)
        }
    }

    /** Restarts into the downloaded version. */
    fun install() {
        manager.unregisterListener(listener)
        manager.completeUpdate()
    }
}

/** Floats over the app once an update has downloaded; [onInstall] restarts into it. */
@Composable
fun UpdateReadyBanner(onInstall: () -> Unit, modifier: Modifier = Modifier) {
    Box(
        modifier
            .windowInsetsPadding(WindowInsets.statusBars)
            .padding(horizontal = Spacing.gutter, vertical = 8.dp)
            .clip(Radius.xl)
            .background(ChangeloomTheme.colors.elevated),
    ) {
        StatusBanner(
            stringResource(R.string.update_ready),
            Icons.Rounded.SystemUpdate,
            actionLabel = stringResource(R.string.update_restart),
            onAction = onInstall,
        )
    }
}
