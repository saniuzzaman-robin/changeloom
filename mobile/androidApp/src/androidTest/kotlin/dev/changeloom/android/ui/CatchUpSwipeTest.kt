package dev.changeloom.android.ui

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.pager.PagerState
import androidx.compose.foundation.pager.VerticalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performTouchInput
import androidx.compose.ui.test.swipeUp
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test

/** Catch-up pages scroll inside, like the real cards; a short flick must still turn the page. */
class CatchUpSwipeTest {
    @get:Rule val compose = createComposeRule()

    private lateinit var pager: PagerState

    private fun showPages(contentHeight: Dp) = compose.setContent {
        pager = rememberPagerState { 3 }
        VerticalPager(pager, Modifier.fillMaxSize().testTag("pager"), pageNestedScrollConnection = catchUpPageScroll(pager)) { page ->
            Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
                Text("Page $page")
                Spacer(Modifier.height(contentHeight))
            }
        }
    }

    /** A quick swipe over a fifth of the screen, far short of the half page a slow drag needs. */
    private fun flickUp() = compose.onNodeWithTag("pager").performTouchInput {
        swipeUp(startY = centerY + height * 0.1f, endY = centerY - height * 0.1f, durationMillis = 100)
    }

    @Test
    fun shortFlickTurnsThePage() {
        showPages(contentHeight = 0.dp)
        flickUp()
        compose.waitForIdle()
        assertEquals(1, pager.settledPage)
    }

    @Test
    fun flickThatScrollsALongStoryStaysOnIt() {
        showPages(contentHeight = 5000.dp)
        flickUp()
        compose.waitForIdle()
        assertEquals(0, pager.settledPage)
    }
}
