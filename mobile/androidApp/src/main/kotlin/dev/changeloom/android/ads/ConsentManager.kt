package dev.changeloom.android.ads

import android.app.Activity
import android.content.Context
import com.google.android.ump.ConsentDebugSettings
import com.google.android.ump.ConsentInformation
import com.google.android.ump.ConsentRequestParameters
import com.google.android.ump.FormError
import com.google.android.ump.UserMessagingPlatform
import dev.changeloom.android.BuildConfig
import dev.changeloom.android.telemetry.Analytics
import dev.changeloom.android.telemetry.AnalyticsConsent
import dev.changeloom.android.telemetry.AppLog
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

private const val TAG = "Consent"

// IAB TCF v2 keys the UMP SDK writes to the app's default shared preferences.
private const val TCF_GDPR_APPLIES = "IABTCF_gdprApplies"
private const val TCF_PURPOSE_CONSENTS = "IABTCF_PurposeConsents"

/**
 * Google UMP consent for ads. [gather] shows the consent form only where the law needs one (EEA, UK, ...); UMP
 * keeps the answer across launches. Ads load only while [canRequestAds], and the same answer sets Analytics
 * consent mode (whose manifest default is denied until then).
 */
class ConsentManager(private val context: Context, private val analytics: Analytics) {
    private val info: ConsentInformation = UserMessagingPlatform.getConsentInformation(context)

    private val _canRequestAds = MutableStateFlow(info.canRequestAds())
    val canRequestAds: StateFlow<Boolean> = _canRequestAds.asStateFlow()

    private val _privacyOptionsRequired = MutableStateFlow(privacyOptionsRequired())

    /** True where the user must be able to change their choice later (Profile shows "Privacy options"). */
    val privacyOptionsRequired: StateFlow<Boolean> = _privacyOptionsRequired.asStateFlow()

    init {
        // An answer from an earlier launch applies before this launch's update returns.
        applyAnalyticsConsent()
    }

    /** Refreshes the consent status and shows the form if it is required. Call once the first screen shows. */
    fun gather(activity: Activity) {
        info.requestConsentInfoUpdate(
            activity,
            requestParameters(),
            { UserMessagingPlatform.loadAndShowConsentFormIfRequired(activity) { error -> updated(error, "Consent form failed") } },
            // Offline or blocked: the answer cached from an earlier launch still applies.
            { error -> updated(error, "Consent status update failed") },
        )
    }

    private fun requestParameters(): ConsentRequestParameters {
        val params = ConsentRequestParameters.Builder()
        if (BuildConfig.UMP_TEST_DEVICE_ID.isNotEmpty()) {
            params.setConsentDebugSettings(
                ConsentDebugSettings.Builder(context)
                    .setDebugGeography(ConsentDebugSettings.DebugGeography.DEBUG_GEOGRAPHY_EEA)
                    .addTestDeviceHashedId(BuildConfig.UMP_TEST_DEVICE_ID)
                    .build(),
            )
        }
        return params.build()
    }

    fun showPrivacyOptions(activity: Activity) {
        UserMessagingPlatform.showPrivacyOptionsForm(activity) { error -> updated(error, "Privacy options form failed") }
    }

    private fun updated(error: FormError?, message: String) {
        if (error != null) AppLog.w(TAG, "$message: ${error.errorCode} ${error.message}")
        _canRequestAds.value = info.canRequestAds()
        _privacyOptionsRequired.value = privacyOptionsRequired()
        applyAnalyticsConsent()
    }

    private fun privacyOptionsRequired() =
        info.privacyOptionsRequirementStatus == ConsentInformation.PrivacyOptionsRequirementStatus.REQUIRED

    private fun applyAnalyticsConsent() {
        // Not asked yet: keep the manifest defaults until UMP knows the user's region.
        if (info.consentStatus == ConsentInformation.ConsentStatus.UNKNOWN) return
        val prefs = context.getSharedPreferences("${context.packageName}_preferences", Context.MODE_PRIVATE)
        val gdprApplies = prefs.getInt(TCF_GDPR_APPLIES, 0) == 1
        analytics.setConsent(AnalyticsConsent.fromTcf(gdprApplies, prefs.getString(TCF_PURPOSE_CONSENTS, null).orEmpty()))
    }
}
