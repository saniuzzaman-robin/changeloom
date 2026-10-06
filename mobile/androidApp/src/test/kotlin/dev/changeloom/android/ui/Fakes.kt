package dev.changeloom.android.ui

import android.content.ContextWrapper
import android.content.SharedPreferences
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dev.changeloom.android.telemetry.Analytics
import dev.changeloom.android.telemetry.AnalyticsConsent
import dev.changeloom.shared.auth.AuthRepository
import dev.changeloom.shared.auth.AuthUser
import dev.changeloom.shared.auth.ReauthRequiredException
import dev.changeloom.shared.auth.SignInMethod
import dev.changeloom.shared.data.ChangeloomApi
import dev.changeloom.shared.data.Story
import dev.changeloom.shared.data.StoryCache
import dev.changeloom.shared.data.StorySummary
import io.ktor.client.engine.mock.MockEngine
import io.ktor.client.engine.mock.MockRequestHandleScope
import io.ktor.client.request.HttpRequestData
import io.ktor.client.request.HttpResponseData
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.job
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeout

private const val AWAIT_TIMEOUT_MS = 5_000L

/** Waits, in real time, for a state the api calls lead to: Ktor's engine finishes off the test scheduler. */
suspend fun <T> StateFlow<T>.await(predicate: (T) -> Boolean): T =
    withContext(Dispatchers.Default) { withTimeout(AWAIT_TIMEOUT_MS) { first(predicate) } }

/**
 * Cancels the view model's work and waits for it to stop. Call it before a test ends when requests may still be in
 * flight: MainDispatcherRule resets Dispatchers.Main afterwards, and a coroutine resuming on it then fails whichever
 * test runs next.
 */
suspend fun ViewModel.stop() = viewModelScope.coroutineContext.job.cancelAndJoin()

/** Resource lookups in tests just name the id, so assertions don't depend on the English text. */
val testStrings = Strings { id, args -> "string:$id" + args.joinToString(prefix = "(", postfix = ")").takeIf { args.isNotEmpty() }.orEmpty() }

class FakeAuth(method: SignInMethod = SignInMethod.Password) : AuthRepository {
    override val currentUser = MutableStateFlow<AuthUser?>(AuthUser("uid", "ada@example.com", signInMethod = method))
    var needsReauth = false
    var reauthFails: Exception? = null
    /** Exceptions [deleteUser] throws, one per call, before it starts succeeding. */
    val deleteFailures = ArrayDeque<Exception>()
    val calls = mutableListOf<String>()

    override suspend fun idToken(forceRefresh: Boolean) = "tok"
    override suspend fun signInWithEmail(email: String, password: String) {
        calls += "signIn:$email:$password"
    }
    override suspend fun registerWithEmail(email: String, password: String) {
        calls += "register:$email:$password"
    }
    override suspend fun signInWithGoogleIdToken(googleIdToken: String) = false
    override fun signOut() {
        currentUser.value = null
    }
    override fun needsReauth() = needsReauth
    override suspend fun reauthenticateWithPassword(password: String) {
        reauthFails?.let { throw it }
        calls += "reauthPassword:$password"
        needsReauth = false
    }
    override suspend fun reauthenticateWithGoogleIdToken(googleIdToken: String) {
        reauthFails?.let { throw it }
        calls += "reauthGoogle:$googleIdToken"
        needsReauth = false
    }
    override suspend fun deleteUser() {
        deleteFailures.removeFirstOrNull()?.let { throw it }
        calls += "deleteUser"
        currentUser.value = null
    }
}

fun reauthRequired() = ReauthRequiredException()

object NoAnalytics : Analytics {
    override fun screenView(name: String) = Unit
    override fun login(method: String) = Unit
    override fun signUp(method: String) = Unit
    override fun storyOpen(storyId: Long) = Unit
    override fun bookmarkAdd(storyId: Long) = Unit
    override fun share(storyId: Long) = Unit
    override fun search() = Unit
    override fun topicsUpdate(count: Int, onboarding: Boolean) = Unit
    override fun topicRequest() = Unit
    override fun notificationOpen(storyId: Long) = Unit
    override fun setConsent(consent: AnalyticsConsent) = Unit
}

class MemoryCache : StoryCache {
    var cleared = false
    override suspend fun loadTimeline() = emptyList<StorySummary>()
    override suspend fun saveTimeline(items: List<StorySummary>) = Unit
    override suspend fun loadStory(id: Long): Story? = null
    override suspend fun saveStory(story: Story) = Unit
    override suspend fun loadFollowed() = emptyList<String>()
    override suspend fun saveFollowed(slugs: List<String>) = Unit
    override suspend fun clear() {
        cleared = true
    }
}

/** An api backed by [handler]; every request is also recorded in [requests] as "METHOD /path?query". */
fun fakeApi(
    auth: AuthRepository,
    requests: MutableList<String> = mutableListOf(),
    handler: suspend MockRequestHandleScope.(HttpRequestData) -> HttpResponseData,
): ChangeloomApi {
    val engine = MockEngine { request ->
        requests += "${request.method.value} ${request.url.encodedPathAndQuery}"
        handler(request)
    }
    return ChangeloomApi(ChangeloomApi.createClient("http://api.test", engine), auth)
}

/** Just enough Context for ThemePreferences: in-memory shared preferences. */
class PrefsContext : ContextWrapper(null) {
    private val prefs = MemoryPrefs()
    override fun getSharedPreferences(name: String?, mode: Int): SharedPreferences = prefs
}

private class MemoryPrefs : SharedPreferences {
    private val values = mutableMapOf<String, Any?>()
    override fun getAll(): MutableMap<String, *> = values
    override fun getString(key: String, defValue: String?) = values[key] as String? ?: defValue
    @Suppress("UNCHECKED_CAST")
    override fun getStringSet(key: String, defValues: MutableSet<String>?) = values[key] as MutableSet<String>? ?: defValues
    override fun getInt(key: String, defValue: Int) = values[key] as Int? ?: defValue
    override fun getLong(key: String, defValue: Long) = values[key] as Long? ?: defValue
    override fun getFloat(key: String, defValue: Float) = values[key] as Float? ?: defValue
    override fun getBoolean(key: String, defValue: Boolean) = values[key] as Boolean? ?: defValue
    override fun contains(key: String) = key in values
    override fun registerOnSharedPreferenceChangeListener(listener: SharedPreferences.OnSharedPreferenceChangeListener?) = Unit
    override fun unregisterOnSharedPreferenceChangeListener(listener: SharedPreferences.OnSharedPreferenceChangeListener?) = Unit
    override fun edit(): SharedPreferences.Editor = object : SharedPreferences.Editor {
        override fun putString(key: String, value: String?) = apply { values[key] = value }
        override fun putStringSet(key: String, values: MutableSet<String>?) = apply { this@MemoryPrefs.values[key] = values?.toMutableSet() }
        override fun putInt(key: String, value: Int) = apply { this@MemoryPrefs.values[key] = value }
        override fun putLong(key: String, value: Long) = apply { this@MemoryPrefs.values[key] = value }
        override fun putFloat(key: String, value: Float) = apply { this@MemoryPrefs.values[key] = value }
        override fun putBoolean(key: String, value: Boolean) = apply { this@MemoryPrefs.values[key] = value }
        override fun remove(key: String) = apply { values.remove(key) }
        override fun clear() = apply { values.clear() }
        override fun commit() = true
        override fun apply() = Unit
    }
}
