package dev.changeloom.android.telemetry

import kotlin.test.Test
import kotlin.test.assertEquals

class AnalyticsConsentTest {
    @Test
    fun outsideGdprEverythingIsGranted() {
        assertEquals(AnalyticsConsent(true, true, true, true), AnalyticsConsent.fromTcf(gdprApplies = false, purposeConsents = ""))
    }

    @Test
    fun underGdprPurposesMapToSignals() {
        // Purposes 1..10: storage, basic ads, ads profile, personalised ads, ..., 7 = measure ad performance.
        assertEquals(AnalyticsConsent(true, true, true, true), AnalyticsConsent.fromTcf(true, "1111111111"))
        assertEquals(AnalyticsConsent(true, true, false, false), AnalyticsConsent.fromTcf(true, "1100000000"))
        assertEquals(AnalyticsConsent(true, true, true, false), AnalyticsConsent.fromTcf(true, "1110001000"))
        assertEquals(AnalyticsConsent(false, false, false, true), AnalyticsConsent.fromTcf(true, "0011001000"))
        assertEquals(AnalyticsConsent(false, false, false, false), AnalyticsConsent.fromTcf(true, ""))
    }
}
