package dev.changeloom.android.ui

import androidx.activity.compose.BackHandler
import androidx.annotation.StringRes
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.animateDpAsState
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.PagerState
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.ArrowForward
import androidx.compose.material.icons.rounded.BookmarkAdded
import androidx.compose.material.icons.rounded.NotificationsActive
import androidx.compose.material.icons.rounded.Tune
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dev.changeloom.android.R
import dev.changeloom.android.ui.components.IconTile
import dev.changeloom.android.ui.components.LoomMark
import dev.changeloom.android.ui.components.PrimaryButton
import dev.changeloom.android.ui.components.ScreenBackdrop
import dev.changeloom.android.ui.components.TextAction
import dev.changeloom.android.ui.theme.ChangeloomTheme
import dev.changeloom.android.ui.theme.Radius
import dev.changeloom.android.ui.theme.Spacing
import dev.changeloom.android.ui.theme.ThemeMode
import dev.changeloom.android.ui.theme.expoTween
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** Whether this install has finished (or skipped) the intro. Per install, so a second account doesn't see it again. */
interface IntroStore {
    var introSeen: Boolean
}

/** Decides once per install whether the intro shows before onboarding. Lives in the activity's store, not a session's. */
class IntroViewModel(private val store: IntroStore, private val io: CoroutineDispatcher = Dispatchers.IO) : ViewModel() {
    /** Null until the flag has been read from disk. */
    private val _show = MutableStateFlow<Boolean?>(null)
    val show: StateFlow<Boolean?> = _show.asStateFlow()

    init {
        viewModelScope.launch {
            val seen = withContext(io) { store.introSeen }
            _show.compareAndSet(null, !seen)
        }
    }

    /** Skip or Get started: never shown again on this install. */
    fun finish() {
        _show.value = false
        viewModelScope.launch(io) { store.introSeen = true }
    }
}

private data class IntroPage(val icon: ImageVector, @StringRes val title: Int, @StringRes val body: Int)

private val PAGES = listOf(
    IntroPage(Icons.Rounded.Tune, R.string.intro_feed_title, R.string.intro_feed_body),
    IntroPage(Icons.Rounded.BookmarkAdded, R.string.intro_save_title, R.string.intro_save_body),
    IntroPage(Icons.Rounded.NotificationsActive, R.string.intro_alerts_title, R.string.intro_alerts_body),
)

private val PAGE_ICON_SIZE = 72.dp
private val HEADER_MARK_SIZE = 32.dp
private val DOT_SIZE = 8.dp
private val DOT_ACTIVE_WIDTH = 24.dp

@Composable
fun IntroScreen(onDone: () -> Unit) {
    val pager = rememberPagerState { PAGES.size }
    val scope = rememberCoroutineScope()
    BackHandler(enabled = pager.currentPage > 0) { scope.launch { pager.animateScrollToPage(pager.currentPage - 1) } }
    IntroContent(
        pager,
        onNext = {
            if (pager.currentPage == PAGES.lastIndex) onDone() else scope.launch { pager.animateScrollToPage(pager.currentPage + 1) }
        },
        onSkip = onDone,
    )
}

@Composable
private fun IntroContent(pager: PagerState, onNext: () -> Unit, onSkip: () -> Unit) {
    val c = ChangeloomTheme.colors
    val last = pager.currentPage == PAGES.lastIndex
    Box(Modifier.fillMaxSize().background(c.bg)) {
        ScreenBackdrop(Modifier.matchParentSize())
        Column(Modifier.fillMaxSize().readableWidth().windowInsetsPadding(WindowInsets.safeDrawing)) {
            Row(
                Modifier.fillMaxWidth().heightIn(min = 48.dp).padding(horizontal = Spacing.gutter),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                LoomMark(Modifier.size(HEADER_MARK_SIZE))
                Spacer(Modifier.weight(1f))
                if (!last) TextAction(stringResource(R.string.intro_skip), onClick = onSkip, color = c.fgMuted)
            }
            HorizontalPager(pager, Modifier.weight(1f).fillMaxWidth()) { page ->
                IntroPageContent(PAGES[page])
            }
            PageDots(pager.currentPage, Modifier.align(Alignment.CenterHorizontally).padding(vertical = 16.dp))
            PrimaryButton(
                stringResource(if (last) R.string.intro_get_started else R.string.intro_next),
                onClick = onNext,
                modifier = Modifier.fillMaxWidth().padding(horizontal = Spacing.gutter).padding(bottom = 16.dp),
                trailingIcon = Icons.AutoMirrored.Rounded.ArrowForward,
            )
        }
    }
}

@Composable
private fun IntroPageContent(page: IntroPage) {
    val c = ChangeloomTheme.colors
    // Scrolls only when large text doesn't fit; centred otherwise.
    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(horizontal = 24.dp, vertical = 16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp, Alignment.CenterVertically),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        IconTile(page.icon, size = PAGE_ICON_SIZE)
        Spacer(Modifier.height(8.dp))
        Text(
            stringResource(page.title),
            Modifier.semantics { heading() },
            style = MaterialTheme.typography.headlineLarge,
            color = c.fg,
            textAlign = TextAlign.Center,
        )
        Text(stringResource(page.body), style = MaterialTheme.typography.bodyLarge, color = c.fgMuted, textAlign = TextAlign.Center)
    }
}

@Composable
private fun PageDots(current: Int, modifier: Modifier = Modifier) {
    val c = ChangeloomTheme.colors
    val description = stringResource(R.string.intro_page_of, current + 1, PAGES.size)
    Row(modifier.clearAndSetSemantics { contentDescription = description }, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
        PAGES.indices.forEach { index ->
            val active = index == current
            val width by animateDpAsState(if (active) DOT_ACTIVE_WIDTH else DOT_SIZE, expoTween(), label = "introDotWidth")
            val color by animateColorAsState(if (active) c.primary else c.lineStrong, expoTween(), label = "introDotColor")
            Box(Modifier.size(width = width, height = DOT_SIZE).clip(Radius.pill).background(color))
        }
    }
}

@Composable
private fun IntroPreview(page: Int) {
    IntroContent(rememberPagerState(initialPage = page) { PAGES.size }, onNext = {}, onSkip = {})
}

@Preview(name = "Intro, dark", heightDp = 800)
@Composable
private fun IntroFirstDark() = ChangeloomTheme(ThemeMode.Dark) { IntroPreview(0) }

@Preview(name = "Intro last, light", heightDp = 800)
@Composable
private fun IntroLastLight() = ChangeloomTheme(ThemeMode.Light) { IntroPreview(PAGES.lastIndex) }
