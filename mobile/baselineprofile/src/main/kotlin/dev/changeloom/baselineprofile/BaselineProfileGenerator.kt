package dev.changeloom.baselineprofile

import androidx.benchmark.macro.junit4.BaselineProfileRule
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

/**
 * Generates the app's baseline profile: startup → timeline → story. Run with
 * `gradle :androidApp:generateStagingReleaseBaselineProfile` on a signed-in device (API 33+, or rooted).
 */
@RunWith(AndroidJUnit4::class)
class BaselineProfileGenerator {
    @get:Rule
    val rule = BaselineProfileRule()

    @Test
    fun generate() = rule.collect(packageName = targetAppId, includeInStartupProfile = true) {
        startAndWaitForFeed()
        scrollFeed()
        openStoryAndBack()
    }
}
