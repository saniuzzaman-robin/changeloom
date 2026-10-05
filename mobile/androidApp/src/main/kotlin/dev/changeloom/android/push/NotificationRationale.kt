package dev.changeloom.android.push

import android.Manifest
import android.content.pm.PackageManager
import android.os.Build
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.Notifications
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.core.content.ContextCompat
import dev.changeloom.android.R
import dev.changeloom.android.play.AppPreferences
import dev.changeloom.android.ui.components.ChangeloomDialog
import dev.changeloom.android.ui.components.DialogConfirmButton
import dev.changeloom.android.ui.components.SecondaryButton
import org.koin.compose.koinInject

/**
 * Explains story alerts before Android 13+ asks for the notification permission, once per install. "Not now"
 * skips the system prompt, so a later "Allow" in Settings is still possible (a system denial can stick).
 */
@Composable
fun NotificationRationale(prefs: AppPreferences = koinInject()) {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return
    val context = LocalContext.current
    var show by rememberSaveable {
        mutableStateOf(
            !prefs.notificationsAsked &&
                ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED,
        )
    }
    // Push registration doesn't depend on the answer: the token is registered either way.
    val permission = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) {}
    if (!show) return
    val answered = { allow: Boolean ->
        prefs.notificationsAsked = true
        show = false
        if (allow) permission.launch(Manifest.permission.POST_NOTIFICATIONS)
    }
    ChangeloomDialog(
        onDismiss = { answered(false) },
        title = stringResource(R.string.notifications_rationale_title),
        message = stringResource(R.string.notifications_rationale_body),
        icon = Icons.Rounded.Notifications,
        actions = {
            SecondaryButton(stringResource(R.string.not_now), onClick = { answered(false) }, dense = true, modifier = Modifier.weight(1f))
            DialogConfirmButton(stringResource(R.string.notifications_allow), onClick = { answered(true) }, modifier = Modifier.weight(1f))
        },
    )
}
