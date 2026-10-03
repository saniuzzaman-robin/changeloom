package dev.changeloom.android.push

import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import com.google.firebase.messaging.FirebaseMessaging
import com.google.firebase.messaging.FirebaseMessagingService
import com.google.firebase.messaging.RemoteMessage
import dev.changeloom.android.MainActivity
import dev.changeloom.android.R
import dev.changeloom.android.telemetry.AppLog
import dev.changeloom.shared.auth.AuthRepository
import dev.changeloom.shared.data.ChangeloomApi
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch
import kotlinx.coroutines.tasks.await
import kotlinx.coroutines.withTimeoutOrNull
import org.koin.android.ext.android.inject

private const val TAG = "Push"
private const val PLATFORM = "android"
private const val UNREGISTER_TIMEOUT_MS = 5_000L

/** Intent extra (and FCM data key) holding the id of the story a notification is about. */
const val EXTRA_STORY_ID = "story_id"

/**
 * Registers this device's FCM token with the API. Failures are logged: push is best-effort.
 *
 * FirebaseMessaging.getToken() is deprecated in favour of register()/FIDs, which also needs api, schema and
 * manifest changes (and the backend sender on FIDs), so it is suppressed until that migration is done.
 */
@Suppress("DEPRECATION")
class DeviceRegistrar(private val api: ChangeloomApi) {
    /** Registers [token], or the current FCM token when null. */
    suspend fun register(token: String? = null) {
        try {
            api.registerDevice(token ?: FirebaseMessaging.getInstance().token.await(), PLATFORM)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            AppLog.w(TAG, "Could not register device for push", e)
        }
    }

    /** Call while still signed in. Bounded wait so sign-out is not held up by a bad connection. */
    suspend fun unregister() {
        try {
            withTimeoutOrNull(UNREGISTER_TIMEOUT_MS) {
                api.unregisterDevice(FirebaseMessaging.getInstance().token.await())
            } ?: AppLog.w(TAG, "Timed out unregistering device for push")
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            AppLog.w(TAG, "Could not unregister device for push", e)
        }
    }
}

/** The channel early builds used for every push; [createNotificationChannels] replaces it. */
private const val LEGACY_CHANNEL_ID = "security_alerts"

/**
 * Three channels, so users can silence the routine ones and keep the urgent ones: story alerts (high importance,
 * pops up; the api sends high-severity security stories here), the daily digest, and story updates (default
 * importance; the fallback for any message without a channel).
 */
fun createNotificationChannels(context: Context) {
    val manager = context.getSystemService(NotificationManager::class.java)
    manager.deleteNotificationChannel(LEGACY_CHANNEL_ID)
    manager.createNotificationChannels(
        listOf(
            NotificationChannel(
                context.getString(R.string.story_alerts_channel_id),
                context.getString(R.string.story_alerts_channel_name),
                NotificationManager.IMPORTANCE_HIGH,
            ).apply { description = context.getString(R.string.story_alerts_channel_description) },
            NotificationChannel(
                context.getString(R.string.daily_digest_channel_id),
                context.getString(R.string.daily_digest_channel_name),
                NotificationManager.IMPORTANCE_DEFAULT,
            ).apply { description = context.getString(R.string.daily_digest_channel_description) },
            NotificationChannel(
                context.getString(R.string.story_updates_channel_id),
                context.getString(R.string.story_updates_channel_name),
                NotificationManager.IMPORTANCE_DEFAULT,
            ).apply { description = context.getString(R.string.story_updates_channel_description) },
        ),
    )
}

class ChangeloomMessagingService : FirebaseMessagingService() {
    private val registrar: DeviceRegistrar by inject()
    private val auth: AuthRepository by inject()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    override fun onNewToken(token: String) {
        if (auth.currentUser.value == null) return // registered after sign-in instead
        scope.launch { registrar.register(token) }
    }

    override fun onDestroy() {
        scope.cancel()
        super.onDestroy()
    }

    /** Only called while the app is in the foreground; in the background FCM shows the notification itself. */
    override fun onMessageReceived(message: RemoteMessage) {
        val storyId = message.data[EXTRA_STORY_ID]
        val open = Intent(this, MainActivity::class.java)
            .addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_CLEAR_TOP)
            .putExtra(EXTRA_STORY_ID, storyId)
        val pending = PendingIntent.getActivity(
            this, storyId.hashCode(), open, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        val channel = message.notification?.channelId ?: getString(R.string.story_updates_channel_id)
        val notification = android.app.Notification.Builder(this, channel)
            .setSmallIcon(R.drawable.ic_stat_changeloom)
            .setContentTitle(message.notification?.title)
            .setContentText(message.notification?.body)
            .setContentIntent(pending)
            .setAutoCancel(true)
            .build()
        getSystemService(NotificationManager::class.java).notify(storyId.hashCode(), notification)
    }
}
