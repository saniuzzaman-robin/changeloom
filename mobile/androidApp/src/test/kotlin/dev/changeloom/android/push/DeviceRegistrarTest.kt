package dev.changeloom.android.push

import dev.changeloom.android.ui.FakeAuth
import dev.changeloom.android.ui.fakeApi
import io.ktor.client.engine.mock.respond
import io.ktor.client.engine.mock.respondError
import io.ktor.http.HttpStatusCode
import kotlinx.coroutines.test.runTest
import java.util.Collections
import kotlin.test.Test
import kotlin.test.assertEquals

class MemoryRegistrationStore : RegistrationStore {
    override var registeredKey: String? = null
    override var registeredAt = 0L
}

class DeviceRegistrarTest {
    private val auth = FakeAuth()
    private val requests: MutableList<String> = Collections.synchronizedList(mutableListOf())
    private var status = HttpStatusCode.NoContent
    private val api = fakeApi(auth, requests) { if (status.value >= 400) respondError(status) else respond("", status) }
    private var now = 1_000_000L
    private var fcm = "token-a"
    private val registrar = DeviceRegistrar(api, auth, MemoryRegistrationStore(), { now }, { fcm })

    private val registrations get() = requests.count { it == "PUT /v1/me/devices" }

    @Test
    fun anUnchangedRegistrationIsSentOncePerDay() = runTest {
        registrar.register()
        registrar.register()
        assertEquals(1, registrations)

        now += REREGISTER_AFTER_MS - 1
        registrar.register()
        assertEquals(1, registrations)

        now += 1
        registrar.register()
        assertEquals(2, registrations)
    }

    @Test
    fun aNewTokenOrAccountRegistersAgain() = runTest {
        registrar.register()
        fcm = "token-b"
        registrar.register()
        assertEquals(2, registrations)

        registrar.register("token-c") // from onNewToken
        assertEquals(3, registrations)

        auth.currentUser.value = auth.currentUser.value?.copy(uid = "other")
        registrar.register("token-c")
        assertEquals(4, registrations)
    }

    @Test
    fun aFailedRegistrationIsRetriedNextTime() = runTest {
        status = HttpStatusCode.ServiceUnavailable
        registrar.register()
        status = HttpStatusCode.NoContent
        registrar.register()
        registrar.register()
        assertEquals(2, registrations)
    }

    @Test
    fun signingOutForgetsTheRegistration() = runTest {
        registrar.register()
        registrar.unregister()
        registrar.register()
        assertEquals(2, registrations)
    }

    @Test
    fun nothingIsSentWhenSignedOut() = runTest {
        auth.currentUser.value = null
        registrar.register("token-a")
        assertEquals(0, registrations)
    }

    @Test
    fun aClockMovedBackRegistersAgain() = runTest {
        registrar.register()
        now -= 1
        registrar.register()
        assertEquals(2, registrations)
    }
}
