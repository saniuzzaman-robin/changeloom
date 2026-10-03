package dev.changeloom.android.push

import android.Manifest
import android.content.pm.PackageManager
import android.os.Build
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.core.content.ContextCompat
import dev.changeloom.android.R
import dev.changeloom.android.play.AppPreferences
import dev.changeloom.android.ui.components.TextAction
import dev.changeloom.android.ui.theme.ChangeloomTheme
import dev.changeloom.android.ui.theme.Radius
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
    val c = ChangeloomTheme.colors
    AlertDialog(
        onDismissRequest = { answered(false) },
        confirmButton = { TextAction(stringResource(R.string.notifications_allow), onClick = { answered(true) }) },
        dismissButton = { TextAction(stringResource(R.string.not_now), onClick = { answered(false) }, color = c.fgMuted) },
        title = { Text(stringResource(R.string.notifications_rationale_title)) },
        text = { Text(stringResource(R.string.notifications_rationale_body)) },
        shape = Radius.xxl,
        containerColor = c.elevated,
        titleContentColor = c.fg,
        textContentColor = c.fgMuted,
    )
}
