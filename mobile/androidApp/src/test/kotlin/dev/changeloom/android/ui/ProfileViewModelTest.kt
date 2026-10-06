package dev.changeloom.android.ui

import dev.changeloom.android.auth.SessionManager
import dev.changeloom.android.data.FollowSuggester
import dev.changeloom.android.push.DeviceRegistrar
import dev.changeloom.android.ui.theme.ThemePreferences
import dev.changeloom.shared.auth.SignInMethod
import dev.changeloom.shared.data.TimelineRepository
import io.ktor.client.engine.mock.MockRequestHandleScope
import io.ktor.client.engine.mock.respond
import io.ktor.client.engine.mock.respondError
import io.ktor.client.request.HttpRequestData
import io.ktor.client.request.HttpResponseData
import io.ktor.http.HttpHeaders
import io.ktor.http.HttpStatusCode
import io.ktor.http.headersOf
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.runTest
import org.junit.Rule
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue

class ProfileViewModelTest {
    @get:Rule val main = MainDispatcherRule()

    /** [scope] stands in for the app scope: a supervisor, like SessionManager's default, so a failed delete only reaches its caller. */
    private class Fixture(
        scope: TestScope,
        val auth: FakeAuth,
        deleteStatus: HttpStatusCode = HttpStatusCode.NoContent,
        handler: (suspend MockRequestHandleScope.(HttpRequestData) -> HttpResponseData)? = null,
    ) {
        val requests = mutableListOf<String>()
        val cache = MemoryCache()
        val vm: ProfileViewModel

        init {
            val api = fakeApi(auth, requests, handler ?: { request ->
                if (request.url.encodedPath == "/v1/me" && deleteStatus.value >= 400) respondError(deleteStatus) else respond("", deleteStatus)
            })
            val repo = TimelineRepository(api, cache)
            val appScope = CoroutineScope(scope.backgroundScope.coroutineContext + SupervisorJob(scope.backgroundScope.coroutineContext[Job]))
            val session = SessionManager(auth, DeviceRegistrar(api), repo, api, FollowSuggester(PrefsContext()), appScope)
            vm = ProfileViewModel(api, repo, auth, session, ThemePreferences(PrefsContext()), NoAnalytics, testStrings)
        }
    }

    @Test
    fun aRecentSignInDeletesServerDataCacheAndAccount() = runTest {
        val f = Fixture(this, FakeAuth())
        f.vm.startDelete()
        assertEquals(DeleteStep.Confirm, f.vm.state.value.delete?.step)
        f.vm.confirmDelete()
        f.auth.currentUser.await { it == null }
        assertTrue("DELETE /v1/me" in f.requests)
        assertTrue(f.cache.cleared)
        assertEquals(listOf("deleteUser"), f.auth.calls)
    }

    @Test
    fun anOldSignInAsksForThePasswordFirst() = runTest {
        val f = Fixture(this, FakeAuth().apply { needsReauth = true })
        f.vm.startDelete()
        f.vm.confirmDelete()
        assertEquals(DeleteStep.Password, f.vm.state.value.delete?.step)
        assertTrue(f.requests.isEmpty())
        f.vm.deleteWithPassword("pw")
        f.auth.currentUser.await { it == null }
        assertEquals(listOf("reauthPassword:pw", "deleteUser"), f.auth.calls)
    }

    @Test
    fun googleAccountsReauthenticateWithGoogle() = runTest {
        val f = Fixture(this, FakeAuth(SignInMethod.Google).apply { needsReauth = true })
        f.vm.confirmDelete()
        assertEquals(DeleteStep.Google, f.vm.state.value.delete?.step)
    }

    @Test
    fun aRejectedDeleteAsksToReauthenticateAndRetries() = runTest {
        val auth = FakeAuth().apply { deleteFailures += reauthRequired() }
        val f = Fixture(this, auth)
        f.vm.confirmDelete()
        assertEquals(DeleteState(DeleteStep.Password), f.vm.state.await { it.delete?.step != DeleteStep.Deleting }.delete)
        f.vm.deleteWithPassword("pw")
        auth.currentUser.await { it == null }
        assertEquals(listOf("reauthPassword:pw", "deleteUser"), auth.calls)
        assertEquals(2, f.requests.count { it == "DELETE /v1/me" })
    }

    @Test
    fun aWrongPasswordKeepsThePasswordStepWithAnError() = runTest {
        val f = Fixture(this, FakeAuth().apply { needsReauth = true; reauthFails = IllegalStateException("wrong") })
        f.vm.confirmDelete()
        f.vm.deleteWithPassword("nope")
        val delete = assertNotNull(f.vm.state.await { it.delete?.step != DeleteStep.Deleting }.delete)
        assertEquals(DeleteStep.Password, delete.step)
        assertNotNull(delete.error)
        assertTrue(f.requests.isEmpty())
    }

    @Test
    fun aServerFailureLeavesTheAccountAndShowsAnError() = runTest {
        val f = Fixture(this, FakeAuth(), deleteStatus = HttpStatusCode.ServiceUnavailable)
        f.vm.confirmDelete()
        val delete = assertNotNull(f.vm.state.await { it.delete?.step != DeleteStep.Deleting }.delete)
        assertEquals(DeleteStep.Confirm, delete.step)
        assertNotNull(delete.error)
        assertTrue(f.auth.calls.isEmpty())
    }

    @Test
    fun cancelClosesTheDialogUnlessDeleting() = runTest {
        val f = Fixture(this, FakeAuth())
        f.vm.startDelete()
        f.vm.cancelDelete()
        assertNull(f.vm.state.value.delete)
    }

    /** /v1/me says [muted]; muted-topics answers [putStatus], and on success with "b" and "c" (another device muted c). */
    private fun mutesApi(muted: String, putStatus: HttpStatusCode = HttpStatusCode.OK): suspend MockRequestHandleScope.(HttpRequestData) -> HttpResponseData = { request ->
        val json = headersOf(HttpHeaders.ContentType, "application/json")
        when (request.url.encodedPath) {
            // A country, so refresh doesn't save the device's and race the unmute.
            "/v1/me" -> respond("""{"id":1,"topics":[],"country":"BD","muted_topics":$muted}""", headers = json)
            "/v1/me/muted-topics" ->
                if (putStatus.value >= 400) respondError(putStatus) else respond("""{"id":1,"topics":[],"muted_topics":["b","c"]}""", headers = json)
            else -> respond("""{"items":[]}""", headers = json)
        }
    }

    @Test
    fun unmutingSavesTheSetAndReloadsTheTimeline() = runTest {
        val f = Fixture(this, FakeAuth(), handler = mutesApi("""["a","b"]"""))
        f.vm.refresh()
        f.vm.state.await { it.muted == listOf("a", "b") }
        f.vm.unmute("a")
        assertEquals(listOf("b"), f.vm.state.value.muted)
        // The server's set wins, and the timeline reloads before it lands.
        f.vm.state.await { it.muted == listOf("b", "c") }
        assertTrue("PUT /v1/me/muted-topics" in f.requests)
        assertTrue(f.requests.any { it.startsWith("GET /v1/timeline") })
    }

    @Test
    fun aFailedUnmuteKeepsTheTopicMuted() = runTest {
        val f = Fixture(this, FakeAuth(), handler = mutesApi("""["a","b"]""", HttpStatusCode.ServiceUnavailable))
        f.vm.refresh()
        f.vm.state.await { it.muted == listOf("a", "b") }
        f.vm.unmute("a")
        val state = f.vm.state.await { it.error != null }
        assertEquals(listOf("a", "b"), state.muted)
    }
}
