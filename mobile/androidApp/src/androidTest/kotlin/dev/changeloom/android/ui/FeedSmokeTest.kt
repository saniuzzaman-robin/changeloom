package dev.changeloom.android.ui

import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.ui.semantics.SemanticsActions
import androidx.compose.ui.semantics.getOrNull
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithTag
import androidx.compose.ui.test.onFirst
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.platform.app.InstrumentationRegistry
import dev.changeloom.android.R
import dev.changeloom.android.ui.theme.ChangeloomTheme
import dev.changeloom.android.ui.theme.ThemeMode
import dev.changeloom.shared.data.TimelineState
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test

/** Renders the feed without the api: stories show, their swipe actions reach TalkBack, the empty state works. */
class FeedSmokeTest {
    @get:Rule val compose = createComposeRule()

    private val strings = InstrumentationRegistry.getInstrumentation().targetContext.resources
    private val opened = mutableListOf<Long>()
    private val readToggles = mutableListOf<Pair<Long, Boolean>>()

    private fun showFeed(state: TimelineState) = compose.setContent {
        ChangeloomTheme(ThemeMode.Dark) {
            FeedContent(
                state = state,
                displayName = "Ada Lovelace",
                userEmail = null,
                contentPadding = PaddingValues(),
                onOpen = { opened += it },
                onProfile = {},
                onRefresh = {},
                onLoadMore = {},
                onSetRead = { id, read -> readToggles += id to read },
                onSetSaved = { _, _ -> },
                onDismissError = {},
            )
        }
    }

    @Test
    fun storiesShowAndOpen() {
        val stories = previewStories()
        showFeed(TimelineState(items = stories))
        compose.onNodeWithText(stories.first().title).assertIsDisplayed().performClick()
        assertEquals(listOf(stories.first().id), opened)
        // No ads were passed in, so none show.
        compose.onAllNodesWithTag("ad_card").assertCountEquals(0)
    }

    @Test
    fun swipeActionsAreAccessible() {
        val stories = previewStories()
        showFeed(TimelineState(items = stories))
        val markRead = strings.getString(R.string.mark_read)
        val hasMarkRead = SemanticsMatcher("has a \"$markRead\" action") { node ->
            node.config.getOrNull(SemanticsActions.CustomActions)?.any { it.label == markRead } == true
        }
        val node = compose.onAllNodes(hasMarkRead).onFirst().fetchSemanticsNode()
        compose.runOnUiThread { node.config[SemanticsActions.CustomActions].first { it.label == markRead }.action() }
        compose.waitForIdle()
        assertEquals(1, readToggles.size)
        assertEquals(true, readToggles.single().second)
    }

    @Test
    fun emptyFeedExplainsWhatComes() {
        showFeed(TimelineState())
        compose.onNodeWithText(strings.getString(R.string.feed_empty_title)).assertIsDisplayed()
    }
}
