package dev.changeloom.android.auth

import dev.changeloom.android.data.FollowSuggester
import dev.changeloom.android.push.DeviceRegistrar
import dev.changeloom.android.telemetry.AppLog
import dev.changeloom.shared.auth.AuthRepository
import dev.changeloom.shared.data.ChangeloomApi
import dev.changeloom.shared.data.TimelineRepository
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex

private const val TAG = "Session"

/**
 * The one way to sign out. It runs in its own scope, so it finishes even when the screen that asked
 * for it (and its ViewModels) goes away. It also signs out when the api rejects a refreshed token.
 */
class SessionManager(
    private val auth: AuthRepository,
    private val registrar: DeviceRegistrar,
    private val repo: TimelineRepository,
    private val api: ChangeloomApi,
    private val suggester: FollowSuggester,
    private val scope: CoroutineScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate),
) {
    private val signingOut = Mutex()

    init {
        scope.launch {
            api.sessionExpired.collect {
                AppLog.w(TAG, "The api rejected a refreshed token; signing out")
                signOut()
            }
        }
    }

    /**
     * Stops pushes for this device while still signed in, clears the local cache so another account never
     * sees this user's data, then signs out. A sign-out already in progress makes this a no-op.
     */
    fun signOut() {
        if (!signingOut.tryLock()) return
        scope.launch {
            try {
                if (auth.currentUser.value != null) {
                    registrar.unregister()
                    repo.clear()
                    suggester.clear()
                    auth.signOut()
                }
            } finally {
                signingOut.unlock()
            }
        }
    }

    /**
     * Deletes the user's data on the server (devices included), the local cache, then the sign-in account, which
     * signs out. Throws ReauthRequiredException when the sign-in is too old: re-authenticate and call it again (the
     * server delete is safe to repeat). It runs in this scope, so leaving the screen doesn't stop it halfway.
     */
    suspend fun deleteAccount() = scope.async {
        api.deleteMe()
        repo.clear()
        suggester.clear()
        auth.deleteUser()
    }.await()
}
