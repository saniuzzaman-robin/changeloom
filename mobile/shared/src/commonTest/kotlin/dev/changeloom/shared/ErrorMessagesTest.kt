package dev.changeloom.shared

import dev.changeloom.shared.data.ApiException
import dev.changeloom.shared.data.isUnexpected
import dev.changeloom.shared.data.userMessage
import kotlinx.io.IOException
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class ErrorMessagesTest {
    private fun api(status: Int) = ApiException(status, message = "HTTP $status: internal detail")

    @Test
    fun messagesNeverShowTheRawError() {
        val failed = "Couldn't load stories"
        assertEquals("Your session has expired. Sign in again.", userMessage(api(401), failed))
        assertEquals("Couldn't load stories: it isn't available any more.", userMessage(api(404), failed))
        assertEquals("Couldn't load stories: too many requests. Wait a minute and try again.", userMessage(api(429), failed))
        assertEquals("Couldn't load stories. Changeloom is having trouble; try again soon.", userMessage(api(503), failed))
        assertEquals("Couldn't load stories. Check your connection and try again.", userMessage(IOException("reset"), failed))
        assertEquals("Couldn't load stories. Try again.", userMessage(api(400), failed))
        assertEquals(
            "Couldn't load stories: this copy of the app couldn't be verified. Install Changeloom from Google Play.",
            userMessage(ApiException(403, "app_check_failed", "HTTP 403"), failed),
        )
        assertEquals("Couldn't load stories. Try again.", userMessage(IllegalStateException("boom"), failed))
    }

    @Test
    fun onlyServerErrorsAndBugsAreUnexpected() {
        assertTrue(isUnexpected(api(500)))
        assertTrue(isUnexpected(IllegalStateException("boom")))
        assertFalse(isUnexpected(api(404)))
        assertFalse(isUnexpected(IOException("offline")))
    }
}
