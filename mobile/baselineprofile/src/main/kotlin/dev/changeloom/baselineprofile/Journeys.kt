package dev.changeloom.baselineprofile

import androidx.benchmark.macro.MacrobenchmarkScope
import androidx.test.uiautomator.By
import androidx.test.uiautomator.Direction
import androidx.test.uiautomator.Until

// Test tags exposed as resource ids by MainActivity (testTagsAsResourceId).
private const val FEED = "feed"
private const val STORY_CARD = "story_card"
private const val STORY_DETAIL = "story_detail"
private const val WAIT_MS = 15_000L

/** :androidApp's application id, the same for every flavor. */
const val targetAppId = "dev.changeloom.android"

/** Cold-starts the app and waits for the timeline. The device must already be signed in with followed topics. */
fun MacrobenchmarkScope.startAndWaitForFeed() {
    pressHome()
    startActivityAndWait()
    waitForFeed()
}

fun MacrobenchmarkScope.waitForFeed() {
    check(device.wait(Until.hasObject(By.res(FEED)), WAIT_MS)) {
        "The timeline did not appear: sign in on the device and follow topics before benchmarking"
    }
}

fun MacrobenchmarkScope.scrollFeed() {
    val feed = device.findObject(By.res(FEED))
    // Keeps flings clear of the system gesture areas.
    feed.setGestureMargin(device.displayWidth / 5)
    repeat(2) {
        feed.fling(Direction.DOWN)
        device.waitForIdle()
    }
    feed.fling(Direction.UP)
    device.waitForIdle()
}

fun MacrobenchmarkScope.openStoryAndBack() {
    val card = checkNotNull(device.findObject(By.res(STORY_CARD))) { "No story card on screen: the timeline is empty" }
    card.click()
    check(device.wait(Until.hasObject(By.res(STORY_DETAIL)), WAIT_MS)) { "The story did not open" }
    device.waitForIdle()
    device.pressBack()
    waitForFeed()
}
