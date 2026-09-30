package dev.changeloom.android.push

import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.util.Log
import com.google.firebase.messaging.FirebaseMessaging
import com.google.firebase.messaging.FirebaseMessagingService
import com.google.firebase.messaging.RemoteMessage
import dev.changeloom.android.MainActivity
import dev.changeloom.android.R
import dev.changeloom.shared.auth.AuthRepository
import dev.changeloom.shared.data.ChangeloomApi
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import kotlinx.coroutines.tasks.await
import kotlinx.coroutines.withTimeoutOrNull
import org.koin.android.ext.android.inject

private const val TAG = "Push"
private const val PLATFORM = "android"
private const val UNREGISTER_TIMEOUT_MS = 5_000L

/** Intent extra (and FCM data key) holding the id of the story a notification is about. */
const val EXTRA_STORY_ID = "story_id"

/** Registers this device's FCM token with the API. Failures are logged: push is best-effort. */
class DeviceRegistrar(private val api: ChangeloomApi) {
    /** Registers [token], or the current FCM token when null. */
    suspend fun register(token: String? = null) {
        try {
            api.registerDevice(token ?: FirebaseMessaging.getInstance().token.await(), PLATFORM)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            Log.w(TAG, "Could not register device for push", e)
        }
    }

    /** Call while still signed in. Bounded wait so sign-out is not held up by a bad connection. */
    suspend fun unregister() {
        try {
            withTimeoutOrNull(UNREGISTER_TIMEOUT_MS) {
                api.unregisterDevice(FirebaseMessaging.getInstance().token.await())
            } ?: Log.w(TAG, "Timed out unregistering device for push")
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            Log.w(TAG, "Could not unregister device for push", e)
        }
    }
}

fun createNotificationChannel(context: Context) {
    val channel = NotificationChannel(
        context.getString(R.string.security_channel_id),
        context.getString(R.string.security_channel_name),
        NotificationManager.IMPORTANCE_HIGH,
    )
    context.getSystemService(NotificationManager::class.java).createNotificationChannel(channel)
}

class ChangeloomMessagingService : FirebaseMessagingService() {
    private val registrar: DeviceRegistrar by inject()
    private val auth: AuthRepository by inject()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    override fun onNewToken(token: String) {
        if (auth.currentUser.value == null) return // registered after sign-in instead
        scope.launch { registrar.register(token) }
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
        val notification = android.app.Notification.Builder(this, getString(R.string.security_channel_id))
            .setSmallIcon(android.R.drawable.stat_notify_error)
            .setContentTitle(message.notification?.title)
            .setContentText(message.notification?.body)
            .setContentIntent(pending)
            .setAutoCancel(true)
            .build()
        getSystemService(NotificationManager::class.java).notify(storyId.hashCode(), notification)
    }
}
