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
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import org.koin.android.ext.android.inject
import java.security.MessageDigest

private const val TAG = "Push"
private const val PLATFORM = "android"
private const val UNREGISTER_TIMEOUT_MS = 5_000L

/** A registration the server already has is sent again after this long, in case the server dropped it. */
internal const val REREGISTER_AFTER_MS = 24 * 60 * 60 * 1000L

/** Intent extra (and FCM data key) holding the id of the story a notification is about. */
const val EXTRA_STORY_ID = "story_id"

/** The last registration the server accepted: a hash of (account, token) and when, in [now] millis. */
interface RegistrationStore {
    var registeredKey: String?
    var registeredAt: Long
}

/**
 * Registers this device's FCM token with the API. Failures are logged: push is best-effort. A registration is sent
 * only when the (account, token) pair changed or the last one is over [REREGISTER_AFTER_MS] old, not on every launch.
 *
 * FirebaseMessaging.getToken() is deprecated in favour of register()/FIDs, which also needs api, schema and
 * manifest changes (and the backend sender on FIDs), so it is suppressed until that migration is done.
 */
@Suppress("DEPRECATION")
class DeviceRegistrar(
    private val api: ChangeloomApi,
    private val auth: AuthRepository,
    private val store: RegistrationStore,
    private val now: () -> Long = System::currentTimeMillis,
    private val fcmToken: suspend () -> String = { FirebaseMessaging.getInstance().token.await() },
) {
    /** Registers [token], or the current FCM token when null, for the signed-in account. */
    suspend fun register(token: String? = null) {
        try {
            val uid = auth.currentUser.value?.uid ?: return
            val fcm = token ?: fcmToken()
            val key = registrationKey(uid, fcm)
            // Off the main thread: the first preferences read loads the file.
            val fresh = withContext(Dispatchers.IO) {
                store.registeredKey == key && now() - store.registeredAt in 0 until REREGISTER_AFTER_MS
            }
            if (fresh) return
            api.registerDevice(fcm, PLATFORM)
            withContext(Dispatchers.IO) {
                store.registeredKey = key
                store.registeredAt = now()
            }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            AppLog.w(TAG, "Could not register device for push", e)
        }
    }

    /** Call while still signed in. Bounded wait so sign-out is not held up by a bad connection. */
    suspend fun unregister() {
        try {
            // Forgotten first: whatever the call's outcome, the next sign-in registers again.
            withContext(Dispatchers.IO) { store.registeredKey = null }
            withTimeoutOrNull(UNREGISTER_TIMEOUT_MS) {
                api.unregisterDevice(fcmToken())
            } ?: AppLog.w(TAG, "Timed out unregistering device for push")
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            AppLog.w(TAG, "Could not unregister device for push", e)
        }
    }
}

/** Hashed, so the account id and push token aren't stored in preferences in the clear. */
private fun registrationKey(uid: String, token: String): String =
    MessageDigest.getInstance("SHA-256").digest("$uid\n$token".toByteArray()).joinToString("") { (it.toInt() and 0xff).toString(16).padStart(2, '0') }

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

    @Suppress("OVERRIDE_DEPRECATION") // still the only token callback in firebase-messaging 25.x
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
