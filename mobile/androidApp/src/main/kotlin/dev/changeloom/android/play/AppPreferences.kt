package dev.changeloom.android.play

import android.content.Context
import androidx.core.content.edit
import dev.changeloom.android.push.RegistrationStore
import dev.changeloom.android.ui.IntroStore

private const val FILE = "app_prefs"
private const val KEY_STORY_READS = "story_reads"
private const val KEY_LAST_REVIEW_PROMPT = "last_review_prompt_ms"
private const val KEY_NOTIFICATIONS_ASKED = "notifications_asked"
private const val KEY_DEVICE_REGISTERED = "device_registered"
private const val KEY_DEVICE_REGISTERED_AT = "device_registered_at_ms"
private const val KEY_INTRO_SEEN = "intro_seen"

/** Small per-install settings: not tied to an account, so they survive sign-out. */
class AppPreferences(context: Context) : ReviewStore, RegistrationStore, IntroStore {
    private val prefs by lazy { context.getSharedPreferences(FILE, Context.MODE_PRIVATE) }

    override var storyReads: Int
        get() = prefs.getInt(KEY_STORY_READS, 0)
        set(value) = prefs.edit { putInt(KEY_STORY_READS, value) }

    override var lastPromptAt: Long
        get() = prefs.getLong(KEY_LAST_REVIEW_PROMPT, 0L)
        set(value) = prefs.edit { putLong(KEY_LAST_REVIEW_PROMPT, value) }

    /** Set once the notification rationale has been answered, so it isn't shown again. */
    var notificationsAsked: Boolean
        get() = prefs.getBoolean(KEY_NOTIFICATIONS_ASKED, false)
        set(value) = prefs.edit { putBoolean(KEY_NOTIFICATIONS_ASKED, value) }

    override var introSeen: Boolean
        get() = prefs.getBoolean(KEY_INTRO_SEEN, false)
        set(value) = prefs.edit { putBoolean(KEY_INTRO_SEEN, value) }

    override var registeredKey: String?
        get() = prefs.getString(KEY_DEVICE_REGISTERED, null)
        set(value) = prefs.edit { putString(KEY_DEVICE_REGISTERED, value) }

    override var registeredAt: Long
        get() = prefs.getLong(KEY_DEVICE_REGISTERED_AT, 0L)
        set(value) = prefs.edit { putLong(KEY_DEVICE_REGISTERED_AT, value) }
}
